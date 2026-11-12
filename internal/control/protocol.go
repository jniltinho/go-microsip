// Package control is the local control of a running go-microsip by the command line and the MCP
// server (design D10 of migrate-to-go-wails; the Rust candidate's add-agent-cli-mcp).
//
// The window that owns the profile serves this protocol on a private Unix socket of its own
// name; the command line and the MCP server are clients. One JSON object per line, at most
// MaxMessage bytes. A connection starts with a Hello answered by a Welcome, then carries
// Request and Response pairs in order. Unknown fields are refused everywhere.
//
// The contract is the Rust candidate's, version 1, under go-microsip's own protocol name and
// socket: the two applications never answer each other.
package control

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jniltinho/go-microsip/internal/core/domain"
)

const (
	// Protocol is the name sent in Hello and Welcome.
	Protocol = "go-microsip-control"
	// SchemaVersion is the version of this contract.
	SchemaVersion = 1
	// MaxMessage is the largest message, its line break included.
	MaxMessage = 1 << 20
	// MaxPage is the largest page of contacts or history.
	MaxPage = 100
	// SocketFile is the socket's name in the private runtime directory.
	SocketFile = "control.sock"
)

// Hello is a client's first message.
type Hello struct {
	Protocol string `json:"protocol"`
	Version  uint32 `json:"version"`
}

// CurrentHello is the hello of this version.
func CurrentHello() Hello { return Hello{Protocol: Protocol, Version: SchemaVersion} }

// Mode says which kind of process owns the profile.
type Mode string

// The owners there are.
const (
	// ModeWindow is the desktop window: a mutation no grant covers waits for the person at it.
	ModeWindow Mode = "gui"
	// ModeService is the headless service: it runs only what its bootstrap grant allows.
	ModeService Mode = "service"
)

// Welcome is the owner's answer to Hello.
type Welcome struct {
	Protocol string `json:"protocol"`
	Version  uint32 `json:"version"`
	Mode     Mode   `json:"mode"`
	// Epoch is the owner's session; it changes at every start.
	Epoch uint64 `json:"epoch"`
}

// The operations. Reads have no effect; the others are mutations and need an idempotency key.
// The names are also the MCP tools' names.
const (
	StatusGet      = "status_get"      // registration, call and do-not-disturb, without names or numbers
	DevicesList    = "devices_list"    // audio devices
	ContactsSearch = "contacts_search" // contacts matching a text
	HistoryList    = "history_list"    // call history
	ConfigShow     = "config_show"     // the effective configuration, never secrets
	OperationGet   = "operation_get"   // the outcome of an earlier mutation
	CallDial       = "call_dial"       // place a call
	CallAnswer     = "call_answer"     // answer a ringing incoming call
	CallReject     = "call_reject"     // reject a ringing incoming call
	CallHangup     = "call_hangup"     // end a call
	CallDTMF       = "call_dtmf"       // send DTMF digits
	CallMute       = "call_mute"       // mute or unmute the microphone
	CallHold       = "call_hold"       // hold or resume
	CallTransfer   = "call_transfer"   // blind transfer
	DNDSet         = "dnd_set"         // do-not-disturb
)

// The history filters.
const (
	FilterAll      = "all"
	FilterIncoming = "incoming"
	FilterOutgoing = "outgoing"
	FilterMissed   = "missed"
)

// Operation is one operation and its arguments. Only the fields its Op names are used; on the
// wire it is {"op": name, "args": {...}}, with no "args" for an operation that takes none.
//
// A call operation names the call and the snapshot version the client saw, so that it never
// lands on a call that replaced it.
type Operation struct {
	Op string `json:"-"`

	Query           string  `json:"query"`            // contacts_search: literal text, may be empty
	Cursor          *string `json:"cursor"`           // contacts_search, history_list: from a page before
	Limit           *uint32 `json:"limit"`            // contacts_search, history_list: at most MaxPage
	Filter          *string `json:"filter"`           // history_list
	OperationID     string  `json:"operation_id"`     // operation_get
	Target          string  `json:"target"`           // call_dial, call_transfer: a number or SIP user
	CallID          uint64  `json:"call_id"`          // the call operations: from status_get
	ExpectedVersion uint64  `json:"expected_version"` // the call operations: from status_get
	Digits          string  `json:"digits"`           // call_dtmf: 0-9 * # A-D, at most 32
	Muted           bool    `json:"muted"`            // call_mute: the state wanted
	Held            bool    `json:"held"`             // call_hold: the state wanted
	Enabled         bool    `json:"enabled"`          // dnd_set: the state wanted
}

// shape is the arguments an operation takes.
type shape struct{ required, optional []string }

var (
	call   = []string{"call_id", "expected_version"}
	shapes = map[string]shape{
		StatusGet:      {},
		DevicesList:    {},
		ConfigShow:     {},
		ContactsSearch: {required: []string{"query"}, optional: []string{"cursor", "limit"}},
		HistoryList:    {optional: []string{"filter", "cursor", "limit"}},
		OperationGet:   {required: []string{"operation_id"}},
		CallDial:       {required: []string{"target"}},
		CallAnswer:     {required: call},
		CallReject:     {required: call},
		CallHangup:     {required: call},
		CallDTMF:       {required: append(slices.Clone(call), "digits")},
		CallMute:       {required: append(slices.Clone(call), "muted")},
		CallHold:       {required: append(slices.Clone(call), "held")},
		CallTransfer:   {required: append(slices.Clone(call), "target")},
		DNDSet:         {required: []string{"enabled"}},
	}
	reads = []string{StatusGet, DevicesList, ContactsSearch, HistoryList, ConfigShow, OperationGet}
)

// errShape is every way an operation's JSON can be wrong; the text never repeats the input.
var errShape = errors.New("the operation is not one of this contract, or its arguments are not")

// UnmarshalJSON reads {"op": ..., "args": {...}} and refuses an unknown operation, an unknown or
// missing argument, and an argument of the wrong type.
func (o *Operation) UnmarshalJSON(data []byte) error {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(data, &envelope) != nil {
		return errShape
	}
	var name string
	if json.Unmarshal(envelope["op"], &name) != nil {
		return errShape
	}
	want, known := shapes[name]
	args, hasArgs := envelope["args"]
	if !known || len(envelope) > 2 || (len(envelope) == 2 && !hasArgs) {
		return errShape
	}
	*o = Operation{Op: name}
	if len(want.required)+len(want.optional) == 0 {
		if hasArgs && string(bytes.TrimSpace(args)) != "null" {
			return errShape
		}
		return nil
	}
	var given map[string]json.RawMessage
	if !hasArgs || json.Unmarshal(args, &given) != nil || given == nil {
		return errShape
	}
	for key, value := range given {
		required := slices.Contains(want.required, key)
		if !required && !slices.Contains(want.optional, key) {
			return errShape
		}
		if required && string(bytes.TrimSpace(value)) == "null" {
			return errShape
		}
	}
	for _, key := range want.required {
		if _, ok := given[key]; !ok {
			return errShape
		}
	}
	if json.Unmarshal(args, (*operationArgs)(o)) != nil { // Op is not an argument: it stays
		return errShape
	}
	return nil
}

// operationArgs is Operation without its methods, so that reading the arguments does not come
// back to UnmarshalJSON.
type operationArgs Operation

// MarshalJSON writes the wire form, the same bytes for the same operation: the owner's MAC of a
// request is computed over it.
func (o Operation) MarshalJSON() ([]byte, error) {
	want, known := shapes[o.Op]
	if !known {
		return nil, errShape
	}
	if len(want.required)+len(want.optional) == 0 {
		return json.Marshal(map[string]string{"op": o.Op})
	}
	whole, err := json.Marshal(operationArgs(o))
	if err != nil {
		return nil, err
	}
	var all, args map[string]json.RawMessage
	if err := json.Unmarshal(whole, &all); err != nil {
		return nil, err
	}
	args = map[string]json.RawMessage{}
	for _, key := range want.required {
		args[key] = all[key]
	}
	for _, key := range want.optional {
		if string(all[key]) != "null" {
			args[key] = all[key]
		}
	}
	return json.Marshal(struct {
		Op   string                     `json:"op"`
		Args map[string]json.RawMessage `json:"args"`
	}{o.Op, args})
}

// Mutation reports whether the operation has effects outside the owner.
func (o Operation) Mutation() bool { return !slices.Contains(reads, o.Op) }

// CallRef is the call an operation names and the snapshot version the client saw.
type CallRef struct{ CallID, ExpectedVersion uint64 }

// Call returns the call a call operation targets.
func (o Operation) Call() (CallRef, bool) {
	if slices.Contains(shapes[o.Op].required, "call_id") {
		return CallRef{o.CallID, o.ExpectedVersion}, true
	}
	return CallRef{}, false
}

// Validate checks bounds and formats. The error never repeats the input.
func (o Operation) Validate() error {
	page := func() error {
		if o.Limit != nil && (*o.Limit == 0 || *o.Limit > MaxPage) {
			return invalid("limit must be between 1 and 100")
		}
		if o.Cursor != nil && !Token(*o.Cursor, 64) {
			return invalid("cursor is not valid")
		}
		return nil
	}
	target := func() error {
		if _, err := domain.ParseDialTarget(o.Target); err != nil {
			return invalid("target is not a valid number or SIP user")
		}
		return nil
	}
	switch o.Op {
	case ContactsSearch:
		if utf8.RuneCountInString(o.Query) > 64 || strings.IndexFunc(o.Query, unicode.IsControl) >= 0 {
			return invalid("query must be at most 64 printable characters")
		}
		return page()
	case HistoryList:
		if o.Filter != nil && !slices.Contains([]string{FilterAll, FilterIncoming, FilterOutgoing, FilterMissed}, *o.Filter) {
			return invalid("filter must be all, incoming, outgoing or missed")
		}
		return page()
	case OperationGet:
		if !Token(o.OperationID, 64) {
			return invalid("operation_id is not valid")
		}
	case CallDial, CallTransfer:
		return target()
	case CallDTMF:
		if o.Digits == "" || len(o.Digits) > 32 || strings.Trim(o.Digits, "0123456789*#ABCD") != "" {
			return invalid("digits must be 1 to 32 of 0-9 * # A-D")
		}
	default:
		if _, known := shapes[o.Op]; !known {
			return invalid("unknown operation")
		}
	}
	return nil
}

// Token reports whether value is [A-Za-z0-9._:-]{1,max}: identifiers, keys and cursors.
func Token(value string, max int) bool {
	if value == "" || len(value) > max {
		return false
	}
	for _, b := range []byte(value) {
		if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.IndexByte("._:-", b) >= 0) {
			return false
		}
	}
	return true
}

// Request is what a client asks.
type Request struct {
	SchemaVersion uint32 `json:"schema_version"`
	// RequestID is the client's own, echoed in the response.
	RequestID string `json:"request_id"`
	// IdempotencyKey is required for mutations: the same key again never repeats the effect, and
	// the same key for another request is a conflict.
	IdempotencyKey *string   `json:"idempotency_key"`
	Operation      Operation `json:"operation"`
	// Capability is the secret of a grant issued in the window, which the client reads from a
	// private file and never takes as an argument. The owner keeps only its hash.
	Capability *string `json:"capability,omitempty"`
}

// String describes the request without its capability.
func (r Request) String() string {
	capability := "none"
	if r.Capability != nil {
		capability = "[REDACTED]"
	}
	return fmt.Sprintf("request %s: %s, capability %s", r.RequestID, r.Operation.Op, capability)
}

// Validate checks the envelope and the operation.
func (r Request) Validate() error {
	switch {
	case r.SchemaVersion != SchemaVersion:
		return invalid("unsupported schema_version")
	case !Token(r.RequestID, 64):
		return invalid("request_id must be 1 to 64 of A-Z a-z 0-9 . _ : -")
	case r.Capability != nil && !Token(*r.Capability, 128):
		return invalid("capability is not valid")
	case r.IdempotencyKey == nil && r.Operation.Mutation():
		return invalid("idempotency_key is required for this operation")
	case r.IdempotencyKey != nil && !Token(*r.IdempotencyKey, 128):
		return invalid("idempotency_key must be 1 to 128 of A-Z a-z 0-9 . _ : -")
	}
	return r.Operation.Validate()
}

// Status is the class of a response.
type Status string

// The classes of a response.
const (
	// StatusOK: a read succeeded, or a mutation's final outcome is known.
	StatusOK Status = "ok"
	// StatusAccepted: a mutation was taken; ask operation_get for its outcome.
	StatusAccepted Status = "accepted"
	// StatusApprovalRequired: a person must approve the mutation in the window; ask
	// operation_get with the operation id that came back. Not an error.
	StatusApprovalRequired Status = "approval_required"
	// StatusError: see ErrorBody.
	StatusError Status = "error"
)

// Code is a stable error code.
type Code string

// The error codes.
const (
	Validation           Code = "validation"              // malformed or out-of-bounds input
	GrantRequired        Code = "grant_required"          // no grant covers this operation
	ApprovalDenied       Code = "approval_denied"         // a person denied the operation
	ApprovalExpired      Code = "approval_expired"        // nobody approved it in time
	GrantExpired         Code = "grant_expired"           // the grant expired
	EngineUnavailable    Code = "engine_unavailable"      // no owner is running; none was started
	EngineOwnedByService Code = "engine_owned_by_service" // the headless service owns the profile
	Timeout              Code = "timeout"                 // the owner did not answer in time; the outcome may still change
	Conflict             Code = "conflict"                // the idempotency key was used for another request
	StaleEpoch           Code = "stale_epoch"             // the operation belongs to an earlier owner session
	ResultExpired        Code = "result_expired"          // the outcome is older than the retention window
	NotFound             Code = "not_found"               // unknown call, operation or version
	RateLimited          Code = "rate_limited"            // too many requests
	Busy                 Code = "busy"                    // the owner is busy; try again
	StorageUnavailable   Code = "storage_unavailable"     // the database or the ledger is unavailable; nothing was done
	InvalidState         Code = "invalid_state"           // not possible in the current state
	Unsupported          Code = "unsupported"             // not offered in this mode
	Internal             Code = "internal"                // unexpected failure
)

// ExitCode is the command line's exit status for the error.
func (c Code) ExitCode() int {
	switch c {
	case Validation, Conflict, NotFound, InvalidState:
		return 2
	case GrantRequired, ApprovalDenied, ApprovalExpired, GrantExpired, StaleEpoch:
		return 3
	case EngineUnavailable, EngineOwnedByService, StorageUnavailable, Busy, RateLimited, Unsupported, ResultExpired:
		return 4
	case Timeout:
		return 5
	}
	return 1
}

// Retryable reports whether trying again later, with the same idempotency key, may succeed.
func (c Code) Retryable() bool {
	return c == EngineUnavailable || c == Timeout || c == RateLimited || c == Busy
}

// Error is a failure before or during an operation: a stable code and a message free of
// personal data.
type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

func fail(code Code, message string) *Error { return &Error{Code: code, Message: message} }
func invalid(message string) error          { return fail(Validation, message) }

// ErrorBody is the error of a response. Its message never holds numbers, names, digits or
// secrets.
type ErrorBody struct {
	Code      Code   `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// Response is the owner's reply.
type Response struct {
	SchemaVersion uint32 `json:"schema_version"`
	// RequestID echoes the request's; empty when the request could not be read.
	RequestID string `json:"request_id"`
	Status    Status `json:"status"`
	// OperationID is set for a mutation that was taken or waits.
	OperationID *string `json:"operation_id"`
	// Data is the result of a read or of a known outcome.
	Data json.RawMessage `json:"data"`
	// Error is set when Status is StatusError.
	Error *ErrorBody `json:"error"`
}

// OK is a successful read or a final outcome.
func OK(requestID string, data any) Response {
	raw, err := json.Marshal(data)
	if err != nil {
		return Failure(requestID, fail(Internal, "the result could not be written"))
	}
	return Response{SchemaVersion: SchemaVersion, RequestID: requestID, Status: StatusOK, Data: raw}
}

// Pending is a mutation that was taken, or waits for a person.
func Pending(requestID string, status Status, operationID string) Response {
	return Response{SchemaVersion: SchemaVersion, RequestID: requestID, Status: status, OperationID: &operationID}
}

// Failure is an error response. An error that is not an *Error is reported as internal, without
// its text.
func Failure(requestID string, err error) Response {
	var e *Error
	if !errors.As(err, &e) {
		e = fail(Internal, "unexpected failure")
	}
	return Response{SchemaVersion: SchemaVersion, RequestID: requestID, Status: StatusError,
		Error: &ErrorBody{Code: e.Code, Message: e.Message, Retryable: e.Code.Retryable()}}
}

// ExitCode is the command line's exit status: 0 done or taken, 3 waiting for approval, else the
// error's.
func (r Response) ExitCode() int {
	switch {
	case r.Status == StatusOK || r.Status == StatusAccepted:
		return 0
	case r.Status == StatusApprovalRequired:
		return 3
	case r.Error != nil:
		return r.Error.Code.ExitCode()
	}
	return 1
}

// ErrInvalidMessage is a line that is too long or is not the message expected.
var ErrInvalidMessage = errors.New("invalid message")

// errTooLong is the ErrInvalidMessage after which the stream is not at a line's start.
var errTooLong = fmt.Errorf("%w: over the size limit", ErrInvalidMessage)

// WriteMessage writes one message as a JSON line.
func WriteMessage(w io.Writer, message any) error {
	line, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if len(line)+1 > MaxMessage {
		return ErrInvalidMessage
	}
	_, err = w.Write(append(line, '\n'))
	return err
}

// ReadMessage reads one JSON line of at most MaxMessage bytes into message, refusing unknown
// fields. It returns io.EOF at the end of the stream, io.ErrUnexpectedEOF for a line cut short,
// and ErrInvalidMessage for a line that is too long or is not the message expected; after a line
// that is too long the stream is not at a line's start any more.
func ReadMessage(r *bufio.Reader, message any) error {
	var line []byte
	for {
		part, err := r.ReadSlice('\n')
		line = append(line, part...)
		if len(line) > MaxMessage {
			return errTooLong
		}
		switch {
		case err == nil:
			decoder := json.NewDecoder(bytes.NewReader(line))
			decoder.DisallowUnknownFields()
			if decoder.Decode(message) != nil || decoder.More() {
				return ErrInvalidMessage
			}
			return nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(line) > 0:
			return io.ErrUnexpectedEOF
		default:
			return err
		}
	}
}
