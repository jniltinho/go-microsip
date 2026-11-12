package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jniltinho/go-microsip/internal/control"
	"github.com/jniltinho/go-microsip/internal/core/ports/memory"
)

// The control commands of the Rust candidate (apps/rust-microsip/src/cli.rs) with the Cobra
// syntax, against an owner on a socket of the test's own.

// recorder is an owner that writes down what it is asked and answers as told.
type recorder struct {
	mu       sync.Mutex
	requests []control.Request
	answer   func(control.Request) control.Response
}

func (r *recorder) Handle(request control.Request, _ int) control.Response {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, request)
	if r.answer != nil {
		return r.answer(request)
	}
	return control.OK(request.RequestID, map[string]string{"registration": "registered"})
}

func (r *recorder) last(t *testing.T) control.Request {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.requests) == 0 {
		t.Fatal("nothing reached the owner")
	}
	return r.requests[len(r.requests)-1]
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

// session is a runtime directory with an owner of the given kind listening in it.
func owned(t *testing.T, mode control.Mode) (env map[string]string, owner *recorder) {
	t.Helper()
	env = map[string]string{"XDG_RUNTIME_DIR": t.TempDir()}
	dir, err := control.RuntimeDir(func(name string) string { return env[name] })
	if err != nil {
		t.Fatal(err)
	}
	owner = &recorder{}
	endpoint, err := control.Listen(dir, control.Welcome{Protocol: control.Protocol, Version: control.SchemaVersion, Mode: mode, Epoch: 4}, owner)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = endpoint.Close() })
	return env, owner
}

// typed runs a command line with this standard input.
func typed(env map[string]string, input string, args ...string) (int, string) {
	var out bytes.Buffer
	code := execute(&session{platform: Platform{Files: me{}, Locks: &memory.Locks{}}, env: env, stdout: &out, stderr: &out,
		stdin: strings.NewReader(input), version: "1.2.3", commit: "abc1234"}, args)
	return code, out.String()
}

func TestControlCommandsMapToOperations(t *testing.T) {
	env, owner := owned(t, control.ModeWindow)
	for _, c := range []struct {
		line     string
		want     string // the operation on the wire
		mutation bool
	}{
		{"status", `{"op":"status_get"}`, false},
		{"status --json", `{"op":"status_get"}`, false},
		{"operation get op-4-abc", `{"op":"operation_get","args":{"operation_id":"op-4-abc"}}`, false},
		{"devices list", `{"op":"devices_list"}`, false},
		{"config show", `{"op":"config_show"}`, false},
		{"contacts search", `{"op":"contacts_search","args":{"query":""}}`, false},
		{"contacts search ana --limit 5 --cursor o:20", `{"op":"contacts_search","args":{"cursor":"o:20","limit":5,"query":"ana"}}`, false},
		{"history list", `{"op":"history_list","args":{}}`, false},
		{"history list --filter missed --limit 100", `{"op":"history_list","args":{"filter":"missed","limit":100}}`, false},
		{"call dial 1002", `{"op":"call_dial","args":{"target":"1002"}}`, true},
		{"call answer 3 2", `{"op":"call_answer","args":{"call_id":3,"expected_version":2}}`, true},
		{"call reject 3 2", `{"op":"call_reject","args":{"call_id":3,"expected_version":2}}`, true},
		{"call hangup 3 2", `{"op":"call_hangup","args":{"call_id":3,"expected_version":2}}`, true},
		{"call mute 3 2", `{"op":"call_mute","args":{"call_id":3,"expected_version":2,"muted":true}}`, true},
		{"call unmute 3 2", `{"op":"call_mute","args":{"call_id":3,"expected_version":2,"muted":false}}`, true},
		{"call hold 3 2", `{"op":"call_hold","args":{"call_id":3,"expected_version":2,"held":true}}`, true},
		{"call resume 3 2", `{"op":"call_hold","args":{"call_id":3,"expected_version":2,"held":false}}`, true},
		{"call dtmf 3 2 12#", `{"op":"call_dtmf","args":{"call_id":3,"digits":"12#","expected_version":2}}`, true},
		{"call transfer 3 2 1003", `{"op":"call_transfer","args":{"call_id":3,"expected_version":2,"target":"1003"}}`, true},
		{"dnd on", `{"op":"dnd_set","args":{"enabled":true}}`, true},
		{"dnd off --key my-key-1", `{"op":"dnd_set","args":{"enabled":false}}`, true},
	} {
		before := owner.count()
		code, out := cli(env, strings.Fields(c.line)...)
		if code != 0 || owner.count() != before+1 {
			t.Errorf("%s: status %d, %d requests\n%s", c.line, code, owner.count()-before, out)
			continue
		}
		request := owner.last(t)
		wire, _ := json.Marshal(request.Operation)
		if string(wire) != c.want || (request.IdempotencyKey != nil) != c.mutation || request.Capability != nil || !strings.HasPrefix(request.RequestID, "req-") {
			t.Errorf("%s reached the owner as %s (key %v)", c.line, wire, request.IdempotencyKey)
		}
		if c.mutation && !strings.Contains(out, "idempotency key: "+*request.IdempotencyKey) {
			t.Errorf("%s does not print its key:\n%s", c.line, out)
		}
		if strings.Contains(c.line, "--key") && *request.IdempotencyKey != "my-key-1" {
			t.Errorf("%s: the key sent is %q", c.line, *request.IdempotencyKey)
		}
		if strings.Contains(c.line, "--json") {
			var response control.Response
			if json.Unmarshal([]byte(out), &response) != nil || response.Status != control.StatusOK || strings.Count(out, "\n") != 1 {
				t.Errorf("%s: the JSON answer:\n%s", c.line, out)
			}
		} else if !strings.HasPrefix(out, "ok\n") || !strings.Contains(out, `"registration": "registered"`) {
			t.Errorf("%s: the answer for a person:\n%s", c.line, out)
		}
	}
	// Two random keys are two keys.
	cli(env, "dnd", "on")
	first := *owner.last(t).IdempotencyKey
	cli(env, "dnd", "on")
	if second := *owner.last(t).IdempotencyKey; first == second || !control.Token(first, 128) {
		t.Errorf("two requests without --key share the key %q", first)
	}
}

func TestControlAnswersBecomeExitStatuses(t *testing.T) {
	env, owner := owned(t, control.ModeWindow)
	fail := func(code control.Code) func(control.Request) control.Response {
		return func(r control.Request) control.Response {
			return control.Failure(r.RequestID, &control.Error{Code: code, Message: "said by the owner"})
		}
	}
	for name, c := range map[string]struct {
		answer func(control.Request) control.Response
		status int
		text   string
	}{
		"waiting for a person": {func(r control.Request) control.Response {
			return control.Pending(r.RequestID, control.StatusApprovalRequired, "op-4-abc")
		}, 3, "waiting for approval in the go-microsip window\noperation: op-4-abc\nidempotency key: "},
		"taken": {func(r control.Request) control.Response {
			return control.Pending(r.RequestID, control.StatusAccepted, "op-4-def")
		}, 0, "accepted\noperation: op-4-def\n"},
		"not authorized":  {fail(control.GrantRequired), 3, "error: said by the owner (grant_required)\n"},
		"a conflict":      {fail(control.Conflict), 2, "(conflict)"},
		"the owner busy":  {fail(control.Busy), 4, "(busy)"},
		"an old session":  {fail(control.StaleEpoch), 3, "(stale_epoch)"},
		"something broke": {fail(control.Internal), 1, "(internal)"},
	} {
		owner.answer = c.answer
		if code, out := cli(env, "call", "dial", "1002"); code != c.status || !strings.Contains(out, c.text) {
			t.Errorf("%s: status %d, want %d\n%s", name, code, c.status, out)
		}
	}
	// What cannot be a request is refused here, with the usage status, and is not repeated.
	before := owner.count()
	for _, line := range []string{"call dial not-a-number;rm", "call answer x 2", "call answer 3 y", "call answer 3", "call dtmf 3 2 12E", "dnd maybe",
		"history list --filter everything", "history list --limit 0", "operation get ../x", "call dial 1002 --key a/b", "status extra",
		"contacts search a b", "call teleport 1 2", "call dial 1002 --yes", "approve op-4-abc"} {
		code, out := cli(env, strings.Fields(line)...)
		if code != 2 || strings.Contains(out, "rm") && strings.Contains(out, "not-a-number") {
			t.Errorf("%s: status %d\n%s", line, code, out)
		}
	}
	if owner.count() != before {
		t.Errorf("%d invalid command lines reached the owner", owner.count()-before)
	}
}

func TestControlWithoutAnOwnerStartsNothing(t *testing.T) {
	env := map[string]string{"XDG_RUNTIME_DIR": t.TempDir()}
	code, out := cli(env, "status", "--json")
	var response control.Response
	if code != 4 || json.Unmarshal([]byte(out), &response) != nil || response.Error == nil || response.Error.Code != control.EngineUnavailable || !response.Error.Retryable {
		t.Errorf("status without an owner: %d\n%s", code, out)
	}
	if entries, _ := os.ReadDir(env["XDG_RUNTIME_DIR"]); len(entries) != 0 {
		t.Errorf("the command left something in the runtime directory: %v", entries)
	}
	if code, out := cli(env, "call", "dial", "1002"); code != 4 || !strings.Contains(out, "go-microsip is not running; nothing was started (engine_unavailable)") {
		t.Errorf("a call without an owner: %d\n%s", code, out)
	}
	// Without a runtime directory there is nowhere an owner could be.
	if code, out := cli(nil, "status"); code != 4 || !strings.Contains(out, "engine_unavailable") {
		t.Errorf("status without a runtime directory: %d\n%s", code, out)
	}
}

func TestTargetsAndDigitsCanComeFromTheStandardInput(t *testing.T) {
	env, owner := owned(t, control.ModeWindow)
	for line, want := range map[string]string{
		"call dial -":         `{"op":"call_dial","args":{"target":"1002"}}`,
		"call transfer 3 2 -": `{"op":"call_transfer","args":{"call_id":3,"expected_version":2,"target":"1002"}}`,
	} {
		if code, out := typed(env, "1002\nsomething else\n", strings.Fields(line)...); code != 0 {
			t.Errorf("%s: %d\n%s", line, code, out)
		}
		if wire, _ := json.Marshal(owner.last(t).Operation); string(wire) != want {
			t.Errorf("%s reached the owner as %s", line, wire)
		}
	}
	if code, out := typed(env, "90#\n", "call", "dtmf", "3", "2", "-"); code != 0 {
		t.Errorf("digits from the standard input: %d\n%s", code, out)
	}
	if wire, _ := json.Marshal(owner.last(t).Operation); string(wire) != `{"op":"call_dtmf","args":{"call_id":3,"digits":"90#","expected_version":2}}` {
		t.Errorf("digits from the standard input reached the owner as %s", wire)
	}
	before := owner.count()
	if code, _ := typed(env, "", "call", "dial", "-"); code != 2 || owner.count() != before {
		t.Errorf("an empty standard input: status %d", code)
	}
}

func TestAGrantFileGoesWithTheRequestAndIsNeverPrinted(t *testing.T) {
	env, owner := owned(t, control.ModeWindow)
	dir := t.TempDir()
	secret := strings.Repeat("a", 64)
	grant := write(t, filepath.Join(dir, "g.grant"), secret+"\n")
	for _, line := range [][]string{{"history", "list", "--grant-file", grant}, {"call", "dial", "1002", "--grant-file", grant, "--json"}} {
		code, out := cli(env, line...)
		if code != 0 || strings.Contains(out, secret) {
			t.Errorf("%v: %d\n%s", line, code, out)
		}
		if got := owner.last(t).Capability; got == nil || *got != secret {
			t.Errorf("%v: the capability did not go with the request", line)
		}
	}
	// A bootstrap file is a grant file too: its capability is the service's grant.
	boot := write(t, filepath.Join(dir, "b.json"), `{"bootstrap_id":"boot-4-abc","capability":"`+secret+`"}`+"\n")
	if code, _ := cli(env, "status", "--grant-file", boot); code != 0 || *owner.last(t).Capability != secret {
		t.Errorf("a bootstrap file as a grant file: %d", code)
	}
	// A grant file others can read, or that holds no grant, is refused before anything is sent.
	before := owner.count()
	loose := filepath.Join(dir, "loose.grant")
	if err := os.WriteFile(loose, []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"readable by others": loose, "missing": filepath.Join(dir, "none"), "no grant": write(t, filepath.Join(dir, "w.grant"), "two words\n")} {
		code, out := cli(env, "status", "--grant-file", path)
		if code != 2 || strings.Contains(out, secret) || !strings.Contains(out, "grant file") {
			t.Errorf("a grant file %s: %d\n%s", name, code, out)
		}
	}
	if owner.count() != before {
		t.Error("a request went out with a grant file that was refused")
	}
}

func TestTheServiceTurnsTheWindowAndLinksAway(t *testing.T) {
	env, _ := owned(t, control.ModeService)
	if code, out := cli(env); code != 4 || !strings.Contains(out, "engine_owned_by_service") {
		t.Errorf("the window beside the service: %d\n%s", code, out)
	}
	if code, out := cli(env, "tel:1002"); code != 4 || !strings.Contains(out, "uri_confirmation_unavailable") || strings.Contains(out, "1002") {
		t.Errorf("a link beside the service: %d\n%s", code, out)
	}
	// Beside a window, or beside nothing, the window's own start decides.
	window, _ := owned(t, control.ModeWindow)
	if (&session{env: window}).servedByService() || (&session{env: map[string]string{"XDG_RUNTIME_DIR": t.TempDir()}}).servedByService() {
		t.Error("a window, or nothing, passed for the headless service")
	}
}

func TestTheServiceNeedsABootstrapFile(t *testing.T) {
	env := map[string]string{"XDG_RUNTIME_DIR": t.TempDir()}
	dir := t.TempDir()
	if code, out := cli(env, "service"); code != 2 || !strings.Contains(out, "--grant-file FILE") {
		t.Errorf("the service without a grant file: %d\n%s", code, out)
	}
	secret := strings.Repeat("b", 64)
	plain := write(t, filepath.Join(dir, "g.grant"), secret+"\n")
	if code, out := cli(env, "service", "--grant-file", plain); code != 2 || !strings.Contains(out, "not a bootstrap grant") || strings.Contains(out, secret) {
		t.Errorf("the service with a session's grant: %d\n%s", code, out)
	}
	boot := write(t, filepath.Join(dir, "b.json"), `{"bootstrap_id":"boot-4-abc","capability":"`+secret+`"}`)
	if code, out := cli(map[string]string{}, "service", "--grant-file", boot); code != 4 || !strings.Contains(out, "runtime_unavailable") {
		t.Errorf("the service without a runtime directory: %d\n%s", code, out)
	}
	// With no account configured it cannot be the softphone.
	if code, out := cli(env, "service", "--grant-file", boot); code != 1 || !strings.Contains(out, "configuration: INVALID") || strings.Contains(out, secret) {
		t.Errorf("the service without a configuration: %d\n%s", code, out)
	}
}
