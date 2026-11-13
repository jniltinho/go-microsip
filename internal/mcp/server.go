// Package mcp is the stdio Model Context Protocol server. It opens no network port and never
// imports the desktop toolkit, so it runs without a display.
//
// Every tool is exactly one control operation, run by the softphone that is running, through
// the same socket as the command line: the owner's rules apply unchanged. A mutation waits for
// a person at the window, personal reads need a grant, and nothing here can approve anything.
// The server never starts a softphone, never opens the database, and offers no shell, file or
// network access. The standard output carries MCP messages and nothing else.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jniltinho/go-microsip/internal/buildinfo"
	"github.com/jniltinho/go-microsip/internal/control"
)

const (
	// MaxLine is the longest input line taken: one JSON-RPC message.
	MaxLine = 1 << 20
	// RatePerSecond is how many tool calls a second are taken; more get rate_limited.
	RatePerSecond = 10

	keyName = "idempotency_key"
)

// ratePerSecond is RatePerSecond; tests that make many calls raise it.
var ratePerSecond = RatePerSecond

// Reach sends one request to the owner of the session and returns its answer. It never starts
// an owner: without one the answer is engine_unavailable.
type Reach func(control.Request) control.Response

// Serve runs the server on standard input and output until the client disconnects. capability
// is the grant of --grant-file, presented with every request; nil without one.
func Serve(ctx context.Context, reach Reach, capability *string) error {
	return newServer(reach, capability).Run(ctx, &mcp.StdioTransport{MaxLineLength: MaxLine})
}

type object = map[string]any

func schema(properties object, required ...string) object {
	if required == nil {
		required = []string{}
	}
	return object{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

var (
	keyProperty = object{"type": "string", "pattern": "^[A-Za-z0-9._:-]{1,128}$",
		"description": "Reuse the same key to retry safely; a new key is a new request."}
	targetProperty = object{"type": "string", "minLength": 1, "maxLength": 64, "description": "Number or SIP user."}
)

// with is the properties of a call operation and what the tool adds to them.
func with(extra object) object {
	properties := object{
		"call_id":          object{"type": "integer", "minimum": 0, "description": "From status_get."},
		"expected_version": object{"type": "integer", "minimum": 0, "description": "From status_get."},
		keyName:            keyProperty,
	}
	for name, property := range extra {
		properties[name] = property
	}
	return properties
}

func annotations(readOnly, destructive, openWorld bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: readOnly, IdempotentHint: readOnly, DestructiveHint: &destructive, OpenWorldHint: &openWorld}
}

// Tools are the tools advertised, one for each control operation, with strict schemas.
func Tools() []*mcp.Tool {
	const pending = " Waits for a person to approve it in the go-microsip window; query operation_get with the operation_id that comes back."
	page := func(extra object) object {
		properties := object{
			"cursor": object{"type": "string", "pattern": "^[A-Za-z0-9._:-]{1,64}$"},
			"limit":  object{"type": "integer", "minimum": 1, "maximum": 100},
		}
		for name, property := range extra {
			properties[name] = property
		}
		return properties
	}
	read := annotations(true, false, false)
	call := func() object { return schema(with(nil), "call_id", "expected_version", keyName) }
	tool := func(name, description string, input object, notes *mcp.ToolAnnotations) *mcp.Tool {
		return &mcp.Tool{Name: name, Description: description, InputSchema: input, Annotations: notes}
	}
	return []*mcp.Tool{
		tool(control.StatusGet, "Registration, DND and the current call (no names or numbers).", schema(object{}), read),
		tool(control.OperationGet, "State and sanitized outcome of an earlier request.",
			schema(object{"operation_id": object{"type": "string", "pattern": "^[A-Za-z0-9._:-]{1,64}$"}}, "operation_id"), read),
		tool(control.DevicesList, "Audio devices (needs a read grant).", schema(object{}), read),
		tool(control.ContactsSearch, "Search contacts (needs a read grant).",
			schema(page(object{"query": object{"type": "string", "maxLength": 64}}), "query"), read),
		tool(control.HistoryList, "Call history (needs a read grant).",
			schema(page(object{"filter": object{"type": "string", "enum": []string{"all", "incoming", "outgoing", "missed"}}})), read),
		tool(control.ConfigShow, "Sanitized configuration (needs a read grant).", schema(object{}), read),
		tool(control.CallDial, "Place a call."+pending,
			schema(object{"target": targetProperty, keyName: keyProperty}, "target", keyName), annotations(false, false, true)),
		tool(control.CallAnswer, "Answer the ringing call; starts the microphone."+pending, call(), annotations(false, false, true)),
		tool(control.CallReject, "Reject the ringing call."+pending, call(), annotations(false, true, true)),
		tool(control.CallHangup, "End the call."+pending, call(), annotations(false, true, true)),
		tool(control.CallDTMF, "Send DTMF digits."+pending,
			schema(with(object{"digits": object{"type": "string", "pattern": "^[0-9*#A-D]{1,32}$"}}), "call_id", "expected_version", "digits", keyName),
			annotations(false, false, true)),
		tool(control.CallMute, "Mute or unmute the microphone."+pending,
			schema(with(object{"muted": object{"type": "boolean"}}), "call_id", "expected_version", "muted", keyName), annotations(false, false, false)),
		tool(control.CallHold, "Hold or resume the call."+pending,
			schema(with(object{"held": object{"type": "boolean"}}), "call_id", "expected_version", "held", keyName), annotations(false, false, true)),
		tool(control.CallTransfer, "Blind-transfer the call."+pending,
			schema(with(object{"target": targetProperty}), "call_id", "expected_version", "target", keyName), annotations(false, true, true)),
		tool(control.DNDSet, "Turn do-not-disturb on or off."+pending,
			schema(object{"enabled": object{"type": "boolean"}, keyName: keyProperty}, "enabled", keyName), annotations(false, false, false)),
	}
}

func invalid(message string) error { return &control.Error{Code: control.Validation, Message: message} }

// toRequest makes the control request of a tool call. An unknown tool, an unknown argument and
// an argument of the wrong type are refused, without repeating what was sent.
func toRequest(name string, arguments json.RawMessage, requestID string) (control.Request, error) {
	if !slices.ContainsFunc(Tools(), func(t *mcp.Tool) bool { return t.Name == name }) {
		return control.Request{}, invalid("unknown tool")
	}
	var given map[string]json.RawMessage
	if len(arguments) > 0 && json.Unmarshal(arguments, &given) != nil {
		return control.Request{}, invalid("arguments do not match the tool schema")
	}
	if given == nil { // no arguments, or null
		given = map[string]json.RawMessage{}
	}
	r := control.Request{SchemaVersion: control.SchemaVersion, RequestID: requestID}
	if raw, has := given[keyName]; has {
		var key string
		if json.Unmarshal(raw, &key) != nil {
			return control.Request{}, invalid("idempotency_key must be a string")
		}
		r.IdempotencyKey = &key
		delete(given, keyName)
	}
	operation := object{"op": name}
	if len(given) > 0 || name == control.ContactsSearch || name == control.HistoryList {
		operation["args"] = given
	}
	wire, _ := json.Marshal(operation)
	if json.Unmarshal(wire, &r.Operation) != nil {
		return control.Request{}, invalid("arguments do not match the tool schema")
	}
	return r, r.Validate()
}

// toResult is the tool result of an owner's answer: the answer itself, flagged as an error when
// the request failed. approval_required is not an error.
func toResult(response control.Response) *mcp.CallToolResult {
	text, _ := json.Marshal(response)
	var structured object
	_ = json.Unmarshal(text, &structured)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(text)}}, StructuredContent: structured,
		IsError: response.Status == control.StatusError}
}

// server is the state of one MCP server.
type server struct {
	reach      Reach
	capability *string

	mu     sync.Mutex
	window time.Time
	used   int
	next   uint64
}

// begin counts one call in the one-second window and names its request.
func (s *server) begin() (requestID string, allowed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now := time.Now(); now.Sub(s.window) >= time.Second {
		s.window, s.used = now, 0
	}
	s.used++
	s.next++
	return fmt.Sprintf("mcp-%d-%d", os.Getpid(), s.next), s.used <= ratePerSecond
}

func (s *server) call(_ context.Context, call *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	requestID, allowed := s.begin()
	if !allowed {
		return toResult(control.Failure(requestID, &control.Error{Code: control.RateLimited, Message: "too many tool calls"})), nil
	}
	request, err := toRequest(call.Params.Name, call.Params.Arguments, requestID)
	if err != nil {
		return toResult(control.Failure(requestID, err)), nil
	}
	request.Capability = s.capability
	return toResult(s.reach(request)), nil
}

func newServer(reach Reach, capability *string) *mcp.Server {
	info := buildinfo.Get()
	out := mcp.NewServer(&mcp.Implementation{Name: "go-microsip", Version: info.Version}, nil)
	s := &server{reach: reach, capability: capability}
	for _, tool := range Tools() {
		out.AddTool(tool, s.call)
	}
	return out
}
