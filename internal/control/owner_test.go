package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/go-microsip/internal/core/app"
	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports"
	"github.com/jniltinho/go-microsip/internal/core/store"
)

// Ported from the tests of the Rust candidate's owner (apps/rust-microsip/src/owner.rs and
// src/ui/state/control.rs), without its headless service, which go-microsip does not have.

// engine stands in for the softphone: it writes down what it is sent and keeps a real database.
type engine struct {
	mu      sync.Mutex
	sent    []string
	refuse  bool
	store   *store.Store
	account *domain.AccountSettings
	dnd     bool
}

func (e *engine) Send(c domain.Command) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.refuse {
		return app.Refused(domain.NoticeOverloaded)
	}
	switch c := c.(type) {
	case domain.Dial:
		e.sent = append(e.sent, "Dial("+c.Target.Value()+")")
	case domain.Answer:
		e.sent = append(e.sent, fmt.Sprintf("Answer(%d)", c.Call))
	case domain.Reject:
		e.sent = append(e.sent, fmt.Sprintf("Reject(%d)", c.Call))
	case domain.Hangup:
		e.sent = append(e.sent, fmt.Sprintf("Hangup(%d)", c.Call))
	case domain.SendDTMF:
		e.sent = append(e.sent, fmt.Sprintf("Dtmf(%d, %c)", c.Call, c.Key.Rune()))
	case domain.SetMute:
		e.sent = append(e.sent, fmt.Sprintf("SetMute(%d, %v)", c.Call, c.Muted))
	case domain.Hold:
		e.sent = append(e.sent, fmt.Sprintf("Hold(%d)", c.Call))
	case domain.Resume:
		e.sent = append(e.sent, fmt.Sprintf("Resume(%d)", c.Call))
	case domain.Transfer:
		e.sent = append(e.sent, fmt.Sprintf("Transfer(%d, %s)", c.Call, c.Target.Value()))
	default:
		e.sent = append(e.sent, fmt.Sprintf("%T", c))
	}
	return nil
}

func (e *engine) SetDND(on bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.refuse {
		return app.Refused(domain.NoticeOverloaded)
	}
	e.dnd = on
	e.sent = append(e.sent, fmt.Sprintf("SetDnd(%v)", on))
	return nil
}

func (e *engine) Settings() app.Settings {
	e.mu.Lock()
	defer e.mu.Unlock()
	return app.Settings{Account: e.account, Theme: "dark", AudioInput: "mic-1", DND: e.dnd}
}

func (e *engine) ContactsUpTo(search string, limit int) ([]app.Contact, error) {
	return e.store.SearchContacts(search, limit)
}

func (e *engine) HistoryUpTo(filter app.HistoryFilter, search string, limit int) ([]app.HistoryEntry, error) {
	return e.store.RecentCalls(limit, filter, search)
}

func (e *engine) AudioDevices() (inputs, outputs []ports.Device, err error) {
	return []ports.Device{{ID: "mic-1", Name: "Fictional microphone"}}, []ports.Device{{ID: "out-1", Name: "Fictional speaker"}}, nil
}

func (e *engine) Ledger() (app.Ledger, error) {
	if e.store == nil {
		return nil, app.ErrNoStorage
	}
	return e.store, nil
}

// take returns what the engine was sent since the last time.
func (e *engine) take() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	sent := e.sent
	e.sent = nil
	return sent
}

// session is an owner on a database in memory, with a clock the test moves.
type session struct {
	*Owner
	engine  *engine
	clock   time.Time
	changes int
}

func newSession(t *testing.T, durable bool) *session {
	t.Helper()
	s := &session{engine: &engine{}, clock: time.Now()}
	if durable {
		db, err := store.OpenInMemory()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		s.engine.store = db
	}
	s.restart(t)
	return s
}

// restart is a new start of the window on the same database: a new epoch and a new key.
func (s *session) restart(t *testing.T) {
	t.Helper()
	owner, err := NewOwner(s.engine, func() { s.changes++ })
	if err != nil {
		t.Fatal(err)
	}
	owner.now = func() time.Time { return s.clock }
	owner.gap = 20 * time.Millisecond
	s.Owner = owner
}

func (s *session) ask(key string, op Operation) Response {
	r := Request{SchemaVersion: 1, RequestID: "r1", Operation: op}
	if key != "" {
		r.IdempotencyKey = &key
	}
	return s.Handle(r, 0)
}

func (s *session) with(capability, key string, op Operation) Response {
	r := Request{SchemaVersion: 1, RequestID: "r1", Operation: op, Capability: &capability}
	if key != "" {
		r.IdempotencyKey = &key
	}
	return s.Handle(r, 0)
}

func (s *session) established(id, version uint64) {
	s.Observe(domain.CallChanged{Call: domain.CallSnapshot{ID: domain.CallID(id), Version: version, Remote: "1002",
		Phase: domain.CallPhase{Kind: domain.Established}}})
}

// state asks operation_get for an operation and returns its state and outcome.
func (s *session) state(t *testing.T, id string) (state, outcome string, current bool) {
	t.Helper()
	r := s.ask("", Operation{Op: OperationGet, OperationID: id})
	var data struct {
		State        string          `json:"state"`
		CurrentEpoch bool            `json:"current_epoch"`
		Outcome      json.RawMessage `json:"outcome"`
	}
	if r.Status != StatusOK || json.Unmarshal(r.Data, &data) != nil {
		t.Fatalf("operation_get %s: %+v", id, r)
	}
	return data.State, string(data.Outcome), data.CurrentEpoch
}

func dial(target string) Operation { return Operation{Op: CallDial, Target: target} }

func code(r Response) Code {
	if r.Error == nil {
		return ""
	}
	return r.Error.Code
}

func id(t *testing.T, r Response) string {
	t.Helper()
	if r.OperationID == nil {
		t.Fatalf("no operation id in %+v (%+v)", r, r.Error)
	}
	return *r.OperationID
}

func equal(got []string, want ...string) bool { return fmt.Sprint(got) == fmt.Sprint(want) }

func TestStatusIsMaskedAndPersonalReadsNeedAGrant(t *testing.T) {
	s := newSession(t, true)
	s.Observe(domain.RegistrationChanged{State: domain.RegistrationState{Status: domain.Registered}, Account: "1001@pbx.invalid"})
	s.established(3, 2)
	status := s.ask("", Operation{Op: StatusGet})
	wire, _ := json.Marshal(status)
	text := string(wire)
	if strings.Contains(text, "1002") || strings.Contains(text, "pbx.invalid") {
		t.Errorf("the status names someone: %s", text)
	}
	for _, want := range []string{`"call_id":3`, `"version":2`, `"phase":"established"`, `"registration":"registered"`, `"mode":"gui"`, `"direction":"outgoing"`, `"held":false`} {
		if !strings.Contains(text, want) {
			t.Errorf("the status lacks %s: %s", want, text)
		}
	}
	for _, read := range []string{ConfigShow, DevicesList, ContactsSearch, HistoryList} {
		if r := s.ask("", Operation{Op: read}); code(r) != GrantRequired {
			t.Errorf("%s without a grant: %+v", read, r)
		}
	}
	// A call that ended is no call.
	s.Observe(domain.CallChanged{Call: domain.CallSnapshot{ID: 3, Version: 3, Phase: domain.EndedWith(domain.EndFailed)}})
	if wire, _ := json.Marshal(s.ask("", Operation{Op: StatusGet})); !strings.Contains(string(wire), `"call":null`) {
		t.Errorf("the status after the call ended: %s", wire)
	}
}

func TestAPersonApprovesExactlyOnceAndReplaysNeverRepeat(t *testing.T) {
	s := newSession(t, true)
	key := "k1"
	first := s.Handle(Request{SchemaVersion: 1, RequestID: "r1", IdempotencyKey: &key, Operation: dial("1002")}, 42)
	if first.Status != StatusApprovalRequired {
		t.Fatalf("the first request: %+v (%+v)", first, first.Error)
	}
	operation := id(t, first)
	if pending := s.Pending(); len(pending) != 1 || !strings.Contains(pending[0].Requester, "42") || pending[0].Description != "call 1002" ||
		!pending[0].Deadline.Equal(s.clock.Add(ApprovalWindow)) {
		t.Fatalf("waiting for a person: %+v", pending)
	}
	if s.changes != 1 {
		t.Errorf("the window was told of %d changes, want 1", s.changes)
	}
	if sent := s.engine.take(); len(sent) != 0 {
		t.Errorf("sent before the approval: %v", sent)
	}
	// The same request again is the same pending operation.
	if again := s.ask("k1", dial("1002")); id(t, again) != operation || again.Status != StatusApprovalRequired || len(s.Pending()) != 1 {
		t.Errorf("the request repeated: %+v", again)
	}
	// The same key for another destination is a conflict.
	if other := s.ask("k1", dial("1003")); code(other) != Conflict {
		t.Errorf("the key reused for another request: %+v", other)
	}
	s.Decide(operation, true)
	if sent := s.engine.take(); !equal(sent, "Dial(1002)") {
		t.Errorf("sent on the approval: %v", sent)
	}
	// Repeated after it ran, the request reports the outcome and sends nothing.
	if replay := s.ask("k1", dial("1002")); replay.Status != StatusOK || id(t, replay) != operation || len(s.engine.take()) != 0 {
		t.Errorf("the request repeated after it ran: %+v", replay)
	}
	if state, outcome, current := s.state(t, operation); state != "succeeded" || outcome != `{"sent":true}` || !current {
		t.Errorf("the operation: %s %s, current epoch %v", state, outcome, current)
	}
	// Deciding twice does nothing.
	s.Decide(operation, true)
	if sent := s.engine.take(); len(sent) != 0 {
		t.Errorf("sent on a second decision: %v", sent)
	}
	// The audit names the operation and its masked destination, never the number.
	var trail []string
	for _, row := range s.AuditRows(10) {
		trail = append(trail, row.Operation+" "+row.Decision+" "+row.Detail)
	}
	if !equal(trail, "call_dial succeeded -", "call_dial approved destination ***02", "call_dial requested destination ***02") {
		t.Errorf("the audit trail: %q", trail)
	}
}

func TestDenialAndExpiryHaveNoEffect(t *testing.T) {
	s := newSession(t, true)
	denied := id(t, s.ask("k-deny", dial("1002")))
	s.Decide(denied, false)
	expired := id(t, s.ask("k-late", Operation{Op: DNDSet, Enabled: true}))
	late := id(t, s.ask("k-later", dial("1002")))
	s.clock = s.clock.Add(ApprovalWindow + time.Second)
	// An approval that comes after the time is up runs nothing.
	s.Decide(late, true)
	s.Tick(s.clock)
	if len(s.Pending()) != 0 || len(s.engine.take()) != 0 {
		t.Errorf("after a denial and two expiries: %d waiting", len(s.Pending()))
	}
	for operation, want := range map[string]string{denied: "denied", expired: "expired", late: "expired"} {
		if state, _, _ := s.state(t, operation); state != want {
			t.Errorf("%s is %s, want %s", operation, state, want)
		}
	}
}

func TestCallsMustMatchAndStorageGatesMutations(t *testing.T) {
	s := newSession(t, true)
	s.established(3, 5)
	if r := s.ask("k-v", Operation{Op: CallHangup, CallID: 3, ExpectedVersion: 4}); code(r) != NotFound {
		t.Errorf("a call at a version the client did not see: %+v", r)
	}
	if r := s.ask("k-w", Operation{Op: CallHangup, CallID: 4, ExpectedVersion: 5}); code(r) != NotFound {
		t.Errorf("another call: %+v", r)
	}
	if len(s.Pending()) != 0 {
		t.Error("something was recorded for a call that is not the one")
	}
	// The call changes between the request and the approval: nothing is sent.
	hangup := id(t, s.ask("k-h", Operation{Op: CallHangup, CallID: 3, ExpectedVersion: 5}))
	s.established(3, 6)
	s.Decide(hangup, true)
	if state, outcome, _ := s.state(t, hangup); state != "failed" || !strings.Contains(outcome, "ended or changed") || len(s.engine.take()) != 0 {
		t.Errorf("approved after the call changed: %s %s", state, outcome)
	}
	// The call as the client saw it: each operation is its command.
	for i, c := range []struct {
		op   Operation
		want string
	}{
		{Operation{Op: CallMute, Muted: true}, "SetMute(3, true)"},
		{Operation{Op: CallHold, Held: true}, "Hold(3)"},
		{Operation{Op: CallHold, Held: false}, "Resume(3)"},
		{Operation{Op: CallTransfer, Target: "1003"}, "Transfer(3, 1003)"},
		{Operation{Op: CallAnswer}, "Answer(3)"},
		{Operation{Op: CallReject}, "Reject(3)"},
		{Operation{Op: CallHangup}, "Hangup(3)"},
	} {
		c.op.CallID, c.op.ExpectedVersion = 3, 6
		operation := id(t, s.ask(fmt.Sprintf("k-c%d", i), c.op))
		s.Decide(operation, true)
		if sent := s.engine.take(); !equal(sent, c.want) {
			t.Errorf("%s sent %v, want %s", c.op.Op, sent, c.want)
		}
	}
	// A command the engine does not take is a failure, said so.
	s.engine.refuse = true
	refused := id(t, s.ask("k-r", dial("1002")))
	s.Decide(refused, true)
	if state, outcome, _ := s.state(t, refused); state != "failed" || !strings.Contains(outcome, "refused") {
		t.Errorf("refused by the engine: %s %s", state, outcome)
	}
	// Without a database nothing is changed, and status still answers.
	volatile := newSession(t, false)
	if r := volatile.ask("k-m", dial("1002")); code(r) != StorageUnavailable {
		t.Errorf("a mutation without a ledger: %+v", r)
	}
	if r := volatile.ask("", Operation{Op: StatusGet}); r.Status != StatusOK {
		t.Errorf("status without a ledger: %+v", r)
	}
	if _, err := volatile.Issue(GrantSpec{Scopes: []string{"control:dnd"}, Minutes: 5}); err == nil {
		t.Error("a grant was issued without a database")
	}
}

func TestKeysFromAnEarlierEpochAreStaleAndNeverRerun(t *testing.T) {
	s := newSession(t, true)
	old := id(t, s.ask("k-old", dial("1002")))
	first := s.Epoch()
	s.restart(t) // a new epoch on the same database, a new key in memory
	if s.Epoch() != first+1 {
		t.Fatalf("the epoch after a restart: %d, was %d", s.Epoch(), first)
	}
	r := s.ask("k-old", dial("1002"))
	if code(r) != StaleEpoch || id(t, r) != old {
		t.Errorf("a key of the earlier epoch: %+v", r)
	}
	if state, _, current := s.state(t, old); state != "expired" || current {
		t.Errorf("the earlier operation: %s, current epoch %v", state, current)
	}
	if len(s.Pending()) != 0 || len(s.engine.take()) != 0 {
		t.Error("the earlier request waits or ran again")
	}
}

func TestDTMFIsPacedAndDigitsAreNotStored(t *testing.T) {
	s := newSession(t, true)
	s.established(7, 1)
	operation := id(t, s.ask("k-d", Operation{Op: CallDTMF, CallID: 7, ExpectedVersion: 1, Digits: "90#"}))
	if pending := s.Pending(); len(pending) != 1 || strings.Contains(pending[0].Description, "90#") || !strings.Contains(pending[0].Description, "3 DTMF") {
		t.Errorf("the prompt of a DTMF request: %+v", pending)
	}
	start := time.Now()
	s.Decide(operation, true)
	wait := func(operation string) (state, outcome string) {
		for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
			if state, outcome, _ = s.state(t, operation); state != "executing" || time.Now().After(deadline) {
				return state, outcome
			}
		}
	}
	state, outcome := wait(operation)
	if state != "succeeded" || outcome != `{"sent":3}` {
		t.Errorf("the DTMF operation: %s %s", state, outcome)
	}
	if took := time.Since(start); took < 2*s.gap {
		t.Errorf("three digits went out in %v, less than two gaps of %v", took, s.gap)
	}
	if sent := s.engine.take(); !equal(sent, "Dtmf(7, 9)", "Dtmf(7, 0)", "Dtmf(7, #)") {
		t.Errorf("the digits sent: %v", sent)
	}
	record, _, _ := s.engine.store.Operation(operation)
	if stored := fmt.Sprintf("%+v", record); strings.Contains(stored, "90#") {
		t.Errorf("the ledger holds the digits: %s", stored)
	}
	for _, row := range s.AuditRows(10) {
		if strings.Contains(row.Detail, "90#") {
			t.Errorf("the audit holds the digits: %+v", row)
		}
	}
	// The call ends while digits go out: the rest is dropped, and how many went is said.
	s.gap = 50 * time.Millisecond
	cut := id(t, s.ask("k-cut", Operation{Op: CallDTMF, CallID: 7, ExpectedVersion: 1, Digits: "123456"}))
	s.Decide(cut, true)
	time.Sleep(70 * time.Millisecond)
	s.Observe(domain.CallChanged{Call: domain.CallSnapshot{ID: 7, Version: 2, Phase: domain.EndedWith(domain.EndFailed)}})
	state, outcome = wait(cut)
	if sent := s.engine.take(); state != "failed" || len(sent) == 0 || len(sent) >= 6 || outcome != fmt.Sprintf(`{"reason":"the call ended","sent":%d}`, len(sent)) {
		t.Errorf("DTMF cut by the end of the call: %s %s, sent %v", state, outcome, sent)
	}
}

func TestAKeyPastItsRetentionIsAConflict(t *testing.T) {
	s := newSession(t, true)
	// A key recorded in this session but past retention is a conflict, not a storage error.
	if err := s.engine.store.InsertOperation(app.OperationRecord{OperationID: "op-old", Epoch: s.Epoch(), IdempotencyKey: "k-ancient",
		Operation: CallDial, PayloadMAC: make([]byte, 32), State: app.OpSucceeded, CreatedUTC: 1}); err != nil {
		t.Fatal(err)
	}
	if r := s.ask("k-ancient", dial("1002")); code(r) != Conflict {
		t.Errorf("a key past its retention: %+v", r)
	}
}

func TestDenyAllPausesControlAndOrphansAreNotPromisedAgain(t *testing.T) {
	s := newSession(t, true)
	for _, key := range []string{"k-a", "k-b"} {
		s.ask(key, dial("1002"))
	}
	s.DenyAll()
	if len(s.Pending()) != 0 || len(s.engine.take()) != 0 {
		t.Error("something waits or ran after deny all")
	}
	if paused := s.ask("k-c", dial("1002")); code(paused) != Busy || !paused.Error.Retryable {
		t.Errorf("a mutation while paused: %+v", paused)
	}
	if r := s.ask("", Operation{Op: StatusGet}); r.Status != StatusOK {
		t.Errorf("status while paused: %+v", r)
	}
	s.clock = s.clock.Add(PauseAfterDenyAll + time.Second)
	resumed := s.ask("k-d", dial("1002"))
	if resumed.Status != StatusApprovalRequired {
		t.Fatalf("after the pause: %+v", resumed)
	}
	// A ledger row that waits for approval with no prompt behind it (the prompt was lost) is
	// said so, never shown as still pending.
	s.mu.Lock()
	s.pending = nil
	s.mu.Unlock()
	if orphan := s.ask("k-d", dial("1002")); code(orphan) != StorageUnavailable {
		t.Errorf("a request whose prompt was lost: %+v", orphan)
	}
	// At most MaxPending wait at once.
	for i := range MaxPending {
		if r := s.ask(fmt.Sprintf("k-many-%d", i), dial("1002")); r.Status != StatusApprovalRequired {
			t.Fatalf("request %d: %+v", i, r)
		}
	}
	if r := s.ask("k-one-more", dial("1002")); code(r) != Busy {
		t.Errorf("one request over the limit: %+v", r)
	}
}

func spec(scopes []string, destinations ...string) GrantSpec {
	return GrantSpec{Scopes: scopes, Destinations: destinations, Minutes: 10, Quota: 1}
}

func TestGrantsRunOnlyWhatTheyCoverAndNeverPrompt(t *testing.T) {
	s := newSession(t, true)
	issued, err := s.Issue(spec([]string{"control:dial", "read:history"}, "1002"))
	if err != nil {
		t.Fatal(err)
	}
	capability := issued.Capability
	if len(capability) != 64 || !Token(capability, 128) || strings.Contains(issued.String(), capability) {
		t.Fatalf("the capability: %d characters; printed as %s", len(capability), issued)
	}
	if grants := s.Grants(); len(grants) != 1 || grants[0].ID != issued.ID {
		t.Errorf("the grants listed: %+v", grants)
	}
	// Covered: it runs without a prompt.
	ok := s.with(capability, "g-1", dial("1002"))
	if ok.Status != StatusAccepted || len(s.Pending()) != 0 {
		t.Fatalf("a granted dial: %+v (%+v)", ok, ok.Error)
	}
	if sent := s.engine.take(); !equal(sent, "Dial(1002)") {
		t.Errorf("a granted dial sent %v", sent)
	}
	if state, _, _ := s.state(t, id(t, ok)); state != "succeeded" {
		t.Errorf("the granted dial is %s", state)
	}
	// A quota of one call: the next dial goes to a person in the window.
	if again := s.with(capability, "g-2", dial("1002")); again.Status != StatusApprovalRequired {
		t.Errorf("a dial over the quota: %+v", again)
	}
	// A destination outside the grant: a prompt, never a silent call.
	if other := s.with(capability, "g-3", dial("1003")); other.Status != StatusApprovalRequired {
		t.Errorf("a dial outside the grant: %+v", other)
	}
	if sent := s.engine.take(); len(sent) != 0 {
		t.Errorf("sent without the grant covering it: %v", sent)
	}
	// Reads within the scope work; others do not.
	limit := uint32(5)
	if history := s.with(capability, "", Operation{Op: HistoryList, Limit: &limit}); history.Status != StatusOK {
		t.Errorf("history with read:history: %+v", history)
	}
	if contacts := s.with(capability, "", Operation{Op: ContactsSearch}); code(contacts) != GrantRequired {
		t.Errorf("contacts without read:contacts: %+v", contacts)
	}
	// An unknown capability is refused outright, for reads and for mutations: no prompt.
	unknown := strings.Repeat("0", 64)
	if fake := s.with(unknown, "", Operation{Op: ConfigShow}); code(fake) != GrantRequired {
		t.Errorf("a read with an unknown capability: %+v", fake)
	}
	waiting := len(s.Pending())
	if fake := s.with(unknown, "g-4", dial("1002")); code(fake) != GrantRequired || len(s.Pending()) != waiting {
		t.Errorf("a mutation with an unknown capability: %+v", fake)
	}
	s.Revoke(issued.ID)
	if revoked := s.with(capability, "", Operation{Op: HistoryList}); code(revoked) != GrantRequired {
		t.Errorf("a read with a revoked grant: %+v", revoked)
	}
	if len(s.Grants()) != 0 {
		t.Error("a revoked grant is listed")
	}
	// A grant of an earlier session is not one of this session.
	later, err := s.Issue(spec([]string{"read:config"}))
	if err != nil {
		t.Fatal(err)
	}
	s.restart(t)
	if stale := s.with(later.Capability, "", Operation{Op: ConfigShow}); code(stale) != GrantRequired {
		t.Errorf("a grant of the session before: %+v", stale)
	}
	// An expired grant is refused.
	short, err := s.Issue(GrantSpec{Scopes: []string{"read:config"}, Minutes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if r := s.with(short.Capability, "", Operation{Op: ConfigShow}); r.Status != StatusOK {
		t.Errorf("config with read:config: %+v", r)
	}
	// Bad grants are refused and nothing is stored.
	before := len(s.Grants())
	long := spec([]string{"control:dnd"})
	long.Minutes = 61
	many := spec([]string{"control:dnd"})
	many.Quota = 101
	for name, bad := range map[string]GrantSpec{"an unknown scope": spec([]string{"control:shell"}), "no scope": spec(nil), "61 minutes": long,
		"a quota of 101": many, "a number with a space": spec([]string{"control:dial"}, "10 02"), "a number to trim": spec([]string{"control:dial"}, " 1002")} {
		if _, err := s.Issue(bad); err == nil {
			t.Errorf("a grant with %s was issued", name)
		}
	}
	if len(s.Grants()) != before {
		t.Error("a refused grant was stored")
	}
}

func TestAGrantAnswersOnlyTheCallersItNames(t *testing.T) {
	s := newSession(t, true)
	issued, err := s.Issue(GrantSpec{Scopes: []string{"control:answer", "control:reject", "control:dnd"}, Incoming: []string{"1002"}, Minutes: 5, Quota: 2})
	if err != nil {
		t.Fatal(err)
	}
	ringing := func(call uint64, from string) {
		s.Observe(domain.CallChanged{Call: domain.CallSnapshot{ID: domain.CallID(call), Version: 1, Direction: domain.Incoming, Remote: from,
			Phase: domain.CallPhase{Kind: domain.Ringing}}})
	}
	ringing(5, "1002")
	if r := s.with(issued.Capability, "a-1", Operation{Op: CallAnswer, CallID: 5, ExpectedVersion: 1}); r.Status != StatusAccepted {
		t.Errorf("answering a caller of the grant: %+v (%+v)", r, r.Error)
	}
	ringing(6, "1009")
	if r := s.with(issued.Capability, "a-2", Operation{Op: CallAnswer, CallID: 6, ExpectedVersion: 1}); r.Status != StatusApprovalRequired {
		t.Errorf("answering a caller outside the grant: %+v", r)
	}
	if r := s.with(issued.Capability, "a-3", Operation{Op: DNDSet, Enabled: true}); r.Status != StatusAccepted || !s.engine.dnd {
		t.Errorf("do-not-disturb by grant: %+v", r)
	}
	if sent := s.engine.take(); !equal(sent, "Answer(5)", "SetDnd(true)") {
		t.Errorf("sent by the grant: %v", sent)
	}
	// The grant's use is audited with its id, not its capability.
	found := false
	for _, row := range s.AuditRows(20) {
		found = found || (row.Decision == "granted" && strings.Contains(row.Detail, issued.ID))
		if strings.Contains(row.Detail, issued.Capability) {
			t.Errorf("the audit holds a capability: %+v", row)
		}
	}
	if !found {
		t.Error("the grant's use is not in the audit")
	}
}

func TestDenyAllAlsoStopsGrantsAndTheAuditIsCleared(t *testing.T) {
	s := newSession(t, true)
	wide := spec([]string{"control:dial", "control:dnd"}, "1002")
	wide.Quota = 5
	issued, err := s.Issue(wide)
	if err != nil {
		t.Fatal(err)
	}
	s.DenyAll()
	if paused := s.with(issued.Capability, "q-2", Operation{Op: DNDSet, Enabled: true}); code(paused) != Busy {
		t.Errorf("a granted mutation while paused: %+v", paused)
	}
	if sent := s.engine.take(); len(sent) != 0 {
		t.Errorf("sent while paused: %v", sent)
	}
	if len(s.AuditRows(10)) == 0 {
		t.Fatal("nothing in the audit")
	}
	s.ClearAudit()
	if rows := s.AuditRows(10); len(rows) != 0 {
		t.Errorf("the audit after it was cleared: %+v", rows)
	}
}

func TestGrantedReadsArePagedAndHoldNoSecret(t *testing.T) {
	s := newSession(t, true)
	for i := range 5 {
		if _, err := s.engine.store.AddContact(store.NewContact{Name: fmt.Sprintf("Fictional %d", i), Favorite: i == 0,
			Numbers: []store.ContactNumber{{Number: fmt.Sprintf("100%d", i), Label: "work"}}}); err != nil {
			t.Fatal(err)
		}
		ended, duration := int64(1700000100+i), int64(4000)
		if _, err := s.engine.store.RecordCall(store.CallRecord{SessionKey: fmt.Sprintf("s:%d", i), StartedUTC: int64(1700000000 + i), EndedUTC: &ended,
			Direction: domain.Incoming, Remote: fmt.Sprintf("100%d", i), Outcome: store.Missed, DurationMS: &duration}); err != nil {
			t.Fatal(err)
		}
	}
	s.engine.account = &domain.AccountSettings{Username: "1001", Domain: "pbx.invalid", Server: "pbx.invalid", Port: 5061, Transport: domain.TLS, Media: domain.SRTP, Expires: 300}
	issued, err := s.Issue(spec([]string{"read:contacts", "read:history", "read:devices", "read:config"}))
	if err != nil {
		t.Fatal(err)
	}
	limit, cursor := uint32(2), "o:2"
	read := func(op Operation) string {
		t.Helper()
		r := s.with(issued.Capability, "", op)
		if r.Status != StatusOK {
			t.Fatalf("%s: %+v", op.Op, r.Error)
		}
		return string(r.Data)
	}
	first := read(Operation{Op: ContactsSearch, Limit: &limit})
	if !strings.Contains(first, `"next_cursor":"o:2"`) || strings.Count(first, `"name"`) != 2 || !strings.Contains(first, `{"name":"Fictional 0","favorite":true,"numbers":[{"number":"1000","label":"work"}]}`) {
		t.Errorf("the first page of contacts: %s", first)
	}
	if second := read(Operation{Op: ContactsSearch, Limit: &limit, Cursor: &cursor}); !strings.Contains(second, `"next_cursor":"o:4"`) || strings.Count(second, `"name"`) != 2 {
		t.Errorf("the second page of contacts: %s", second)
	}
	last := "o:4"
	if third := read(Operation{Op: ContactsSearch, Limit: &limit, Cursor: &last}); !strings.Contains(third, `"next_cursor":null`) || strings.Count(third, `"name"`) != 1 {
		t.Errorf("the last page of contacts: %s", third)
	}
	if one := read(Operation{Op: ContactsSearch, Query: "Fictional 3"}); strings.Count(one, `"name"`) != 1 {
		t.Errorf("a search of contacts: %s", one)
	}
	missed, outgoing := FilterMissed, FilterOutgoing
	if history := read(Operation{Op: HistoryList, Filter: &missed, Limit: &limit}); !strings.Contains(history, `"next_cursor":"o:2"`) ||
		!strings.Contains(history, `{"direction":"incoming","remote":"1004","outcome":"missed","started_utc":1700000004,"duration_ms":4000,"secure":false}`) {
		t.Errorf("the history: %s", history)
	}
	if none := read(Operation{Op: HistoryList, Filter: &outgoing}); none != `{"items":[],"next_cursor":null}` {
		t.Errorf("the history of calls placed: %s", none)
	}
	if devices := read(Operation{Op: DevicesList}); devices != `{"input":"mic-1","output":"","available":{"inputs":["Fictional microphone"],"outputs":["Fictional speaker"]}}` {
		t.Errorf("the devices: %s", devices)
	}
	if config := read(Operation{Op: ConfigShow}); config != `{"account":{"username":"1001","domain":"pbx.invalid","server":"pbx.invalid","port":5061,"transport":"tls","media":"srtp","expires":300},"dnd":false,"theme":"dark"}` {
		t.Errorf("the configuration: %s", config)
	}
	for _, bad := range []string{"x:2", "o:-1", "o:10001", "o:"} {
		if r := s.with(issued.Capability, "", Operation{Op: ContactsSearch, Cursor: &bad}); code(r) != Validation {
			t.Errorf("the cursor %q: %+v", bad, r)
		}
	}
}

func TestMACsBindRequestsAndIDsAreUnique(t *testing.T) {
	s := newSession(t, true)
	a, b := dial("1002"), dial("1003")
	if string(s.mac(a)) != string(s.mac(a)) || string(s.mac(a)) == string(s.mac(b)) || len(s.mac(a)) != 32 {
		t.Error("the MAC does not bind the request")
	}
	other := newSession(t, true)
	if string(other.mac(a)) == string(s.mac(a)) {
		t.Error("two starts share a key")
	}
	x, _ := s.newID("op")
	y, _ := s.newID("op")
	if !strings.HasPrefix(x, fmt.Sprintf("op-%d-", s.Epoch())) || x == y || !Token(x, 64) {
		t.Errorf("operation ids: %s, %s", x, y)
	}
	dtmf := Operation{Op: CallDTMF, CallID: 1, ExpectedVersion: 1, Digits: "4321#"}
	if auditDetail(dtmf) != "5 digit(s)" || strings.Contains(describe(dtmf), "4321") {
		t.Errorf("DTMF in words: %q, %q", auditDetail(dtmf), describe(dtmf))
	}
	long := dial("5511998877")
	if auditDetail(long) != "destination ***77" || describe(long) != "call 5511998877" || masked("7") != "***7" {
		t.Errorf("a dial in words: %q, %q", auditDetail(long), describe(long))
	}
}

// service is a headless owner on the session's database: the window issued a bootstrap for it.
func (s *session) service(t *testing.T, spec GrantSpec) (IssuedGrant, error) {
	t.Helper()
	issued, err := s.IssueBootstrap(spec)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := NewServiceOwner(s.engine, issued.BootstrapID, issued.Capability)
	if err != nil {
		return issued, err
	}
	owner.now = func() time.Time { return s.clock }
	s.Owner = owner
	return issued, nil
}

func TestTheServiceRunsOnlyWhatItsBootstrapGrants(t *testing.T) {
	s := newSession(t, true)
	window := s.Epoch()
	issued, err := s.service(t, spec([]string{"control:dnd", "control:dial"}, "1002"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Epoch() != window+1 || s.Welcome().Mode != ModeService || s.Welcome().Epoch != uint64(s.Epoch()) {
		t.Errorf("the service's session: epoch %d after %d, welcome %+v", s.Epoch(), window, s.Welcome())
	}
	if wire, _ := json.Marshal(s.ask("", Operation{Op: StatusGet})); !strings.Contains(string(wire), `"mode":"service"`) {
		t.Errorf("the service's status: %s", wire)
	}
	if on := s.with(issued.Capability, "s-1", Operation{Op: DNDSet, Enabled: true}); on.Status != StatusAccepted {
		t.Fatalf("what the bootstrap grants: %+v (%+v)", on, on.Error)
	}
	if sent := s.engine.take(); !equal(sent, "SetDnd(true)") {
		t.Errorf("the service sent %v", sent)
	}
	// What the grant does not cover is refused: nobody is there to ask.
	s.established(3, 1)
	for name, r := range map[string]Response{
		"a destination outside the grant": s.with(issued.Capability, "s-2", dial("1003")),
		"an operation outside the grant":  s.with(issued.Capability, "s-3", Operation{Op: CallMute, CallID: 3, ExpectedVersion: 1, Muted: true}),
		"no grant at all":                 s.ask("s-4", dial("1002")),
		"an unknown capability":           s.with(strings.Repeat("0", 64), "s-5", dial("1002")),
	} {
		if code(r) != GrantRequired {
			t.Errorf("%s: %+v", name, r)
		}
	}
	// The quota of one call: the first dial runs, the second is refused, not asked about.
	if r := s.with(issued.Capability, "s-6", dial("1002")); r.Status != StatusAccepted {
		t.Errorf("a dial the bootstrap grants: %+v (%+v)", r, r.Error)
	}
	if r := s.with(issued.Capability, "s-7", dial("1002")); code(r) != GrantRequired {
		t.Errorf("a dial over the quota: %+v", r)
	}
	if len(s.Pending()) != 0 {
		t.Errorf("the service waits for a person: %+v", s.Pending())
	}
	if sent := s.engine.take(); !equal(sent, "Dial(1002)") {
		t.Errorf("the service sent %v", sent)
	}
	// The bootstrap starts one session only.
	if _, err := NewServiceOwner(s.engine, issued.BootstrapID, issued.Capability); !errors.Is(err, ErrBootstrapConsumed) {
		t.Errorf("the bootstrap used a second time: %v", err)
	}
	if _, err := NewServiceOwner(s.engine, "boot-0-unknown", issued.Capability); !errors.Is(err, ErrBootstrapInvalid) {
		t.Errorf("an unknown bootstrap: %v", err)
	}
	if _, err := NewServiceOwner(&engine{}, issued.BootstrapID, issued.Capability); !errors.Is(err, ErrNoLedger) {
		t.Errorf("a service without a database: %v", err)
	}
}

func TestABootstrapIsOfNoUseToTheWindowThatIssuedIt(t *testing.T) {
	s := newSession(t, true)
	issued, err := s.IssueBootstrap(spec([]string{"control:dnd"}))
	if err != nil {
		t.Fatal(err)
	}
	if issued.BootstrapID == "" || !Token(issued.BootstrapID, 128) || strings.Contains(issued.String(), issued.Capability) {
		t.Fatalf("the bootstrap issued: %s, id %q", issued, issued.BootstrapID)
	}
	// Listed in the window until it is consumed, and revocable there; not a grant of the window.
	if grants := s.Grants(); len(grants) != 1 || grants[0].BootstrapID != issued.BootstrapID {
		t.Errorf("the bootstrap in the window's list: %+v", grants)
	}
	if r := s.with(issued.Capability, "w-1", Operation{Op: DNDSet, Enabled: true}); code(r) != GrantRequired {
		t.Errorf("the bootstrap's capability used at the window: %+v", r)
	}
	s.Revoke(issued.ID)
	if _, err := NewServiceOwner(s.engine, issued.BootstrapID, issued.Capability); !errors.Is(err, ErrBootstrapInvalid) {
		t.Errorf("a revoked bootstrap: %v", err)
	}
	// Its file holds the id and the capability; a session's grant holds the capability alone.
	name, content := issued.File()
	if id, capability, err := bootstrap(content); !strings.HasSuffix(name, ".json") || err != nil || id != issued.BootstrapID || capability != issued.Capability {
		t.Errorf("the bootstrap's file %s: %v", name, err)
	}
	plain, err := s.Issue(spec([]string{"control:dnd"}))
	if err != nil {
		t.Fatal(err)
	}
	if name, content := plain.File(); name != plain.ID+".grant" || content != plain.Capability+"\n" {
		t.Errorf("a session's grant file: %s", name)
	}
}

// brokenLedger is a ledger whose revocations fail, as on a full disk; everything else works.
type brokenLedger struct{ app.Ledger }

func (brokenLedger) RevokeGrant(string) error { return errors.New("the disk is full") }

// A revocation the database does not take is said so, and the grant is refused by this owner
// all the same: Revoke never leaves a capability usable while reporting that it is gone.
func TestARevocationThatIsNotStoredStillRefusesTheGrant(t *testing.T) {
	s := newSession(t, true)
	issued, err := s.Issue(spec([]string{"read:history", "control:dnd"}))
	if err != nil {
		t.Fatal(err)
	}
	if r := s.with(issued.Capability, "", Operation{Op: HistoryList}); r.Status != StatusOK {
		t.Fatalf("history by the grant: %+v", r.Error)
	}
	s.mu.Lock()
	s.ledger = brokenLedger{s.ledger}
	s.mu.Unlock()
	if err := s.Revoke(issued.ID); !errors.Is(err, ErrRevocationNotStored) {
		t.Fatalf("a revocation the database refused: %v", err)
	}
	// The database still says the grant is active; this owner does not use it.
	if _, active, _ := s.engine.store.ActiveGrant(capabilityHash(issued.Capability), s.Epoch()); !active {
		t.Fatal("the test's database took the revocation")
	}
	if r := s.with(issued.Capability, "", Operation{Op: HistoryList}); code(r) != GrantRequired {
		t.Errorf("a read with a grant whose revocation was not stored: %+v", r)
	}
	if r := s.with(issued.Capability, "r-1", Operation{Op: DNDSet, Enabled: true}); code(r) != GrantRequired || len(s.engine.take()) != 0 {
		t.Errorf("a change with a grant whose revocation was not stored: %+v", r)
	}
	if grants := s.Grants(); len(grants) != 0 {
		t.Errorf("the grant is still listed: %+v", grants)
	}
}

// An owner that was closed answers no request and waits for nobody; "deny all" on it returns.
func TestAClosedOwnerAnswersNothing(t *testing.T) {
	s := newSession(t, true)
	issued, err := s.Issue(spec([]string{"control:dnd"}))
	if err != nil {
		t.Fatal(err)
	}
	s.ask("k-wait", dial("1002"))
	s.Close()
	if len(s.Pending()) != 0 {
		t.Error("a closed owner still waits for a person")
	}
	done := make(chan struct{})
	go func() { s.DenyAll(); s.Decide("op-unknown", true); s.Tick(s.clock.Add(time.Hour)); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deny all on a closed owner did not return")
	}
	for name, r := range map[string]Response{"a granted change": s.with(issued.Capability, "c-1", Operation{Op: DNDSet, Enabled: true}),
		"a change": s.ask("c-2", dial("1002")), "status": s.ask("", Operation{Op: StatusGet})} {
		if code(r) != EngineUnavailable {
			t.Errorf("%s on a closed owner: %+v", name, r)
		}
	}
	if sent := s.engine.take(); len(sent) != 0 {
		t.Errorf("a closed owner sent %v", sent)
	}
}

// The listings page through everything, not through the first thousand records only.
func TestListingsPageBeyondAThousandRecords(t *testing.T) {
	s := newSession(t, true)
	for i := range 1005 {
		ended := int64(1700000100 + i)
		if _, err := s.engine.store.RecordCall(store.CallRecord{SessionKey: fmt.Sprintf("s:%d", i), StartedUTC: int64(1700000000 + i), EndedUTC: &ended,
			Direction: domain.Incoming, Remote: "1002", Outcome: store.Missed}); err != nil {
			t.Fatal(err)
		}
	}
	issued, err := s.Issue(spec([]string{"read:history"}))
	if err != nil {
		t.Fatal(err)
	}
	limit := uint32(20)
	page := func(cursor string) (items int, next string) {
		t.Helper()
		r := s.with(issued.Capability, "", Operation{Op: HistoryList, Cursor: &cursor, Limit: &limit})
		var data struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}
		if r.Status != StatusOK || json.Unmarshal(r.Data, &data) != nil {
			t.Fatalf("history at %s: %+v", cursor, r.Error)
		}
		if data.NextCursor != nil {
			next = *data.NextCursor
		}
		return len(data.Items), next
	}
	if items, next := page("o:980"); items != 20 || next != "o:1000" {
		t.Errorf("the page at 980: %d items, next %q", items, next)
	}
	if items, next := page("o:1000"); items != 5 || next != "" {
		t.Errorf("the page at 1000: %d items, next %q", items, next)
	}
}
