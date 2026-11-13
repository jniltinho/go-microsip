package control

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

// Ported from the Rust candidate's crates/control/src/tests.rs.

func parse(t *testing.T, text string) (Request, error) {
	t.Helper()
	var r Request
	err := ReadMessage(bufio.NewReader(strings.NewReader(strings.ReplaceAll(text, "\n", " ")+"\n")), &r)
	return r, err
}

func text(s string) *string { return &s }

func TestRequestsRoundTripWithTheDocumentedShape(t *testing.T) {
	parsed, err := parse(t, `{"schema_version":1,"request_id":"r1","idempotency_key":"k-1",
		"operation":{"op":"call_dtmf","args":{"call_id":4,"expected_version":7,"digits":"12#"}}}`)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Operation{Op: CallDTMF, CallID: 4, ExpectedVersion: 7, Digits: "12#"}); !reflect.DeepEqual(parsed.Operation, want) {
		t.Errorf("the operation read: %+v", parsed.Operation)
	}
	if call, ok := parsed.Operation.Call(); !ok || call.CallID != 4 || call.ExpectedVersion != 7 {
		t.Errorf("the call it names: %+v, %v", call, ok)
	}
	if err := parsed.Validate(); err != nil {
		t.Error(err)
	}
	status, err := parse(t, `{"schema_version":1,"request_id":"r2","idempotency_key":null,"operation":{"op":"status_get"}}`)
	if err != nil || status.Operation.Op != StatusGet || status.Operation.Mutation() || status.Validate() != nil {
		t.Errorf("status_get: %+v, %v, %v", status, err, status.Validate())
	}
	if _, ok := status.Operation.Call(); ok {
		t.Error("status_get names a call")
	}
	// What is written is read back the same, and is the same bytes each time.
	wire, err := json.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	back, err := parse(t, string(wire))
	if err != nil || !reflect.DeepEqual(back, parsed) {
		t.Errorf("read back: %+v, %v", back, err)
	}
	if again, _ := json.Marshal(back); !bytes.Equal(again, wire) {
		t.Errorf("written twice: %s and %s", wire, again)
	}
	if !strings.Contains(string(wire), `"operation":{"op":"call_dtmf","args":{"call_id":4,"digits":"12#","expected_version":7}}`) {
		t.Errorf("the wire form: %s", wire)
	}
	// Optional arguments are written only when given.
	page, _ := json.Marshal(Operation{Op: HistoryList, Filter: text(FilterMissed)})
	if string(page) != `{"op":"history_list","args":{"filter":"missed"}}` {
		t.Errorf("history_list on the wire: %s", page)
	}
	if got, _ := json.Marshal(Operation{Op: StatusGet}); string(got) != `{"op":"status_get"}` {
		t.Errorf("status_get on the wire: %s", got)
	}
}

func TestUnknownFieldsAndOperationsAreRejected(t *testing.T) {
	for _, bad := range []string{
		`{"schema_version":1,"request_id":"r","idempotency_key":null,"operation":{"op":"status_get"},"extra":1}`,
		`{"schema_version":1,"request_id":"r","idempotency_key":"k","operation":{"op":"call_dial","args":{"target":"1002","confirm":true}}}`,
		`{"schema_version":1,"request_id":"r","idempotency_key":"k","operation":{"op":"call_hangup","args":{"call_id":1,"expected_version":1,"force":true}}}`,
		`{"schema_version":1,"request_id":"r","idempotency_key":null,"operation":{"op":"shell","args":{"cmd":"id"}}}`,
		`{"schema_version":1,"request_id":"r","idempotency_key":null,"operation":{"op":"approve","args":{}}}`,
		// Beyond the Rust cases: a missing, null or mistyped argument, arguments nobody asked for.
		`{"schema_version":1,"request_id":"r","idempotency_key":"k","operation":{"op":"call_dial","args":{}}}`,
		`{"schema_version":1,"request_id":"r","idempotency_key":"k","operation":{"op":"call_dial","args":{"target":null}}}`,
		`{"schema_version":1,"request_id":"r","idempotency_key":"k","operation":{"op":"call_dial"}}`,
		`{"schema_version":1,"request_id":"r","idempotency_key":"k","operation":{"op":"call_hangup","args":{"call_id":-1,"expected_version":1}}}`,
		`{"schema_version":1,"request_id":"r","idempotency_key":"k","operation":{"op":"call_hangup","args":{"call_id":"1","expected_version":1}}}`,
		`{"schema_version":1,"request_id":"r","idempotency_key":"k","operation":{"op":"call_dial","args":{"Target":"1002"}}}`,
		`{"schema_version":1,"request_id":"r","idempotency_key":null,"operation":{"op":"status_get","args":{"x":1}}}`,
		`{"schema_version":1,"request_id":"r","idempotency_key":null,"operation":{"op":"status_get","more":1}}`,
		`{"schema_version":1,"request_id":"r","idempotency_key":null,"operation":null}`,
		`{"schema_version":1,"request_id":"r","idempotency_key":null,"operation":{"op":"status_get"}} {}`,
		// Nothing may follow the request on its line, and its fields are spelt as the contract's.
		`{"schema_version":1,"request_id":"r","idempotency_key":null,"operation":{"op":"status_get"}}}`,
		`{"schema_version":1,"request_id":"r","idempotency_key":null,"operation":{"op":"status_get"}}]`,
		`{"schema_version":1,"request_id":"r","idempotency_key":null,"operation":{"op":"status_get"}} x`,
		`{"Schema_Version":1,"request_id":"r","idempotency_key":null,"operation":{"op":"status_get"}}`,
		`{"schema_version":1,"REQUEST_ID":"r","idempotency_key":null,"operation":{"op":"status_get"}}`,
		`{"schema_version":1,"request_id":"r","Capability":"abc","operation":{"op":"status_get"}}`,
	} {
		if r, err := parse(t, bad); err == nil {
			t.Errorf("taken: %s (as %+v)", bad, r)
		}
	}
}

func TestValidationBoundsInputsWithoutEchoingThem(t *testing.T) {
	make := func(key *string, o Operation) Request {
		return Request{SchemaVersion: 1, RequestID: "r1", IdempotencyKey: key, Operation: o}
	}
	dial := func(target string) Operation { return Operation{Op: CallDial, Target: target} }
	var failure *Error
	if err := make(nil, dial("1002")).Validate(); !errors.As(err, &failure) || failure.Code != Validation {
		t.Errorf("a mutation without a key: %v", err)
	}
	if err := make(text("k"), dial("not a number; rm -rf")).Validate(); err == nil || strings.Contains(err.Error(), "rm -rf") {
		t.Errorf("an invalid target: %v", err)
	}
	limit := func(n uint32) *uint32 { return &n }
	for _, c := range []struct {
		o  Operation
		ok bool
	}{
		{dial("1002"), true},
		{Operation{Op: CallDTMF, CallID: 1, ExpectedVersion: 1, Digits: strings.Repeat("1", 33)}, false},
		{Operation{Op: CallDTMF, CallID: 1, ExpectedVersion: 1, Digits: "12E"}, false},
		{Operation{Op: CallDTMF, CallID: 1, ExpectedVersion: 1, Digits: ""}, false},
		{Operation{Op: CallDTMF, CallID: 1, ExpectedVersion: 1, Digits: "0123456789*#ABCD"}, true},
		{Operation{Op: ContactsSearch, Query: strings.Repeat("a", 65)}, false},
		{Operation{Op: ContactsSearch, Query: "a\tb"}, false},
		{Operation{Op: ContactsSearch, Query: ""}, true},
		{Operation{Op: HistoryList, Limit: limit(101)}, false},
		{Operation{Op: HistoryList, Limit: limit(0)}, false},
		{Operation{Op: HistoryList, Filter: text(FilterMissed), Cursor: text("c:20"), Limit: limit(100)}, true},
		{Operation{Op: HistoryList, Filter: text("everything")}, false},
		{Operation{Op: OperationGet, OperationID: "../x"}, false},
		{Operation{Op: CallTransfer, CallID: 1, ExpectedVersion: 1, Target: "a b"}, false},
		{Operation{Op: "shell"}, false},
	} {
		if err := make(text("k"), c.o).Validate(); (err == nil) != c.ok {
			t.Errorf("%+v: %v, want valid %v", c.o, err, c.ok)
		}
	}
	wrongVersion := make(nil, Operation{Op: StatusGet})
	wrongVersion.SchemaVersion = 2
	longID := make(nil, Operation{Op: StatusGet})
	longID.RequestID = strings.Repeat("x", 65)
	badKey := make(text("a key"), Operation{Op: StatusGet})
	badCapability := make(nil, Operation{Op: StatusGet})
	badCapability.Capability = text("not/a/token")
	for name, r := range map[string]Request{"another version": wrongVersion, "a long id": longID, "a key with a space": badKey, "a capability with slashes": badCapability} {
		if r.Validate() == nil {
			t.Errorf("%s was taken", name)
		}
	}
	// A request never prints its capability.
	secret := make(nil, Operation{Op: StatusGet})
	secret.Capability = text("0123456789abcdef")
	if strings.Contains(secret.String(), "0123456789abcdef") {
		t.Errorf("the request printed its capability: %s", secret)
	}
}

func TestExitCodesFollowTheCLIContract(t *testing.T) {
	if got := OK("r", map[string]int{}).ExitCode(); got != 0 {
		t.Errorf("ok exits %d", got)
	}
	if got := Pending("r", StatusAccepted, "op-1").ExitCode(); got != 0 {
		t.Errorf("accepted exits %d", got)
	}
	if got := Pending("r", StatusApprovalRequired, "op-1").ExitCode(); got != 3 {
		t.Errorf("approval_required exits %d", got)
	}
	for code, exit := range map[Code]int{Validation: 2, Conflict: 2, NotFound: 2, InvalidState: 2, GrantRequired: 3, ApprovalDenied: 3,
		ApprovalExpired: 3, GrantExpired: 3, StaleEpoch: 3, EngineUnavailable: 4, EngineOwnedByService: 4, StorageUnavailable: 4,
		Busy: 4, RateLimited: 4, Unsupported: 4, ResultExpired: 4, Timeout: 5, Internal: 1} {
		response := Failure("r", fail(code, "x"))
		if response.ExitCode() != exit || response.Error.Retryable != code.Retryable() {
			t.Errorf("%s: exits %d (want %d), retryable %v", code, response.ExitCode(), exit, response.Error.Retryable)
		}
	}
	for _, code := range []Code{EngineUnavailable, Timeout, RateLimited, Busy} {
		if !code.Retryable() {
			t.Errorf("%s is not retryable", code)
		}
	}
	// An error of another kind is internal, and its text stays here.
	if r := Failure("r", errors.New("a path or a number")); r.Error.Code != Internal || strings.Contains(r.Error.Message, "path") {
		t.Errorf("a foreign error: %+v", r.Error)
	}
	// The response on the wire has every field, as the Rust contract's.
	wire, _ := json.Marshal(Pending("r", StatusAccepted, "op-1"))
	if string(wire) != `{"schema_version":1,"request_id":"r","status":"accepted","operation_id":"op-1","data":null,"error":null}` {
		t.Errorf("the response on the wire: %s", wire)
	}
}

func TestFramingIsBoundedAndLineBased(t *testing.T) {
	var buffer bytes.Buffer
	if err := WriteMessage(&buffer, CurrentHello()); err != nil {
		t.Fatal(err)
	}
	if err := WriteMessage(&buffer, OK("r", map[string]int{"a": 1})); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(&buffer)
	var hello Hello
	if err := ReadMessage(reader, &hello); err != nil || hello != CurrentHello() {
		t.Errorf("the hello: %+v, %v", hello, err)
	}
	var response Response
	if err := ReadMessage(reader, &response); err != nil || response.RequestID != "r" || string(response.Data) != `{"a":1}` {
		t.Errorf("the response: %+v, %v", response, err)
	}
	if err := ReadMessage(reader, &hello); !errors.Is(err, io.EOF) {
		t.Errorf("at the end of the stream: %v", err)
	}
	if err := ReadMessage(bufio.NewReader(strings.NewReader(`{"protocol":"x"`)), &hello); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("a line cut short: %v", err)
	}
	huge := append(bytes.Repeat([]byte{'x'}, MaxMessage+10), '\n')
	if err := ReadMessage(bufio.NewReader(bytes.NewReader(huge)), &hello); !errors.Is(err, ErrInvalidMessage) {
		t.Errorf("a line over the limit: %v", err)
	}
	if err := ReadMessage(bufio.NewReader(strings.NewReader("{\"protocol\":1}\n")), &hello); !errors.Is(err, ErrInvalidMessage) {
		t.Errorf("a hello of the wrong type: %v", err)
	}
	if err := WriteMessage(io.Discard, strings.Repeat("x", MaxMessage)); !errors.Is(err, ErrInvalidMessage) {
		t.Errorf("writing a message over the limit: %v", err)
	}
	// The largest line that fits is read.
	fits := []byte(`{"protocol":"` + strings.Repeat("p", MaxMessage-28) + `","version":1}` + "\n")
	if len(fits) != MaxMessage {
		t.Fatalf("the test's line is %d bytes", len(fits))
	}
	if err := ReadMessage(bufio.NewReader(bytes.NewReader(fits)), &hello); err != nil {
		t.Errorf("a line of exactly the limit: %v", err)
	}
}
