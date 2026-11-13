package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jniltinho/go-microsip/internal/control"
	"github.com/jniltinho/go-microsip/internal/core/ports/memory"
)

// Task 6.6: every go-microsip command written in docs/agent-cli-mcp.md and in the README is a
// command of this build, with these options and this many arguments; and each control command
// is run as written, against an owner on a socket of the test's own, which must receive it.

// documented returns the go-microsip command lines of a document: those of its sh blocks, with
// what is piped into them, and those of its inline code.
func documented(t *testing.T, path string) (blocks []documentedLine, inline []string) {
	t.Helper()
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fenced := regexp.MustCompile("(?s)```sh\n(.*?)```")
	for _, block := range fenced.FindAllStringSubmatch(string(text), -1) {
		for _, line := range strings.Split(block[1], "\n") {
			if i := strings.Index(line, "  #"); i >= 0 { // a comment after the command
				line = line[:i]
			}
			line = strings.TrimSpace(line)
			var input string
			if echoed, rest, piped := strings.Cut(line, " | "); piped {
				input, line = strings.TrimPrefix(echoed, "echo ")+"\n", rest
			}
			if _, rest, wrapped := strings.Cut(line, " -- "); wrapped { // "claude mcp add go-microsip -- go-microsip mcp"
				line = rest
			}
			if strings.HasPrefix(line, "go-microsip") {
				blocks = append(blocks, documentedLine{strings.Fields(line)[1:], input})
			}
		}
	}
	outside := fenced.ReplaceAllString(string(text), "")
	for _, span := range regexp.MustCompile("`(go-microsip [^`]+)`").FindAllStringSubmatch(outside, -1) {
		inline = append(inline, span[1])
	}
	return blocks, inline
}

type documentedLine struct {
	args  []string
	input string
}

func TestEveryDocumentedCommandRunsAsWritten(t *testing.T) {
	env, owner := owned(t, control.ModeWindow)
	dir := t.TempDir()
	grant := write(t, filepath.Join(dir, "grant.json"), `{"bootstrap_id":"boot-4-abc","capability":"`+strings.Repeat("a", 64)+`"}`)
	fixtures := map[string]string{"FILE": grant, "OPERATION_ID": "op-4-abc", "CALL_ID": "3", "VERSION": "2"}
	// The commands that talk to the running softphone are run; the others are checked against
	// the command tree: they would start a server, a softphone, a window or an import here.
	control_ := map[string]bool{"status": true, "operation": true, "call": true, "dnd": true, "devices": true, "contacts": true, "history": true}
	parsed := func(args []string) {
		t.Helper()
		root := (&session{platform: Platform{Files: me{}, Locks: &memory.Locks{}}, env: env, version: "1.2.3"}).root()
		command, rest, err := root.Find(args)
		if err != nil {
			t.Errorf("go-microsip %s: %v", strings.Join(args, " "), err)
			return
		}
		if err := command.ParseFlags(rest); err != nil {
			// --help is the one option that stops the parsing: it is run instead.
			if code, _ := cli(env, args...); err.Error() != "pflag: help requested" || code != 0 {
				t.Errorf("go-microsip %s: %v", strings.Join(args, " "), err)
			}
			return
		}
		if err := command.ValidateArgs(command.Flags().Args()); err != nil {
			t.Errorf("go-microsip %s: %v", strings.Join(args, " "), err)
		}
		if command == root && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			t.Errorf("go-microsip %s is no command of this build", strings.Join(args, " "))
		}
	}
	ran, checked := 0, 0
	for _, document := range []string{filepath.Join("..", "docs", "agent-cli-mcp.md"), filepath.Join("..", "README.md")} {
		blocks, inline := documented(t, document)
		for _, line := range blocks {
			args := make([]string, len(line.args))
			for i, word := range line.args {
				args[i] = word
				if fixture, placeholder := fixtures[word]; placeholder {
					args[i] = fixture
				}
			}
			written := "go-microsip " + strings.Join(line.args, " ")
			if len(args) == 0 || !(control_[args[0]] || args[0] == "config" && len(args) > 1 && args[1] == "show") {
				parsed(args)
				checked++
				continue
			}
			before := owner.count()
			code, out := typed(env, line.input, args...)
			if code != 0 || owner.count() != before+1 {
				t.Errorf("%s (%s): status %d, %d requests\n%s", written, filepath.Base(document), code, owner.count()-before, out)
				continue
			}
			ran++
			request := owner.last(t)
			wire, _ := json.Marshal(request.Operation)
			// What was typed is what the owner was asked: the command's words, the piped line, the grant.
			if want := strings.TrimSpace(line.input); want != "" && !strings.Contains(string(wire), `"`+want+`"`) {
				t.Errorf("%s with %q on the standard input reached the owner as %s", written, want, wire)
			}
			if op := request.Operation.Op; !strings.Contains(op, strings.ReplaceAll(args[0], "devices", "devices_list")[:3]) &&
				!(args[0] == "config" && op == control.ConfigShow) && !(args[0] == "operation" && op == control.OperationGet) {
				t.Errorf("%s reached the owner as %s", written, op)
			}
			if strings.Contains(written, "--grant-file") != (request.Capability != nil) {
				t.Errorf("%s: a capability went with the request: %v", written, request.Capability != nil)
			}
		}
		for _, span := range inline {
			// Inline code names commands with placeholders and alternatives; only those written
			// as one command line are checked.
			if strings.ContainsAny(span, "|[<") {
				continue
			}
			args := strings.Fields(span)[1:]
			for i, word := range args {
				if fixture, placeholder := fixtures[word]; placeholder {
					args[i] = fixture
				}
			}
			parsed(args)
			checked++
		}
	}
	t.Logf("%d documented commands were run against an owner, %d checked against the command tree", ran, checked)
	if ran < 25 || checked < 8 {
		t.Errorf("%d documented commands were run and %d checked: the documents or the patterns changed", ran, checked)
	}
	// The MCP client's configuration names the mcp command.
	text, _ := os.ReadFile(filepath.Join("..", "docs", "agent-cli-mcp.md"))
	var client struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	block := regexp.MustCompile("(?s)```json\n(.*?)```").FindSubmatch(text)
	if block == nil || json.Unmarshal(block[1], &client) != nil || len(client.Args) != 1 || client.Args[0] != "mcp" || !filepath.IsAbs(client.Command) {
		t.Errorf("the MCP client configuration in the document: %s", block)
	} else {
		parsed(client.Args)
	}
	// --help lists every command the documents name, and the make target of the README exists.
	_, help := cli(env, "--help")
	for _, name := range []string{"status", "operation", "call", "dnd", "devices", "contacts", "history", "config", "mcp", "service", "import", "doctor"} {
		if !regexp.MustCompile(`(?m)^  ` + name + `\s`).MatchString(help) {
			t.Errorf("--help does not list %s:\n%s", name, help)
		}
	}
	if makefile, _ := os.ReadFile(filepath.Join("..", "build", "go.mk")); !regexp.MustCompile(`(?m)^go-dist:`).Match(makefile) {
		t.Error("the README's make go-dist is no target of build/go.mk")
	}
	// Every tool and every exit status the document lists is the contract's.
	for _, tool := range []string{"status_get", "operation_get", "devices_list", "contacts_search", "history_list", "config_show", "call_dial",
		"call_answer", "call_reject", "call_hangup", "call_dtmf", "call_mute", "call_hold", "call_transfer", "dnd_set"} {
		if !strings.Contains(string(text), "`"+tool+"`") {
			t.Errorf("the document does not name the tool %s", tool)
		}
	}
	for _, code := range []string{"engine_unavailable", "grant_required", "conflict", "stale_epoch", "engine_owned_by_service", "uri_confirmation_unavailable", "approval_required"} {
		if !strings.Contains(string(text), "`"+code+"`") {
			t.Errorf("the document does not name %s", code)
		}
	}
}
