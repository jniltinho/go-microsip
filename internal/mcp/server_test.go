package mcp

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A client can initialize, list the tools and call the read-only one, in process.
func TestHandshakeListAndCall(t *testing.T) {
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := newServer().Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "status_get" {
		t.Fatalf("tools = %+v, want only status_get", tools.Tools)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "status_get"})
	if err != nil || res.IsError {
		t.Fatalf("status_get: %v %+v", err, res)
	}
}
