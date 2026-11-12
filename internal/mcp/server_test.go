package mcp

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jniltinho/go-microsip/internal/control"
)

// owner stands in for the running softphone: it writes down the requests and answers as told.
type owner struct {
	mu       sync.Mutex
	requests []control.Request
	answer   func(control.Request) control.Response
}

func (o *owner) reach(r control.Request) control.Response {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.requests = append(o.requests, r)
	if o.answer != nil {
		return o.answer(r)
	}
	return control.OK(r.RequestID, map[string]string{"registration": "registered"})
}

func (o *owner) last(t *testing.T) control.Request {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.requests) == 0 {
		t.Fatal("nothing reached the owner")
	}
	return o.requests[len(o.requests)-1]
}

func (o *owner) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.requests)
}

// unlimited lifts the rate limit for a test that makes many calls.
func unlimited(t *testing.T) {
	ratePerSecond = 1000
	t.Cleanup(func() { ratePerSecond = RatePerSecond })
}

// connect is a client of a server in this process.
func connect(t *testing.T, o *owner, capability *string) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := newServer(o.reach, capability).Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// call runs a tool and returns the owner's answer as the client reads it.
func call(t *testing.T, cs *mcp.ClientSession, name string, arguments any) (control.Response, bool) {
	t.Helper()
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("%s: the result is not text: %+v", name, result.Content)
	}
	var response control.Response
	if err := json.Unmarshal([]byte(text.Text), &response); err != nil {
		t.Fatalf("%s: the result is not the owner's answer: %s", name, text.Text)
	}
	structured, _ := json.Marshal(result.StructuredContent)
	var same control.Response
	if json.Unmarshal(structured, &same) != nil || same.Status != response.Status {
		t.Errorf("%s: the structured result differs from the text: %s", name, structured)
	}
	return response, result.IsError
}

type args = map[string]any

func TestTheFifteenToolsAreAdvertisedWithStrictSchemas(t *testing.T) {
	cs := connect(t, &owner{}, nil)
	listed, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
		wire, _ := json.Marshal(tool.InputSchema)
		if !strings.Contains(string(wire), `"additionalProperties":false`) || !strings.Contains(string(wire), `"type":"object"`) {
			t.Errorf("%s: its schema lets unknown arguments in: %s", tool.Name, wire)
		}
		read := slices.Contains([]string{"status_get", "operation_get", "devices_list", "contacts_search", "history_list", "config_show"}, tool.Name)
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint != read {
			t.Errorf("%s: read-only hint %+v, want %v", tool.Name, tool.Annotations, read)
		}
		if !read && !strings.Contains(string(wire), `"idempotency_key"`) {
			t.Errorf("%s changes something and takes no idempotency key: %s", tool.Name, wire)
		}
		if strings.Contains(strings.ToLower(tool.Name+tool.Description), "approve") && !strings.Contains(tool.Description, "Waits for a person to approve") {
			t.Errorf("%s speaks of approving: %s", tool.Name, tool.Description)
		}
	}
	want := []string{"status_get", "operation_get", "devices_list", "contacts_search", "history_list", "config_show", "call_dial", "call_answer",
		"call_reject", "call_hangup", "call_dtmf", "call_mute", "call_hold", "call_transfer", "dnd_set"}
	slices.Sort(names)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Errorf("the tools: %v", names)
	}
}

func TestEachToolIsOneControlOperation(t *testing.T) {
	unlimited(t)
	o := &owner{}
	cs := connect(t, o, nil)
	for _, c := range []struct {
		tool      string
		arguments args
		want      string // the operation on the wire
		key       string
	}{
		{"status_get", nil, `{"op":"status_get"}`, ""},
		{"operation_get", args{"operation_id": "op-1-abc"}, `{"op":"operation_get","args":{"operation_id":"op-1-abc"}}`, ""},
		{"devices_list", args{}, `{"op":"devices_list"}`, ""},
		{"config_show", nil, `{"op":"config_show"}`, ""},
		{"contacts_search", args{"query": "ana", "limit": 5}, `{"op":"contacts_search","args":{"limit":5,"query":"ana"}}`, ""},
		{"history_list", nil, `{"op":"history_list","args":{}}`, ""},
		{"history_list", args{"filter": "missed", "cursor": "o:20"}, `{"op":"history_list","args":{"cursor":"o:20","filter":"missed"}}`, ""},
		{"call_dial", args{"target": "1002", "idempotency_key": "k-1"}, `{"op":"call_dial","args":{"target":"1002"}}`, "k-1"},
		{"call_answer", args{"call_id": 3, "expected_version": 2, "idempotency_key": "k-2"}, `{"op":"call_answer","args":{"call_id":3,"expected_version":2}}`, "k-2"},
		{"call_reject", args{"call_id": 3, "expected_version": 2, "idempotency_key": "k-3"}, `{"op":"call_reject","args":{"call_id":3,"expected_version":2}}`, "k-3"},
		{"call_hangup", args{"call_id": 3, "expected_version": 2, "idempotency_key": "k-4"}, `{"op":"call_hangup","args":{"call_id":3,"expected_version":2}}`, "k-4"},
		{"call_dtmf", args{"call_id": 3, "expected_version": 2, "digits": "12#", "idempotency_key": "k-5"}, `{"op":"call_dtmf","args":{"call_id":3,"digits":"12#","expected_version":2}}`, "k-5"},
		{"call_mute", args{"call_id": 3, "expected_version": 2, "muted": true, "idempotency_key": "k-6"}, `{"op":"call_mute","args":{"call_id":3,"expected_version":2,"muted":true}}`, "k-6"},
		{"call_hold", args{"call_id": 3, "expected_version": 2, "held": false, "idempotency_key": "k-7"}, `{"op":"call_hold","args":{"call_id":3,"expected_version":2,"held":false}}`, "k-7"},
		{"call_transfer", args{"call_id": 3, "expected_version": 2, "target": "1003", "idempotency_key": "k-8"}, `{"op":"call_transfer","args":{"call_id":3,"expected_version":2,"target":"1003"}}`, "k-8"},
		{"dnd_set", args{"enabled": true, "idempotency_key": "k-9"}, `{"op":"dnd_set","args":{"enabled":true}}`, "k-9"},
	} {
		before := o.count()
		response, failed := call(t, cs, c.tool, c.arguments)
		if failed || response.Status != control.StatusOK || o.count() != before+1 {
			t.Errorf("%s %v: %+v (%+v)", c.tool, c.arguments, response, response.Error)
			continue
		}
		request := o.last(t)
		wire, _ := json.Marshal(request.Operation)
		key := ""
		if request.IdempotencyKey != nil {
			key = *request.IdempotencyKey
		}
		if string(wire) != c.want || key != c.key || request.SchemaVersion != control.SchemaVersion || request.Capability != nil ||
			!strings.HasPrefix(request.RequestID, "mcp-") || response.RequestID != request.RequestID {
			t.Errorf("%s %v reached the owner as %s, key %q, request %q", c.tool, c.arguments, wire, key, request.RequestID)
		}
	}
}

func TestWhatDoesNotMatchAToolNeverReachesTheOwner(t *testing.T) {
	unlimited(t)
	o := &owner{}
	cs := connect(t, o, nil)
	for name, c := range map[string]struct {
		tool      string
		arguments args
	}{
		"an argument the tool does not have": {"call_dial", args{"target": "1002", "idempotency_key": "k", "confirm": true}},
		"a tool that approves":               {"call_dial", args{"target": "1002", "idempotency_key": "k", "approve": true}},
		"a change without a key":             {"call_dial", args{"target": "1002"}},
		"a key that is not text":             {"dnd_set", args{"enabled": true, "idempotency_key": 7}},
		"a target that is no number":         {"call_dial", args{"target": "not a number; rm -rf", "idempotency_key": "k"}},
		"digits that are none":               {"call_dtmf", args{"call_id": 1, "expected_version": 1, "digits": "12E", "idempotency_key": "k"}},
		"a call id that is text":             {"call_hangup", args{"call_id": "1", "expected_version": 1, "idempotency_key": "k"}},
		"a missing argument":                 {"call_mute", args{"call_id": 1, "expected_version": 1, "idempotency_key": "k"}},
		"a page over the limit":              {"history_list", args{"limit": 101}},
		"an argument for a tool without any": {"status_get", args{"verbose": true}},
		"a contacts search without a query":  {"contacts_search", args{}},
	} {
		response, failed := call(t, cs, c.tool, c.arguments)
		if !failed || response.Error == nil || response.Error.Code != control.Validation {
			t.Errorf("%s: %+v (%+v)", name, response, response.Error)
		}
		if response.Error != nil && (strings.Contains(response.Error.Message, "rm -rf") || strings.Contains(response.Error.Message, "12E")) {
			t.Errorf("%s: the error repeats what was sent: %s", name, response.Error.Message)
		}
	}
	if o.count() != 0 {
		t.Errorf("%d invalid calls reached the owner", o.count())
	}
	// A tool that does not exist is the protocol's own error: no shell, no file, no approval.
	for _, tool := range []string{"shell", "approve", "read_file", "grant_issue"} {
		if result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args{}}); err == nil && !result.IsError {
			t.Errorf("the tool %q exists: %+v", tool, result)
		}
	}
	if o.count() != 0 {
		t.Error("an unknown tool reached the owner")
	}
}

func TestTheOwnersAnswerIsTheToolsResult(t *testing.T) {
	o := &owner{}
	capability := strings.Repeat("a", 64)
	cs := connect(t, o, &capability)
	// Waiting for a person is not an error; a refusal is.
	o.answer = func(r control.Request) control.Response {
		return control.Pending(r.RequestID, control.StatusApprovalRequired, "op-1-abc")
	}
	response, failed := call(t, cs, "call_dial", args{"target": "1002", "idempotency_key": "k-1"})
	if failed || response.Status != control.StatusApprovalRequired || response.OperationID == nil || *response.OperationID != "op-1-abc" {
		t.Errorf("a call that waits for approval: %+v, error flag %v", response, failed)
	}
	// The grant of --grant-file goes with every request, and is never in an answer.
	if got := o.last(t).Capability; got == nil || *got != capability {
		t.Error("the capability did not go with the request")
	}
	o.answer = func(r control.Request) control.Response {
		return control.Failure(r.RequestID, &control.Error{Code: control.GrantRequired, Message: "this read needs a read grant issued in the window"})
	}
	response, failed = call(t, cs, "history_list", nil)
	if !failed || response.Error == nil || response.Error.Code != control.GrantRequired {
		t.Errorf("a read without a grant: %+v, error flag %v", response, failed)
	}
	wire, _ := json.Marshal(response)
	if strings.Contains(string(wire), capability) {
		t.Errorf("the answer holds the capability: %s", wire)
	}
	// No owner: the answer says so, and nothing was started.
	o.answer = func(r control.Request) control.Response {
		return control.Failure(r.RequestID, &control.Error{Code: control.EngineUnavailable, Message: "go-microsip is not running; nothing was started"})
	}
	if response, failed := call(t, cs, "status_get", nil); !failed || response.Error.Code != control.EngineUnavailable || !response.Error.Retryable {
		t.Errorf("status without an owner: %+v", response)
	}
}

func TestToolCallsAreLimitedToTenASecond(t *testing.T) {
	o := &owner{}
	cs := connect(t, o, nil)
	limited := 0
	for i := range 25 {
		response, failed := call(t, cs, "status_get", nil)
		if failed && response.Error != nil && response.Error.Code == control.RateLimited {
			limited++
		} else if failed {
			t.Fatalf("call %d: %+v", i, response.Error)
		}
	}
	// 25 calls take far less than two seconds: at most two windows of ten were let through.
	if limited < 5 || o.count() > 2*RatePerSecond || o.count()+limited != 25 {
		t.Errorf("%d of 25 calls limited, %d reached the owner", limited, o.count())
	}
}
