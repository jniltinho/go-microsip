package store

import (
	"database/sql"
	"errors"
	"strings"
)

// The control ledger, its audit and the grants (the Rust candidate's AG-04 and AG-05).
//
// Every change an agent asks for over the control endpoint is recorded here before it has any
// effect, under the owner's epoch and the client's idempotency key. The request itself is kept
// only as a MAC the owner computes with a key that lives in memory for one epoch, so digits and
// numbers cannot be recovered from the database. Outcomes and audit details are sanitized by the
// caller; here, control characters and oversized text are refused.
//
// Each owner start opens a new epoch: operations still waiting for approval expire, and those
// that may have been sent become "outcome_unknown" and are never retried. A grant is stored as
// the SHA-256 of its capability and is bound to the epoch it was issued in. A bootstrap grant is
// the one exported for the headless service: it has no epoch until the service consumes its
// single-use id, in the service's own new epoch.

const (
	// LedgerRetentionSecs is how long an operation's outcome can be asked for.
	LedgerRetentionSecs = 24 * 60 * 60
	// AuditRetentionDays is how long audit rows are kept.
	AuditRetentionDays = 90
	// MaxGrantSecs is the longest a grant may be valid.
	MaxGrantSecs = 60 * 60

	maxControlText = 1024
	maxGrantList   = 64
)

// OperationState is where a recorded operation stands.
type OperationState string

// The states of an operation. The last five are final: they never change again.
const (
	OpAwaitingApproval OperationState = "awaiting_approval" // waiting for a person at the window
	OpExecuting        OperationState = "executing"         // approved or granted; about to be sent
	OpSucceeded        OperationState = "succeeded"         // handed to the engine, which took it
	OpFailed           OperationState = "failed"            // refused by the engine or the owner; no effect
	OpDenied           OperationState = "denied"            // a person denied it; no effect
	OpExpired          OperationState = "expired"           // nobody decided in time, or the owner restarted first
	OpOutcomeUnknown   OperationState = "outcome_unknown"   // the owner stopped after it may have been sent
)

// Final reports whether the operation can no longer change.
func (s OperationState) Final() bool { return s != OpAwaitingApproval && s != OpExecuting }

// OperationRecord is one recorded operation.
type OperationRecord struct {
	OperationID    string         // opaque, given to the client
	Epoch          int64          // the owner epoch that took it
	IdempotencyKey string         // the client's key
	Operation      string         // its name ("call_dial", ...)
	PayloadMAC     []byte         // MAC of the request under the epoch's key, 32 bytes
	State          OperationState // where it stands
	Outcome        string         // sanitized JSON, or empty
	CreatedUTC     int64          // Unix seconds
}

// AuditRow is one line of the audit trail.
type AuditRow struct {
	AtUTC                       int64
	Operation, Decision, Detail string
}

// Grant is a stored grant. The capability itself is never stored.
type Grant struct {
	ID           string   // public, shown in the window
	Scopes       []string // "read:contacts", "control:dial", ...
	Destinations []string // exact targets allowed for dialing and transfers
	Incoming     []string // exact callers that may be answered or rejected
	ExpiresUTC   int64    // Unix seconds
	Quota, Used  uint32   // calls (dial or answer) allowed, and used so far
	Epoch        int64    // the owner epoch it is valid in; 0 for a bootstrap not consumed yet
	BootstrapID  string   // the single-use id of a bootstrap grant; empty for a session's grant
	Revoked      bool
}

func controlText(values ...string) error {
	for _, v := range values {
		if !textOK(v, maxControlText) {
			return invalid(ControlText)
		}
	}
	return nil
}

// optionalText is controlText for a value that may be absent.
func optionalText(value string) error {
	if value == "" {
		return nil
	}
	return controlText(value)
}

// BeginControlEpoch opens a new owner epoch: waiting operations expire, those that may have been
// sent become outcome_unknown, and rows past their retention are dropped. On an error nothing
// changes.
func (s *Store) BeginControlEpoch() (epoch int64, err error) {
	now := nowUTC()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, wrap(err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = tx.QueryRow("SELECT COALESCE(MAX(epoch), 0) + 1 FROM control_epochs").Scan(&epoch); err != nil {
		return 0, wrap(err)
	}
	for _, step := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO control_epochs (epoch, started_utc) VALUES (?1, ?2)", []any{epoch, now}},
		{"UPDATE control_operations SET state = 'expired', updated_utc = ?1 WHERE state = 'awaiting_approval'", []any{now}},
		{"UPDATE control_operations SET state = 'outcome_unknown', updated_utc = ?1 WHERE state = 'executing'", []any{now}},
		{"DELETE FROM control_operations WHERE created_utc < ?1", []any{now - LedgerRetentionSecs}},
		{"DELETE FROM control_audit WHERE at_utc < ?1", []any{now - AuditRetentionDays*86400}},
	} {
		if _, err = tx.Exec(step.sql, step.args...); err != nil {
			return 0, wrap(err)
		}
	}
	return epoch, wrap(tx.Commit())
}

// FindOperationKey returns the newest record of an idempotency key within the retention window.
func (s *Store) FindOperationKey(key string) (OperationRecord, bool, error) {
	return s.operation("WHERE idempotency_key = ?1 AND created_utc >= ?2 ORDER BY epoch DESC LIMIT 1", key)
}

// OperationKeyUsed reports whether the key was ever recorded in the epoch, the retention window
// aside: that window limits only what is served.
func (s *Store) OperationKeyUsed(epoch int64, key string) (bool, error) {
	var one int
	err := s.db.QueryRow("SELECT 1 FROM control_operations WHERE epoch = ?1 AND idempotency_key = ?2", epoch, key).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, wrap(err)
}

// Operation returns a record by its id, within the retention window.
func (s *Store) Operation(id string) (OperationRecord, bool, error) {
	return s.operation("WHERE operation_id = ?1 AND created_utc >= ?2", id)
}

func (s *Store) operation(filter, value string) (OperationRecord, bool, error) {
	var r OperationRecord
	var outcome sql.NullString
	err := s.db.QueryRow(`SELECT operation_id, epoch, idempotency_key, operation, payload_mac, state, outcome, created_utc
		FROM control_operations `+filter, value, nowUTC()-LedgerRetentionSecs).
		Scan(&r.OperationID, &r.Epoch, &r.IdempotencyKey, &r.Operation, &r.PayloadMAC, &r.State, &outcome, &r.CreatedUTC)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	r.Outcome = outcome.String
	return r, err == nil, wrap(err)
}

// InsertOperation records a new operation, before it has any effect. The database refuses a
// second record of the same key in one epoch.
func (s *Store) InsertOperation(r OperationRecord) error {
	if err := controlText(r.OperationID, r.IdempotencyKey, r.Operation); err != nil {
		return err
	}
	if optionalText(r.Outcome) != nil || len(r.PayloadMAC) != 32 {
		return invalid(ControlText)
	}
	_, err := s.db.Exec(`INSERT INTO control_operations (operation_id, epoch, idempotency_key, operation, payload_mac, state,
		outcome, created_utc, updated_utc) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?8)`,
		r.OperationID, r.Epoch, r.IdempotencyKey, r.Operation, r.PayloadMAC, string(r.State), nullable(r.Outcome), r.CreatedUTC)
	return wrap(err)
}

// UpdateOperation moves an operation to a state; an empty outcome keeps the one recorded. A final
// state never changes again: NotFound then, as for an unknown operation.
func (s *Store) UpdateOperation(id string, state OperationState, outcome string) error {
	if err := optionalText(outcome); err != nil {
		return err
	}
	return found(changed(s.db.Exec(`UPDATE control_operations SET state = ?2, outcome = COALESCE(?3, outcome), updated_utc = ?4
		WHERE operation_id = ?1 AND state IN ('awaiting_approval', 'executing')`, id, string(state), nullable(outcome), nowUTC())))
}

// Audit appends a sanitized line to the audit trail.
func (s *Store) Audit(operation, decision, detail string) error {
	if err := controlText(operation, decision, detail); err != nil {
		return err
	}
	_, err := s.db.Exec("INSERT INTO control_audit (at_utc, operation, decision, detail) VALUES (?1, ?2, ?3, ?4)",
		nowUTC(), operation, decision, detail)
	return wrap(err)
}

// AuditRows returns the audit trail, newest first.
func (s *Store) AuditRows(limit int) ([]AuditRow, error) {
	rows, err := s.db.Query("SELECT at_utc, operation, decision, detail FROM control_audit ORDER BY at_utc DESC, id DESC LIMIT ?1", limit)
	if err != nil {
		return nil, wrap(err)
	}
	defer rows.Close()
	var out []AuditRow
	for rows.Next() {
		var r AuditRow
		if err := rows.Scan(&r.AtUTC, &r.Operation, &r.Decision, &r.Detail); err != nil {
			return nil, wrap(err)
		}
		out = append(out, r)
	}
	return out, wrap(rows.Err())
}

// ClearAudit deletes the whole audit trail and returns how many lines it had.
func (s *Store) ClearAudit() (int, error) { return changed(s.db.Exec("DELETE FROM control_audit")) }

func grantList(items []string) bool {
	if len(items) > maxGrantList {
		return false
	}
	for _, item := range items {
		if !textOK(item, 128) || strings.Contains(item, "\n") {
			return false
		}
	}
	return true
}

// InsertGrant stores a new grant under the SHA-256 of its capability. A validity longer than
// MaxGrantSecs, or one already over, is refused.
func (s *Store) InsertGrant(g Grant, capabilityHash []byte) error {
	now := nowUTC()
	if len(capabilityHash) != 32 || !textOK(g.ID, 64) || !grantList(g.Scopes) || !grantList(g.Destinations) ||
		!grantList(g.Incoming) || g.ExpiresUTC <= now || g.ExpiresUTC > now+MaxGrantSecs ||
		(g.BootstrapID != "" && !textOK(g.BootstrapID, 64)) || (g.BootstrapID == "") == (g.Epoch == 0) {
		return invalid(ControlText)
	}
	var epoch any // a bootstrap has none until it is consumed
	if g.BootstrapID == "" {
		epoch = g.Epoch
	}
	_, err := s.db.Exec(`INSERT INTO control_grants (id, capability_hash, scopes, destinations, incoming, expires_utc, quota,
		used, epoch, bootstrap_id, bootstrap_consumed, revoked, created_utc) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, 0, ?8, ?9, 0, 0, ?10)`,
		g.ID, capabilityHash, strings.Join(g.Scopes, "\n"), strings.Join(g.Destinations, "\n"), strings.Join(g.Incoming, "\n"),
		g.ExpiresUTC, g.Quota, epoch, nullable(g.BootstrapID), now)
	return wrap(err)
}

// ActiveGrant returns the grant of a capability hash when it can be used in the epoch now: not
// revoked, not expired, issued in that epoch.
func (s *Store) ActiveGrant(capabilityHash []byte, epoch int64) (Grant, bool, error) {
	grants, err := s.grants("WHERE capability_hash = ?1 AND epoch = ?2 AND revoked = 0 AND expires_utc > ?3",
		capabilityHash, epoch, nowUTC())
	if err != nil || len(grants) == 0 {
		return Grant{}, false, err
	}
	return grants[0], true, nil
}

// Grants lists the grants that can still be used in the epoch, and the bootstraps that can
// still be consumed, newest first.
func (s *Store) Grants(epoch int64) ([]Grant, error) {
	return s.grants(`WHERE (epoch = ?1 OR (epoch IS NULL AND bootstrap_consumed = 0)) AND revoked = 0 AND expires_utc > ?2
		ORDER BY created_utc DESC, id`, epoch, nowUTC())
}

// ConsumeBootstrap consumes a bootstrap once and binds it to the epoch. It reports false when
// the id is unknown, already consumed, revoked or expired, or the capability is not its own.
func (s *Store) ConsumeBootstrap(bootstrapID string, capabilityHash []byte, epoch int64) (Grant, bool, error) {
	n, err := changed(s.db.Exec(`UPDATE control_grants SET bootstrap_consumed = 1, epoch = ?3
		WHERE bootstrap_id = ?1 AND capability_hash = ?2 AND bootstrap_consumed = 0 AND revoked = 0 AND expires_utc > ?4`,
		bootstrapID, capabilityHash, epoch, nowUTC()))
	if err != nil || n == 0 {
		return Grant{}, false, err
	}
	return s.ActiveGrant(capabilityHash, epoch)
}

// BootstrapConsumed reports whether a bootstrap id was already consumed, for a precise error.
func (s *Store) BootstrapConsumed(bootstrapID string) (bool, error) {
	var consumed bool
	err := s.db.QueryRow("SELECT bootstrap_consumed FROM control_grants WHERE bootstrap_id = ?1", bootstrapID).Scan(&consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return consumed, wrap(err)
}

func (s *Store) grants(filter string, args ...any) ([]Grant, error) {
	rows, err := s.db.Query(`SELECT id, scopes, destinations, incoming, expires_utc, quota, used, COALESCE(epoch, 0),
		COALESCE(bootstrap_id, ''), revoked FROM control_grants `+filter, args...)
	if err != nil {
		return nil, wrap(err)
	}
	defer rows.Close()
	split := func(text string) []string {
		if text == "" {
			return nil
		}
		return strings.Split(text, "\n")
	}
	var out []Grant
	for rows.Next() {
		var g Grant
		var scopes, destinations, incoming string
		if err := rows.Scan(&g.ID, &scopes, &destinations, &incoming, &g.ExpiresUTC, &g.Quota, &g.Used, &g.Epoch, &g.BootstrapID, &g.Revoked); err != nil {
			return nil, wrap(err)
		}
		g.Scopes, g.Destinations, g.Incoming = split(scopes), split(destinations), split(incoming)
		out = append(out, g)
	}
	return out, wrap(rows.Err())
}

// UseGrantCall counts one call against the grant's quota; false when it is used up.
func (s *Store) UseGrantCall(id string) (bool, error) {
	n, err := changed(s.db.Exec("UPDATE control_grants SET used = used + 1 WHERE id = ?1 AND used < quota", id))
	return n == 1, err
}

// RevokeGrant revokes a grant.
func (s *Store) RevokeGrant(id string) error {
	return found(changed(s.db.Exec("UPDATE control_grants SET revoked = 1 WHERE id = ?1", id)))
}

// nullable stores an empty text as NULL.
func nullable(text string) any {
	if text == "" {
		return nil
	}
	return text
}
