package app

import "github.com/jniltinho/go-microsip/internal/core/store"

// The records of agent control, under the names of this package: front ends do not import the
// store.
type (
	OperationState  = store.OperationState
	OperationRecord = store.OperationRecord
	AuditRow        = store.AuditRow
	Grant           = store.Grant
)

// The states of a recorded operation.
const (
	OpAwaitingApproval = store.OpAwaitingApproval
	OpExecuting        = store.OpExecuting
	OpSucceeded        = store.OpSucceeded
	OpFailed           = store.OpFailed
	OpDenied           = store.OpDenied
	OpExpired          = store.OpExpired
	OpOutcomeUnknown   = store.OpOutcomeUnknown
)

// Ledger is the durable record of agent control in the phone's database: the operations asked
// for, the grants and the audit trail. Everything is written before it has any effect.
type Ledger interface {
	BeginControlEpoch() (int64, error)
	FindOperationKey(key string) (OperationRecord, bool, error)
	OperationKeyUsed(epoch int64, key string) (bool, error)
	Operation(id string) (OperationRecord, bool, error)
	InsertOperation(OperationRecord) error
	UpdateOperation(id string, state OperationState, outcome string) error
	Audit(operation, decision, detail string) error
	AuditRows(limit int) ([]AuditRow, error)
	ClearAudit() (int, error)
	InsertGrant(g Grant, capabilityHash []byte) error
	ActiveGrant(capabilityHash []byte, epoch int64) (Grant, bool, error)
	Grants(epoch int64) ([]Grant, error)
	UseGrantCall(id string) (bool, error)
	RevokeGrant(id string) error
}

// Ledger returns the control ledger; ErrNoStorage when the phone has no database, and agent
// control then changes nothing.
func (p *Phone) Ledger() (Ledger, error) {
	s, err := p.storage()
	if err != nil {
		return nil, err
	}
	return s, nil
}
