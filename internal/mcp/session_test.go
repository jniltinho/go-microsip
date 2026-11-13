package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jniltinho/go-microsip/internal/control"
)

// On a desktop's own session (the VM checks, build/vm/desktop.ps1): `go-microsip mcp`, started as
// an agent starts it beside the window that runs there, initializes, lists its tools, reaches that
// window, and is refused what needs a grant. The client takes the program's standard output for
// JSON-RPC and nothing else: a line that was not would end the session.
func TestTheMCPServerReachesTheWindowOfThisSession(t *testing.T) {
	exe := os.Getenv("GO_MICROSIP_MCP_EXE")
	if exe == "" {
		t.Skip("set GO_MICROSIP_MCP_EXE to the go-microsip program, in a session with its window (make go-vm)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "vm"}, nil).Connect(ctx, &mcp.CommandTransport{Command: exec.Command(exe, "mcp")}, nil)
	if err != nil {
		t.Fatalf("the MCP server did not start: %v", err)
	}
	defer cs.Close()
	listed, err := cs.ListTools(ctx, nil)
	if err != nil || len(listed.Tools) != 15 {
		t.Fatalf("the tools: %+v, %v", listed, err)
	}
	status, failed := call(t, cs, "status_get", nil)
	var data struct {
		Mode         control.Mode `json:"mode"`
		Registration string       `json:"registration"`
	}
	if failed || json.Unmarshal(status.Data, &data) != nil || data.Mode != control.ModeWindow {
		t.Fatalf("status_get: %+v (%+v)", status, status.Error)
	}
	if r, failed := call(t, cs, "history_list", nil); !failed || r.Error == nil || r.Error.Code != control.GrantRequired {
		t.Errorf("history_list without a grant: %+v", r)
	}
	t.Logf("go-microsip mcp: initialized, %d tools, the window answers (mode %s, registration %s), history_list without a grant is refused",
		len(listed.Tools), data.Mode, data.Registration)
}
