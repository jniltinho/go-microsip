package control

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jniltinho/go-microsip/internal/core/app"
	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports"
)

const (
	// ApprovalWindow is how long a person has to approve a request in the window.
	ApprovalWindow = 60 * time.Second
	// MaxPending is how many requests wait for approval at once; more are answered busy.
	MaxPending = 8
	// PauseAfterDenyAll is how long "Deny all and pause" refuses new mutations.
	PauseAfterDenyAll = 5 * time.Minute
	// DTMFGap is the time between two paced digits: a 120 ms event and a pause.
	DTMFGap = 200 * time.Millisecond
)

// Scopes is every scope a grant may carry.
var Scopes = []string{
	"read:contacts", "read:history", "read:devices", "read:config",
	"control:dial", "control:answer", "control:reject", "control:hangup", "control:dtmf",
	"control:mute", "control:hold", "control:transfer", "control:dnd",
}

// scopeFor is the scope a grant needs for an operation; empty when it needs none.
func scopeFor(op string) string {
	switch op {
	case StatusGet, OperationGet:
		return ""
	case ContactsSearch:
		return "read:contacts"
	case HistoryList:
		return "read:history"
	case DevicesList:
		return "read:devices"
	case ConfigShow:
		return "read:config"
	case DNDSet:
		return "control:dnd"
	}
	return "control:" + strings.TrimPrefix(op, "call_")
}

// Phone is what the owner needs of the softphone: *app.Phone.
type Phone interface {
	Send(domain.Command) error
	SetDND(on bool) error
	Settings() app.Settings
	Contacts(search string) ([]app.Contact, error)
	History(filter app.HistoryFilter, search string) ([]app.HistoryEntry, error)
	AudioDevices() (inputs, outputs []ports.Device, err error)
	Ledger() (app.Ledger, error)
}

// Approval is a mutation waiting for a person's decision at the window.
type Approval struct {
	OperationID string
	// Description is what would happen, for the person deciding; it may show the destination.
	Description string
	// Requester is who asked.
	Requester string
	Deadline  time.Time

	operation Operation
}

// Owner is the profile owner's side of agent control for one start of the window: the epoch,
// the key of the request MACs, the requests waiting for a person and the pacing of DTMF.
//
// The MAC key is random at every start and never leaves memory, so the ledger can tell a reused
// idempotency key with another request in the same epoch without storing digits or numbers.
// After a restart the key is gone: old keys are answered stale_epoch and never run again.
//
// A front end feeds it the engine's events (Observe) and shows what Pending returns; changed is
// called, outside any lock, whenever that list changes.
type Owner struct {
	phone   Phone
	mode    Mode
	ledger  app.Ledger // nil: no database; nothing is changed without a ledger
	epoch   int64
	key     []byte
	changed func()
	// now and gap are the clock and the DTMF gap; tests replace them.
	now func() time.Time
	gap time.Duration

	mu           sync.Mutex
	closed       bool
	pending      []Approval
	pausedUntil  time.Time
	registration domain.RegistrationStatus
	call         *domain.CallSnapshot
	pace         sync.Mutex // one DTMF operation at a time
}

// Errors of starting the headless service's owner.
var (
	// ErrNoLedger: the service needs the database, where its grant is.
	ErrNoLedger = errors.New("the settings database is needed")
	// ErrBootstrapConsumed: the bootstrap grant already started a service session.
	ErrBootstrapConsumed = errors.New("the bootstrap grant was already used: export a new one from the window for a new service session")
	// ErrBootstrapInvalid: the bootstrap grant is unknown, expired or revoked.
	ErrBootstrapInvalid = errors.New("the bootstrap grant is unknown, expired or revoked")
)

// NewServiceOwner is the owner of the headless service: it opens a new epoch and consumes, once,
// the bootstrap grant a person exported from the window. Nothing waits for a person there: what
// that grant does not cover is answered grant_required.
func NewServiceOwner(phone Phone, bootstrapID, capability string) (*Owner, error) {
	o, err := NewOwner(phone, nil)
	if err != nil {
		return nil, err
	}
	if o.ledger == nil {
		return nil, ErrNoLedger
	}
	o.mode = ModeService
	switch _, bound, err := o.ledger.ConsumeBootstrap(bootstrapID, capabilityHash(capability), o.epoch); {
	case err != nil:
		return nil, ErrNoLedger
	case !bound:
		if consumed, _ := o.ledger.BootstrapConsumed(bootstrapID); consumed {
			return nil, ErrBootstrapConsumed
		}
		return nil, ErrBootstrapInvalid
	}
	return o, nil
}

// NewOwner opens a new epoch in the phone's ledger, for the window. Without a database, or when
// the epoch cannot be opened, the owner still answers status and refuses every change.
func NewOwner(phone Phone, changed func()) (*Owner, error) {
	o := &Owner{phone: phone, mode: ModeWindow, changed: changed, now: time.Now, gap: DTMFGap, key: make([]byte, 32)}
	if o.changed == nil {
		o.changed = func() {}
	}
	if _, err := rand.Read(o.key); err != nil {
		return nil, err
	}
	if ledger, err := phone.Ledger(); err == nil {
		if epoch, err := ledger.BeginControlEpoch(); err == nil {
			o.ledger, o.epoch = ledger, epoch
		}
	}
	return o, nil
}

// Epoch is the owner's session in the ledger; 0 without one.
func (o *Owner) Epoch() int64 { return o.epoch }

// Welcome is what the owner answers a client's hello with.
func (o *Owner) Welcome() Welcome {
	return Welcome{Protocol: Protocol, Version: SchemaVersion, Mode: o.mode, Epoch: uint64(o.epoch)}
}

// Close stops the owner: what waits is left to the next epoch, which expires it.
func (o *Owner) Close() {
	o.mu.Lock()
	o.closed = true
	o.mu.Unlock()
}

// Observe takes an event of the engine: the owner knows the registration and the call from them.
func (o *Owner) Observe(ev domain.Event) {
	o.mu.Lock()
	defer o.mu.Unlock()
	switch e := ev.(type) {
	case domain.RegistrationChanged:
		o.registration = e.State.Status
	case domain.CallChanged:
		if e.Call.Phase.IsFinal() {
			if o.call != nil && o.call.ID == e.Call.ID {
				o.call = nil
			}
		} else {
			o.call = &e.Call
		}
	}
}

// Pending lists the requests waiting for a decision, oldest first.
func (o *Owner) Pending() []Approval {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.pending)
}

func (o *Owner) mac(op Operation) []byte {
	canonical, _ := json.Marshal(op)
	h := hmac.New(sha256.New, o.key)
	h.Write(canonical)
	return h.Sum(nil)
}

// newID returns an identifier nobody can guess: "<prefix>-<epoch>-<24 hex digits>".
func (o *Owner) newID(prefix string) (string, error) {
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return "", fail(Internal, "no system randomness")
	}
	return fmt.Sprintf("%s-%d-%s", prefix, o.epoch, hex.EncodeToString(random)), nil
}

// capabilityHash is the only form in which a grant's capability is stored.
func capabilityHash(capability string) []byte {
	sum := sha256.Sum256([]byte(capability))
	return sum[:]
}

// masked is a destination as audit rows show it: its last two characters.
func masked(target string) string {
	runes := []rune(target)
	return "***" + string(runes[max(0, len(runes)-2):])
}

// describe is an operation in words for the approval prompt. The person deciding sees the whole
// destination; digits are counted, never shown.
func describe(op Operation) string {
	either := func(on bool, yes, no string) string {
		if on {
			return yes
		}
		return no
	}
	switch op.Op {
	case CallDial:
		return "call " + op.Target
	case CallAnswer:
		return "answer the ringing call (starts the microphone)"
	case CallReject:
		return "reject the ringing call"
	case CallHangup:
		return "hang up the call"
	case CallDTMF:
		return fmt.Sprintf("send %d DTMF digit(s) in the call", utf8.RuneCountInString(op.Digits))
	case CallMute:
		return either(op.Muted, "mute the microphone", "unmute the microphone")
	case CallHold:
		return either(op.Held, "put the call on hold", "resume the call")
	case CallTransfer:
		return "transfer the call to " + op.Target
	case DNDSet:
		return either(op.Enabled, "turn do-not-disturb on", "turn do-not-disturb off")
	}
	return op.Op
}

// auditDetail is an operation for the audit trail: no whole number, no digits.
func auditDetail(op Operation) string {
	switch op.Op {
	case CallDial, CallTransfer:
		return "destination " + masked(op.Target)
	case CallDTMF:
		return fmt.Sprintf("%d digit(s)", utf8.RuneCountInString(op.Digits))
	case CallMute:
		return fmt.Sprintf("muted=%v", op.Muted)
	case CallHold:
		return fmt.Sprintf("held=%v", op.Held)
	case DNDSet:
		return fmt.Sprintf("enabled=%v", op.Enabled)
	}
	return "-"
}

var (
	errLedger    = fail(StorageUnavailable, "the ledger is unavailable; nothing was done")
	errCallGone  = fail(NotFound, "the call ended or changed; read status_get again")
	errNoStorage = fail(StorageUnavailable, "storage is unavailable")
)

// Handle answers one request. pid is the client's process, for the approval prompt; 0 when it
// is not known.
func (o *Owner) Handle(r Request, pid int) Response {
	if err := r.Validate(); err != nil {
		return Failure(r.RequestID, err)
	}
	o.mu.Lock()
	response, notify := o.handle(r, pid)
	o.mu.Unlock()
	if notify {
		o.changed()
	}
	return response
}

func (o *Owner) handle(r Request, pid int) (response Response, notify bool) {
	var data any
	var err error
	switch r.Operation.Op {
	case StatusGet:
		data = o.status()
	case OperationGet:
		data, err = o.operationData(r.Operation.OperationID)
	case DevicesList, ContactsSearch, HistoryList, ConfigShow:
		data, err = o.grantedRead(r)
	default:
		response, notify, err = o.mutation(r, pid)
		if err == nil {
			return response, notify
		}
	}
	if err != nil {
		return Failure(r.RequestID, err), false
	}
	return OK(r.RequestID, data), false
}

type callStatus struct {
	CallID    uint64 `json:"call_id"`
	Version   uint64 `json:"version"`
	Direction string `json:"direction"`
	Phase     string `json:"phase"`
	Secure    bool   `json:"secure"`
	Muted     bool   `json:"muted"`
	Held      bool   `json:"held"`
}

type status struct {
	Mode         Mode        `json:"mode"`
	Epoch        int64       `json:"epoch"`
	Registration string      `json:"registration"`
	DND          bool        `json:"dnd"`
	Call         *callStatus `json:"call"`
}

// status is the registration, do-not-disturb and the call, without a name or a number.
func (o *Owner) status() status {
	s := status{Mode: o.mode, Epoch: o.epoch, Registration: o.registration.String(), DND: o.phone.Settings().DND}
	if c := o.call; c != nil {
		s.Call = &callStatus{CallID: uint64(c.ID), Version: c.Version, Direction: c.Direction.String(),
			Phase: c.Phase.Kind.String(), Secure: c.Secure, Muted: c.Muted, Held: c.Phase.Kind == domain.Held}
	}
	return s
}

type operationStatus struct {
	OperationID  string           `json:"operation_id"`
	Operation    string           `json:"operation"`
	State        string           `json:"state"`
	Final        bool             `json:"final"`
	CurrentEpoch bool             `json:"current_epoch"`
	Outcome      *json.RawMessage `json:"outcome"`
}

func (o *Owner) operationData(id string) (operationStatus, error) {
	if o.ledger == nil {
		return operationStatus{}, fail(StorageUnavailable, "the ledger is unavailable")
	}
	record, found, err := o.ledger.Operation(id)
	switch {
	case err != nil:
		return operationStatus{}, fail(StorageUnavailable, "the ledger is unavailable")
	case !found:
		return operationStatus{}, fail(NotFound, "unknown operation, or older than the 24-hour retention")
	}
	out := operationStatus{OperationID: record.OperationID, Operation: record.Operation, State: string(record.State),
		Final: record.State.Final(), CurrentEpoch: record.Epoch == o.epoch}
	if json.Valid([]byte(record.Outcome)) {
		raw := json.RawMessage(record.Outcome)
		out.Outcome = &raw
	}
	return out, nil
}

// checkCall: the call a call operation names must still be the current call, at the version the
// client saw.
func (o *Owner) checkCall(op Operation) (domain.CallID, error) {
	ref, named := op.Call()
	if !named {
		return 0, nil
	}
	if o.call == nil || uint64(o.call.ID) != ref.CallID || o.call.Version != ref.ExpectedVersion {
		return 0, errCallGone
	}
	return o.call.ID, nil
}

func (o *Owner) mutation(r Request, pid int) (Response, bool, error) {
	id, op, key := r.RequestID, r.Operation, *r.IdempotencyKey
	if o.ledger == nil {
		return Response{}, false, errLedger
	}
	mac := o.mac(op)
	existing, found, err := o.ledger.FindOperationKey(key)
	if err != nil {
		return Response{}, false, errLedger
	}
	if !found {
		// Used in this session, but older than the retention window.
		if used, err := o.ledger.OperationKeyUsed(o.epoch, key); err != nil {
			return Response{}, false, errLedger
		} else if used {
			return Response{}, false, fail(Conflict, "this idempotency key was already used in this session")
		}
	}
	if found {
		if existing.Epoch != o.epoch {
			stale := Failure(id, fail(StaleEpoch, "this key belongs to an earlier owner session; query the operation or use a new key"))
			stale.OperationID = &existing.OperationID
			return stale, false, nil
		}
		if !hmac.Equal(existing.PayloadMAC, mac) {
			return Response{}, false, fail(Conflict, "this idempotency key was used for a different request")
		}
		// The same request again: it is reported, never repeated.
		switch existing.State {
		case app.OpAwaitingApproval:
			if !slices.ContainsFunc(o.pending, func(p Approval) bool { return p.OperationID == existing.OperationID }) {
				return Response{}, false, fail(StorageUnavailable, "this request can no longer be approved; use a new key")
			}
			return Pending(id, StatusApprovalRequired, existing.OperationID), false, nil
		case app.OpExecuting:
			return Pending(id, StatusAccepted, existing.OperationID), false, nil
		}
		data, err := o.operationData(existing.OperationID)
		if err != nil {
			return Response{}, false, err
		}
		done := OK(id, data)
		done.OperationID = &existing.OperationID
		return done, false, nil
	}
	if _, err := o.checkCall(op); err != nil {
		return Response{}, false, err
	}
	now := o.now()
	// "Deny all and pause" stops grants too, not only prompts.
	if now.Before(o.pausedUntil) {
		return Response{}, false, fail(Busy, "control requests are paused in the window; try again later")
	}
	// A grant that covers this exact request runs it without a prompt; anything it does not
	// cover asks a person at the window, and is refused by the service, where nobody is.
	grant, granted, err := o.requestGrant(r)
	if err != nil {
		return Response{}, false, err
	}
	if granted {
		refusal := o.grantAllows(grant, op)
		if refusal == nil {
			operationID, err := o.record(key, op, mac, app.OpExecuting)
			if err != nil {
				return Response{}, false, err
			}
			_ = o.ledger.Audit(op.Op, "granted", auditDetail(op)+" by grant "+grant.ID)
			o.execute(operationID, op)
			return Pending(id, StatusAccepted, operationID), false, nil
		}
		if o.mode == ModeService {
			return Response{}, false, refusal
		}
	}
	if o.mode == ModeService {
		return Response{}, false, fail(GrantRequired, "the service runs only what its grant allows")
	}
	if len(o.pending) >= MaxPending {
		return Response{}, false, fail(Busy, "too many requests are waiting for approval")
	}
	operationID, err := o.record(key, op, mac, app.OpAwaitingApproval)
	if err != nil {
		return Response{}, false, err
	}
	_ = o.ledger.Audit(op.Op, "requested", auditDetail(op))
	requester := "A local program"
	if pid > 0 {
		requester = fmt.Sprintf("A local program (process %d)", pid)
	}
	o.pending = append(o.pending, Approval{OperationID: operationID, Description: describe(op), Requester: requester,
		Deadline: now.Add(ApprovalWindow), operation: op})
	time.AfterFunc(ApprovalWindow+50*time.Millisecond, func() { o.Tick(o.now()) })
	return Pending(id, StatusApprovalRequired, operationID), true, nil
}

// record writes a new ledger row before anything happens and returns its id.
func (o *Owner) record(key string, op Operation, mac []byte, state app.OperationState) (string, error) {
	operationID, err := o.newID("op")
	if err != nil {
		return "", err
	}
	if o.ledger.InsertOperation(app.OperationRecord{OperationID: operationID, Epoch: o.epoch, IdempotencyKey: key,
		Operation: op.Op, PayloadMAC: mac, State: state, CreatedUTC: o.now().Unix()}) != nil {
		return "", errLedger
	}
	return operationID, nil
}

// finish records the final state; the audit row names the operation only.
func (o *Owner) finish(operationID, name string, state app.OperationState, outcome any) {
	text, _ := json.Marshal(outcome)
	_ = o.ledger.UpdateOperation(operationID, state, string(text))
	_ = o.ledger.Audit(name, string(state), "-")
}

type reason struct {
	Reason string `json:"reason"`
	Sent   *int   `json:"sent,omitempty"`
}

type sent struct {
	Sent any `json:"sent"`
}

// Decide is the person's answer to a pending request.
func (o *Owner) Decide(operationID string, approve bool) {
	o.mu.Lock()
	notify := o.decide(operationID, approve)
	o.mu.Unlock()
	if notify {
		o.changed()
	}
}

func (o *Owner) decide(operationID string, approve bool) bool {
	i := slices.IndexFunc(o.pending, func(p Approval) bool { return p.OperationID == operationID })
	if i < 0 || o.closed {
		return false
	}
	p := o.pending[i]
	o.pending = slices.Delete(o.pending, i, i+1)
	switch {
	case !approve:
		o.finish(operationID, p.operation.Op, app.OpDenied, reason{Reason: "denied"})
	case o.now().After(p.Deadline):
		o.finish(operationID, p.operation.Op, app.OpExpired, reason{Reason: "expired"})
	default:
		_ = o.ledger.Audit(p.operation.Op, "approved", auditDetail(p.operation))
		o.execute(operationID, p.operation)
	}
	return true
}

// DenyAll denies every waiting request and refuses new mutations, granted ones too, for
// PauseAfterDenyAll.
func (o *Owner) DenyAll() {
	o.mu.Lock()
	notify := false
	for len(o.pending) > 0 {
		notify = o.decide(o.pending[0].OperationID, false) || notify
	}
	o.pausedUntil = o.now().Add(PauseAfterDenyAll)
	o.mu.Unlock()
	if notify {
		o.changed()
	}
}

// Tick expires the requests nobody decided in time. The owner calls it itself when a request's
// time is up.
func (o *Owner) Tick(now time.Time) {
	o.mu.Lock()
	var gone []Approval
	if !o.closed {
		o.pending = slices.DeleteFunc(o.pending, func(p Approval) bool {
			if now.After(p.Deadline) {
				gone = append(gone, p)
				return true
			}
			return false
		})
	}
	for _, p := range gone {
		o.finish(p.OperationID, p.operation.Op, app.OpExpired, reason{Reason: "expired"})
	}
	o.mu.Unlock()
	if len(gone) > 0 {
		o.changed()
	}
}

// execute runs an authorized operation once. The ledger says "executing" before anything is
// sent, so a crash leaves outcome_unknown, never a second try.
func (o *Owner) execute(operationID string, op Operation) {
	call, err := o.checkCall(op)
	if err != nil {
		o.finish(operationID, op.Op, app.OpFailed, reason{Reason: "the call ended or changed before approval"})
		return
	}
	if o.ledger.UpdateOperation(operationID, app.OpExecuting, "") != nil {
		// Nothing is sent without a durable "executing" record.
		o.finish(operationID, op.Op, app.OpFailed, reason{Reason: "the ledger is unavailable; nothing was done"})
		return
	}
	var command domain.Command
	switch op.Op {
	case CallDial, CallTransfer:
		target, err := domain.ParseDialTarget(op.Target)
		if err != nil {
			o.finish(operationID, op.Op, app.OpFailed, reason{Reason: "invalid"})
			return
		}
		command = domain.Dial{Target: target}
		if op.Op == CallTransfer {
			command = domain.Transfer{Call: call, Target: target}
		}
	case CallAnswer:
		command = domain.Answer{Call: call}
	case CallReject:
		command = domain.Reject{Call: call}
	case CallHangup:
		command = domain.Hangup{Call: call}
	case CallMute:
		command = domain.SetMute{Call: call, Muted: op.Muted}
	case CallHold:
		command = domain.Resume{Call: call}
		if op.Held {
			command = domain.Hold{Call: call}
		}
	case CallDTMF:
		go o.paceDTMF(operationID, call, op.Digits)
		return
	}
	if op.Op == DNDSet {
		err = o.phone.SetDND(op.Enabled)
	} else {
		err = o.phone.Send(command)
	}
	if err != nil {
		o.finish(operationID, op.Op, app.OpFailed, reason{Reason: "the engine refused the command"})
		return
	}
	o.finish(operationID, op.Op, app.OpSucceeded, sent{Sent: true})
}

// paceDTMF sends the digits of one approved operation, DTMFGap apart, while its call is
// established. The digits live only here, in memory.
func (o *Owner) paceDTMF(operationID string, call domain.CallID, digits string) {
	o.pace.Lock()
	defer o.pace.Unlock()
	count := 0
	for _, digit := range digits {
		o.mu.Lock()
		alive := !o.closed && o.call != nil && o.call.ID == call && o.call.Phase.Kind == domain.Established
		key, err := domain.NewDTMFKey(digit)
		if alive && err == nil {
			err = o.phone.Send(domain.SendDTMF{Call: call, Key: key})
		}
		if !alive || err != nil {
			if !o.closed {
				o.finish(operationID, CallDTMF, app.OpFailed, reason{Reason: "the call ended", Sent: &count})
			}
			o.mu.Unlock()
			return
		}
		o.mu.Unlock()
		count++
		time.Sleep(o.gap)
	}
	o.mu.Lock()
	if !o.closed {
		o.finish(operationID, CallDTMF, app.OpSucceeded, sent{Sent: count})
	}
	o.mu.Unlock()
}
