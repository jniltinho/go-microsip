package store

import (
	"bytes"
	"errors"
	"testing"
)

// Ported from the Rust candidate's crates/storage/src/tests.rs (control ledger, audit, grants).

func operation(id string, epoch int64, key string, state OperationState) OperationRecord {
	return OperationRecord{OperationID: id, Epoch: epoch, IdempotencyKey: key, Operation: "call_dial",
		PayloadMAC: bytes.Repeat([]byte{7}, 32), State: state, CreatedUTC: nowUTC()}
}

func TestControlLedgerKeysEpochsAndFinalStates(t *testing.T) {
	s := memory(t)
	first, err := s.BeginControlEpoch()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []OperationRecord{
		operation("op-1", first, "k1", OpAwaitingApproval),
		operation("op-2", first, "k2", OpExecuting),
		operation("op-3", first, "k3", OpExecuting),
	} {
		if err := s.InsertOperation(r); err != nil {
			t.Fatal(err)
		}
	}
	// The same key twice in one epoch is refused by the database.
	if err := s.InsertOperation(operation("op-4", first, "k1", OpAwaitingApproval)); err == nil {
		t.Error("a second record of the same key in one epoch was taken")
	}
	if err := s.UpdateOperation("op-3", OpSucceeded, `{"call_id":4}`); err != nil {
		t.Fatal(err)
	}
	// Final states never change again.
	var failure *Error
	if err := s.UpdateOperation("op-3", OpFailed, ""); !errors.As(err, &failure) || failure.Kind != NotFound {
		t.Errorf("changing a final state: %v, want NotFound", err)
	}
	// A restart opens a new epoch: what waited expires, what may have been sent is unknown.
	second, err := s.BeginControlEpoch()
	if err != nil || second != first+1 {
		t.Fatalf("the second epoch: %d, %v", second, err)
	}
	for id, want := range map[string]OperationState{"op-1": OpExpired, "op-2": OpOutcomeUnknown, "op-3": OpSucceeded} {
		if r, ok, err := s.Operation(id); err != nil || !ok || r.State != want {
			t.Errorf("%s after the restart: %q (found %v, %v), want %q", id, r.State, ok, err, want)
		}
	}
	if r, _, _ := s.Operation("op-3"); r.Outcome != `{"call_id":4}` {
		t.Errorf("the outcome of op-3: %q", r.Outcome)
	}
	// The key is found in the old epoch (the owner answers stale_epoch), the newest record first.
	if err := s.InsertOperation(operation("op-5", second, "k1", OpExecuting)); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]int64{"k1": second, "k2": first} {
		if r, ok, err := s.FindOperationKey(key); err != nil || !ok || r.Epoch != want {
			t.Errorf("key %s: epoch %d (found %v, %v), want %d", key, r.Epoch, ok, err, want)
		}
	}
	if _, ok, err := s.FindOperationKey("missing"); ok || err != nil {
		t.Errorf("a key never used: found %v, %v", ok, err)
	}
	if used, err := s.OperationKeyUsed(first, "k2"); !used || err != nil {
		t.Errorf("k2 in its epoch: %v, %v", used, err)
	}
	if used, err := s.OperationKeyUsed(second, "k2"); used || err != nil {
		t.Errorf("k2 in another epoch: %v, %v", used, err)
	}
	// Bad text and a MAC of the wrong size are refused.
	bad := operation("op\n6", second, "k6", OpExecuting)
	if err := s.InsertOperation(bad); err == nil {
		t.Error("an id with a line break was taken")
	}
	bad.OperationID, bad.PayloadMAC = "op-6", bytes.Repeat([]byte{1}, 8)
	if err := s.InsertOperation(bad); err == nil {
		t.Error("a MAC of 8 bytes was taken")
	}
}

func TestControlRowsExpireAndTheAuditIsSanitizedAndClearable(t *testing.T) {
	s := memory(t)
	epoch, err := s.BeginControlEpoch()
	if err != nil {
		t.Fatal(err)
	}
	old := operation("old", epoch, "k-old", OpSucceeded)
	old.CreatedUTC = nowUTC() - LedgerRetentionSecs - 10
	if err := s.InsertOperation(old); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Operation("old"); ok || err != nil {
		t.Errorf("a record past its retention is served: %v, %v", ok, err)
	}
	if used, err := s.OperationKeyUsed(epoch, "k-old"); !used || err != nil {
		t.Errorf("its key no longer counts as used in its epoch: %v, %v", used, err)
	}
	if _, err := s.BeginControlEpoch(); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, "control_operations"); n != 0 {
		t.Errorf("%d operations after the next epoch, want the old one purged", n)
	}
	if err := s.Audit("call_dial", "approved", "destination ***02"); err != nil {
		t.Fatal(err)
	}
	if err := s.Audit("call_dtmf", "approved", "bad\a"); err == nil {
		t.Error("an audit detail with a control character was taken")
	}
	if rows, err := s.AuditRows(10); err != nil || len(rows) != 1 || rows[0].Detail != "destination ***02" {
		t.Errorf("the audit trail: %v, %v", rows, err)
	}
	if n, err := s.ClearAudit(); n != 1 || err != nil {
		t.Errorf("clearing the audit: %d, %v", n, err)
	}
	if rows, err := s.AuditRows(10); err != nil || len(rows) != 0 {
		t.Errorf("the audit trail after it was cleared: %v, %v", rows, err)
	}
}

func TestGrantsAreEpochBoundQuotaLimitedAndRevocable(t *testing.T) {
	s := memory(t)
	epoch, err := s.BeginControlEpoch()
	if err != nil {
		t.Fatal(err)
	}
	grant := func(id string) Grant {
		return Grant{ID: id, Scopes: []string{"read:history", "control:dial"}, Destinations: []string{"1002"},
			ExpiresUTC: nowUTC() + 600, Quota: 2, Epoch: epoch}
	}
	one, two := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)
	if err := s.InsertGrant(grant("g1"), one); err != nil {
		t.Fatal(err)
	}
	active, ok, err := s.ActiveGrant(one, epoch)
	if err != nil || !ok || len(active.Destinations) != 1 || active.Destinations[0] != "1002" || len(active.Scopes) != 2 || active.Incoming != nil {
		t.Fatalf("the grant as stored: %+v (found %v, %v)", active, ok, err)
	}
	if _, ok, _ := s.ActiveGrant(one, epoch+1); ok {
		t.Error("the grant is active in another epoch")
	}
	if _, ok, _ := s.ActiveGrant(two, epoch); ok {
		t.Error("another capability reaches the grant")
	}
	if listed, err := s.Grants(epoch); err != nil || len(listed) != 1 {
		t.Errorf("the grants listed: %v, %v", listed, err)
	}
	for i, want := range []bool{true, true, false} { // a quota of two
		if got, err := s.UseGrantCall("g1"); got != want || err != nil {
			t.Errorf("call %d of the quota: %v, %v", i+1, got, err)
		}
	}
	if err := s.RevokeGrant("g1"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.ActiveGrant(one, epoch); ok {
		t.Error("a revoked grant is active")
	}
	if listed, _ := s.Grants(epoch); len(listed) != 0 {
		t.Errorf("a revoked grant is listed: %v", listed)
	}
	var failure *Error
	if err := s.RevokeGrant("unknown"); !errors.As(err, &failure) || failure.Kind != NotFound {
		t.Errorf("revoking an unknown grant: %v", err)
	}
	// The validity is capped and the hash must be a SHA-256.
	long := grant("g2")
	long.ExpiresUTC = nowUTC() + MaxGrantSecs + 60
	if err := s.InsertGrant(long, bytes.Repeat([]byte{3}, 32)); err == nil {
		t.Error("a grant valid for more than an hour was taken")
	}
	if err := s.InsertGrant(grant("g3"), bytes.Repeat([]byte{4}, 8)); err == nil {
		t.Error("a hash of 8 bytes was taken")
	}
}
