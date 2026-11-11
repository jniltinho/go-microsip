package store

import (
	"database/sql"
	"strings"
	"unicode"

	"github.com/jniltinho/go-microsip/internal/core/domain"
)

// Outcome is the final outcome of a call as history shows it.
type Outcome string

// Call outcomes.
const (
	Answered    Outcome = "answered"    // the call was established at some point
	Missed      Outcome = "missed"      // incoming, never established and not rejected locally
	Canceled    Outcome = "canceled"    // outgoing, canceled before the answer
	Rejected    Outcome = "rejected"    // rejected: locally when incoming, by the peer when outgoing
	BusyPeer    Outcome = "busy"        // the peer was busy
	NoAnswer    Outcome = "no_answer"   // the peer did not answer
	Failed      Outcome = "failed"      // negotiation, signaling or media failed before the answer
	Interrupted Outcome = "interrupted" // the process stopped before the call ended
)

// OutcomeFromEnd maps how the engine ended a call to its history outcome. A call that was ever
// established is Answered. An incoming call that never was is Missed unless the user rejected it.
func OutcomeFromEnd(direction domain.Direction, established bool, reason domain.EndReason) Outcome {
	switch {
	case established:
		return Answered
	case reason == domain.EndRejected:
		return Rejected
	case direction == domain.Incoming:
		return Missed
	case reason == domain.EndBusy:
		return BusyPeer
	case reason == domain.EndNoAnswer:
		return NoAnswer
	case reason == domain.EndNotAcceptable, reason == domain.EndFailed:
		return Failed
	}
	return Canceled
}

// CallRecord is one finished, or crash-recovered, call.
type CallRecord struct {
	// SessionKey is unique per call, such as "<engine session>:<call id>". Recording the same key
	// twice is a no-op.
	SessionKey string
	// StartedUTC is the start time in Unix seconds.
	StartedUTC int64
	// AnsweredUTC is when the call was established; nil when it never was.
	AnsweredUTC *int64
	// EndedUTC is the end time; nil when interrupted.
	EndedUTC  *int64
	Direction domain.Direction
	// Remote is the remote party's user part or sanitized display text.
	Remote  string
	Outcome Outcome
	// DurationMS is the established time only; nil when unknown.
	DurationMS *int64
	// Secure: the media was SRTP.
	Secure bool
}

// CallStart is a call that has just started, journaled so a crash still leaves a trace.
type CallStart struct {
	// SessionKey is the key later passed to FinishCall.
	SessionKey string
	StartedUTC int64
	Direction  domain.Direction
	Remote     string
	Secure     bool
}

// HistoryEntry is a stored call; ID is for DeleteCall.
type HistoryEntry struct {
	ID   int64
	Call CallRecord
}

// HistoryFilter narrows RecentCalls.
type HistoryFilter uint8

// History filters.
const (
	AllCalls HistoryFilter = iota
	IncomingCalls
	OutgoingCalls
	MissedCalls
)

func validateCall(sessionKey, remote string, duration *int64) error {
	if !textOK(sessionKey, MaxSessionKeyLen) {
		return invalid(SessionKey)
	}
	if len(remote) > MaxRemoteLen || strings.IndexFunc(remote, unicode.IsControl) >= 0 {
		return invalid(Remote)
	}
	if duration != nil && *duration < 0 {
		return invalid(Duration)
	}
	return nil
}

func changed(res sql.Result, err error) (int, error) {
	if err != nil {
		return 0, wrap(err)
	}
	n, err := res.RowsAffected()
	return int(n), wrap(err)
}

// RecordCall records one call. It reports false when the session key was already recorded; the
// existing entry is kept.
func (s *Store) RecordCall(c CallRecord) (bool, error) {
	if err := validateCall(c.SessionKey, c.Remote, c.DurationMS); err != nil {
		return false, err
	}
	n, err := changed(s.db.Exec(`INSERT OR IGNORE INTO calls (session_key, started_utc, answered_utc, ended_utc,
		direction, remote, outcome, duration_ms, secure) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.SessionKey, c.StartedUTC, c.AnsweredUTC, c.EndedUTC, c.Direction.String(), c.Remote, string(c.Outcome), c.DurationMS, c.Secure))
	return n == 1, err
}

// BeginCall journals a call as it starts: an open row with the outcome Interrupted and no end time,
// hidden from history until FinishCall completes it or RecoverInterrupted runs at the next start.
// It reports false when the key already exists.
func (s *Store) BeginCall(c CallStart) (bool, error) {
	if err := validateCall(c.SessionKey, c.Remote, nil); err != nil {
		return false, err
	}
	n, err := changed(s.db.Exec(`INSERT OR IGNORE INTO calls (session_key, started_utc, direction, remote, outcome, secure, open)
		VALUES (?, ?, ?, ?, 'interrupted', ?, 1)`, c.SessionKey, c.StartedUTC, c.Direction.String(), c.Remote, c.Secure))
	return n == 1, err
}

// FinishCall completes a journaled call with its final record (same session key). Without an open
// row it behaves like RecordCall, so a call is still stored exactly once. It reports false when the
// key was already final; the existing entry is kept.
func (s *Store) FinishCall(c CallRecord) (bool, error) {
	if err := validateCall(c.SessionKey, c.Remote, c.DurationMS); err != nil {
		return false, err
	}
	n, err := changed(s.db.Exec(`UPDATE calls SET started_utc = ?2, answered_utc = ?3, ended_utc = ?4, direction = ?5,
		remote = ?6, outcome = ?7, duration_ms = ?8, secure = ?9, open = 0 WHERE session_key = ?1 AND open = 1`,
		c.SessionKey, c.StartedUTC, c.AnsweredUTC, c.EndedUTC, c.Direction.String(), c.Remote, string(c.Outcome), c.DurationMS, c.Secure))
	if err != nil || n == 1 {
		return n == 1, err
	}
	return s.RecordCall(c)
}

// RecoverInterrupted closes every row an earlier process left open (a crash or a kill) as
// Interrupted, with no end time or duration, and returns how many. Call it once at startup, before
// any new call begins.
func (s *Store) RecoverInterrupted() (int, error) {
	return changed(s.db.Exec(`UPDATE calls SET open = 0, outcome = 'interrupted', ended_utc = NULL, duration_ms = NULL WHERE open = 1`))
}

// RecentCalls lists the newest calls first, at most limit, filtered and searched by remote party
// (a case-insensitive substring; wildcards are literal).
func (s *Store) RecentCalls(limit int, filter HistoryFilter, search string) ([]HistoryEntry, error) {
	var direction, outcome any
	switch filter {
	case IncomingCalls:
		direction = "incoming"
	case OutgoingCalls:
		direction = "outgoing"
	case MissedCalls:
		outcome = "missed"
	}
	rows, err := s.db.Query(`SELECT id, session_key, started_utc, answered_utc, ended_utc, direction, remote, outcome, duration_ms, secure
		FROM calls WHERE open = 0 AND (?1 IS NULL OR direction = ?1) AND (?2 IS NULL OR outcome = ?2) AND remote LIKE ?3 ESCAPE '\'
		ORDER BY started_utc DESC, id DESC LIMIT ?4`, direction, outcome, likePattern(strings.TrimSpace(search)), limit)
	if err != nil {
		return nil, wrap(err)
	}
	defer rows.Close()
	var out []HistoryEntry
	for rows.Next() {
		var e HistoryEntry
		var dir, result string
		if err := rows.Scan(&e.ID, &e.Call.SessionKey, &e.Call.StartedUTC, &e.Call.AnsweredUTC, &e.Call.EndedUTC, &dir,
			&e.Call.Remote, &result, &e.Call.DurationMS, &e.Call.Secure); err != nil {
			return nil, wrap(err)
		}
		if dir == "incoming" {
			e.Call.Direction = domain.Incoming
		}
		e.Call.Outcome = Outcome(result) // the CHECK constraint guarantees a known value
		out = append(out, e)
	}
	return out, wrap(rows.Err())
}

// found turns "no row changed" into NotFound.
func found(n int, err error) error {
	if err == nil && n == 0 {
		return &Error{Kind: NotFound}
	}
	return err
}

// DeleteCall deletes one history entry.
func (s *Store) DeleteCall(id int64) error {
	return found(changed(s.db.Exec("DELETE FROM calls WHERE id = ?", id)))
}

// ClearHistory deletes every history entry and returns how many. It is not forensic erasure of WAL
// pages or of existing backups.
func (s *Store) ClearHistory() (int, error) { return changed(s.db.Exec("DELETE FROM calls")) }

// PruneOlderThan deletes the calls that started more than days days ago and returns how many.
func (s *Store) PruneOlderThan(days int) (int, error) {
	return changed(s.db.Exec("DELETE FROM calls WHERE started_utc < ?", nowUTC()-int64(days)*86_400))
}
