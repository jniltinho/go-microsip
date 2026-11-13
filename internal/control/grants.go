package control

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jniltinho/go-microsip/internal/core/app"
	"github.com/jniltinho/go-microsip/internal/core/domain"
)

// Control by grant: reads of personal data, and mutations without a prompt for each, only within
// what a person granted in the window: scope, exact destinations or callers, call quota, expiry
// and the owner's epoch. Everything else falls back to the prompt.

// maxOffset bounds the offset cursors ("o:N"), so that a client cannot page for ever.
const maxOffset = 10000

func denied(message string) *Error { return fail(GrantRequired, message) }

// requestGrant returns the active grant of the request's capability. A capability that names no
// usable grant is an error: such a request is not turned into a prompt.
func (o *Owner) requestGrant(r Request) (app.Grant, bool, error) {
	if r.Capability == nil {
		return app.Grant{}, false, nil
	}
	if o.ledger == nil {
		return app.Grant{}, false, denied("grants are unavailable")
	}
	grant, found, err := o.ledger.ActiveGrant(capabilityHash(*r.Capability), o.epoch)
	switch {
	case err != nil:
		return app.Grant{}, false, fail(StorageUnavailable, "the grant store is unavailable")
	case !found, o.revoked[grant.ID]:
		return app.Grant{}, false, denied("the grant is unknown, expired, revoked or from another session")
	}
	return grant, true, nil
}

// page reads an offset cursor and a limit.
func page(op Operation) (offset, limit int, err error) {
	if op.Cursor != nil {
		n, found := strings.CutPrefix(*op.Cursor, "o:")
		offset, err = strconv.Atoi(n)
		if !found || err != nil || offset < 0 || offset > maxOffset {
			return 0, 0, invalid("cursor is not valid")
		}
	}
	limit = 20
	if op.Limit != nil {
		limit = min(int(*op.Limit), MaxPage)
	}
	return offset, limit, nil
}

// paged cuts one page out of rows and names the next one, when there is one.
func paged[T any](rows []T, offset, limit int) (items []T, next *string) {
	rows = rows[min(offset, len(rows)):]
	if len(rows) > limit {
		cursor := fmt.Sprintf("o:%d", offset+limit)
		return rows[:limit], &cursor
	}
	return rows, nil
}

type (
	listed[T any] struct {
		Items      []T     `json:"items"`
		NextCursor *string `json:"next_cursor"`
	}
	contactNumber struct {
		Number string `json:"number"`
		Label  string `json:"label"`
	}
	contactItem struct {
		Name     string          `json:"name"`
		Favorite bool            `json:"favorite"`
		Numbers  []contactNumber `json:"numbers"`
	}
	historyItem struct {
		Direction  string `json:"direction"`
		Remote     string `json:"remote"`
		Outcome    string `json:"outcome"`
		StartedUTC int64  `json:"started_utc"`
		DurationMS *int64 `json:"duration_ms"`
		Secure     bool   `json:"secure"`
	}
	available struct {
		Inputs  []string `json:"inputs"`
		Outputs []string `json:"outputs"`
	}
	devices struct {
		Input     string     `json:"input"`
		Output    string     `json:"output"`
		Available *available `json:"available"`
	}
	account struct {
		Username  string `json:"username"`
		Domain    string `json:"domain"`
		Server    string `json:"server"`
		Port      uint16 `json:"port"`
		Transport string `json:"transport"`
		Media     string `json:"media"`
		Expires   uint32 `json:"expires"`
	}
	configuration struct {
		Account *account `json:"account"`
		DND     bool     `json:"dnd"`
		Theme   string   `json:"theme"`
	}
)

// grantedRead is a read of personal data, which only a grant with its scope allows.
func (o *Owner) grantedRead(r Request) (any, error) {
	grant, granted, err := o.requestGrant(r)
	switch {
	case err != nil:
		return nil, err
	case !granted:
		return nil, denied("this read needs a read grant issued in the window")
	case !slices.Contains(grant.Scopes, scopeFor(r.Operation.Op)):
		return nil, denied("the grant does not cover this read")
	}
	op := r.Operation
	switch op.Op {
	case ContactsSearch:
		offset, limit, err := page(op)
		if err != nil {
			return nil, err
		}
		rows, err := o.phone.ContactsUpTo(op.Query, offset+limit+1) // one more: is there a next page
		if err != nil {
			return nil, errNoStorage
		}
		found, next := paged(rows, offset, limit)
		items := make([]contactItem, 0, len(found))
		for _, c := range found {
			item := contactItem{Name: c.Name, Favorite: c.Favorite, Numbers: []contactNumber{}}
			for _, n := range c.Numbers {
				item.Numbers = append(item.Numbers, contactNumber{n.Number, n.Label})
			}
			items = append(items, item)
		}
		return listed[contactItem]{items, next}, nil
	case HistoryList:
		offset, limit, err := page(op)
		if err != nil {
			return nil, err
		}
		filter := app.AllCalls
		if op.Filter != nil {
			filter = map[string]app.HistoryFilter{FilterIncoming: app.IncomingCalls, FilterOutgoing: app.OutgoingCalls, FilterMissed: app.MissedCalls}[*op.Filter]
		}
		rows, err := o.phone.HistoryUpTo(filter, "", offset+limit+1)
		if err != nil {
			return nil, errNoStorage
		}
		found, next := paged(rows, offset, limit)
		items := make([]historyItem, 0, len(found))
		for _, h := range found {
			c := h.Call
			items = append(items, historyItem{Direction: c.Direction.String(), Remote: c.Remote, Outcome: string(c.Outcome),
				StartedUTC: c.StartedUTC, DurationMS: c.DurationMS, Secure: c.Secure})
		}
		return listed[historyItem]{items, next}, nil
	case DevicesList:
		settings := o.phone.Settings()
		out := devices{Input: settings.AudioInput, Output: settings.AudioOutput}
		if inputs, outputs, err := o.phone.AudioDevices(); err == nil {
			out.Available = &available{Inputs: []string{}, Outputs: []string{}}
			for _, d := range inputs {
				out.Available.Inputs = append(out.Available.Inputs, d.Name)
			}
			for _, d := range outputs {
				out.Available.Outputs = append(out.Available.Outputs, d.Name)
			}
		}
		return out, nil
	case ConfigShow:
		settings := o.phone.Settings()
		out := configuration{DND: settings.DND, Theme: settings.Theme}
		if a := settings.Account; a != nil {
			out.Account = &account{Username: a.Username, Domain: a.Domain, Server: a.Server, Port: a.Port,
				Transport: a.Transport.String(), Media: a.Media.String(), Expires: a.Expires}
		}
		return out, nil
	}
	return nil, denied("not a read")
}

// grantAllows reports whether the grant authorizes this mutation right now; for dialing and
// answering it uses one call of the quota.
func (o *Owner) grantAllows(grant app.Grant, op Operation) error {
	if !slices.Contains(grant.Scopes, scopeFor(op.Op)) {
		return denied("the grant does not cover this operation")
	}
	switch op.Op {
	case CallDial, CallTransfer:
		if !slices.Contains(grant.Destinations, op.Target) {
			return denied("the destination is not in the grant")
		}
	case CallAnswer, CallReject:
		if o.call == nil || o.call.Direction != domain.Incoming || o.call.Phase.Kind != domain.Ringing {
			return fail(InvalidState, "no call is ringing")
		}
		if !slices.Contains(grant.Incoming, o.call.Remote) {
			return denied("the caller is not in the grant")
		}
	}
	if op.Op == CallDial || op.Op == CallAnswer {
		if within, err := o.ledger.UseGrantCall(grant.ID); err != nil {
			return denied("the grant store is unavailable")
		} else if !within {
			return denied("the grant's call quota is used up")
		}
	}
	return nil
}

// GrantSpec is what a person grants in the window.
type GrantSpec struct {
	// Scopes are some of Scopes.
	Scopes []string
	// Destinations are the exact numbers allowed for dialing and transfers.
	Destinations []string
	// Incoming are the exact callers that may be answered or rejected.
	Incoming []string
	// Minutes is the validity, 1 to 60.
	Minutes uint32
	// Quota is how many calls (dial or answer) are allowed in all, at most 100.
	Quota uint32
}

// IssuedGrant is a grant as issued. The capability is shown or written once and never stored.
type IssuedGrant struct {
	// ID is public.
	ID string
	// Capability is the secret: 64 hexadecimal digits.
	Capability string
	// BootstrapID is the single-use id of a grant exported for the headless service; empty for
	// a grant of this session.
	BootstrapID string
}

// String describes the grant without its capability.
func (g IssuedGrant) String() string { return g.ID + " [REDACTED]" }

// File is the grant as its private file holds it: the capability alone for a session's grant,
// which a client passes with --grant-file; the bootstrap id and the capability for the service's.
func (g IssuedGrant) File() (name, content string) {
	if g.BootstrapID != "" {
		return g.ID + ".json", fmt.Sprintf("{\"bootstrap_id\":%q,\"capability\":%q}\n", g.BootstrapID, g.Capability)
	}
	return g.ID + ".grant", g.Capability + "\n"
}

// Issue stores a grant for this session and returns its capability. The error is a reason a
// person can read; nothing was stored then.
func (o *Owner) Issue(spec GrantSpec) (IssuedGrant, error) { return o.issue(spec, false) }

// IssueBootstrap stores a single-use grant for the headless service: it is bound to no session
// until the service consumes it, once.
func (o *Owner) IssueBootstrap(spec GrantSpec) (IssuedGrant, error) { return o.issue(spec, true) }

func (o *Owner) issue(spec GrantSpec, bootstrap bool) (IssuedGrant, error) {
	if len(spec.Scopes) == 0 || slices.ContainsFunc(spec.Scopes, func(s string) bool { return !slices.Contains(Scopes, s) }) {
		return IssuedGrant{}, errors.New("choose at least one known permission")
	}
	if spec.Minutes < 1 || spec.Minutes > 60 || spec.Quota > 100 {
		return IssuedGrant{}, errors.New("validity must be 1 to 60 minutes and the call limit at most 100")
	}
	for _, number := range slices.Concat(spec.Destinations, spec.Incoming) {
		if target, err := domain.ParseDialTarget(number); err != nil || target.Value() != number {
			return IssuedGrant{}, errors.New("every number must be a valid number or SIP user")
		}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.ledger == nil {
		return IssuedGrant{}, errors.New("grants need the settings database")
	}
	id, err := o.newID("grant")
	if err != nil {
		return IssuedGrant{}, errors.New("no system randomness")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return IssuedGrant{}, errors.New("no system randomness")
	}
	capability := hex.EncodeToString(secret)
	issued := IssuedGrant{ID: id, Capability: capability}
	grant := app.Grant{ID: id, Scopes: spec.Scopes, Destinations: spec.Destinations, Incoming: spec.Incoming,
		ExpiresUTC: o.now().Unix() + int64(spec.Minutes)*60, Quota: spec.Quota, Epoch: o.epoch}
	decision := "issued"
	if bootstrap {
		if issued.BootstrapID, err = o.newID("boot"); err != nil {
			return IssuedGrant{}, errors.New("no system randomness")
		}
		grant.Epoch, grant.BootstrapID, decision = 0, issued.BootstrapID, "bootstrap issued"
	}
	if o.ledger.InsertGrant(grant, capabilityHash(capability)) != nil {
		return IssuedGrant{}, errors.New("the grant could not be stored")
	}
	_ = o.ledger.Audit("grant", decision, fmt.Sprintf("%d scopes, %d min", len(spec.Scopes), spec.Minutes))
	return issued, nil
}

// Grants lists the grants that can still be used, for the window. A grant of this session that
// was revoked here is not one, stored or not. An export for the headless service whose
// revocation the database did not take is: the service that takes it is another process, so it
// stays listed, to be revoked again.
func (o *Owner) Grants() []app.Grant {
	if o.ledger == nil {
		return nil
	}
	grants, _ := o.ledger.Grants(o.epoch)
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.DeleteFunc(grants, func(g app.Grant) bool { return o.revoked[g.ID] && g.BootstrapID == "" })
}

// ErrRevocationNotStored means the database did not take a revocation. This owner refuses the
// grant from then on all the same; a bootstrap exported for the headless service, which another
// process consumes, stays usable until it expires.
var ErrRevocationNotStored = errors.New("the revocation could not be stored: this window refuses the grant from now on, but an export for the headless service stays usable until it expires")

// Revoke ends a grant now. The grant is refused by this owner from the moment it is asked,
// whether or not the database takes the revocation; when it does not, the error says so.
func (o *Owner) Revoke(id string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.ledger == nil {
		return errors.New("grants need the settings database")
	}
	if o.revoked == nil {
		o.revoked = map[string]bool{}
	}
	o.revoked[id] = true
	if err := o.ledger.RevokeGrant(id); err != nil {
		return ErrRevocationNotStored
	}
	_ = o.ledger.Audit("grant", "revoked", "-")
	return nil
}

// AuditRows returns the audit trail, newest first.
func (o *Owner) AuditRows(limit int) []app.AuditRow {
	if o.ledger == nil {
		return nil
	}
	rows, _ := o.ledger.AuditRows(limit)
	return rows
}

// ClearAudit deletes the audit trail; the window asks the person first.
func (o *Owner) ClearAudit() {
	if o.ledger != nil {
		_, _ = o.ledger.ClearAudit()
	}
}
