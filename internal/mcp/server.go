// Package mcp is the stdio Model Context Protocol server. It opens no network port and never
// imports the desktop toolkit, so it runs without a display.
//
// G0 scope: the transport and one read-only tool, to prove the headless path. The softphone's
// tools and their grants arrive with milestone M4.
package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jniltinho/go-microsip/internal/buildinfo"
)

// Serve runs the server on standard input and output until the client disconnects.
func Serve(ctx context.Context) error {
	return newServer().Run(ctx, &mcp.StdioTransport{})
}

func newServer() *mcp.Server {
	info := buildinfo.Get()
	s := mcp.NewServer(&mcp.Implementation{Name: "go-microsip", Version: info.Version}, nil)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "status_get",
		Description: "Report the application's build and whether a softphone instance is running.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, statusGet)
	return s
}

// status is the status_get result.
type status struct {
	Build buildinfo.Info `json:"build"`
	// Instance is "none" until the engine and the control transport exist (M1, M4).
	Instance string `json:"instance"`
}

func statusGet(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, status, error) {
	return nil, status{Build: buildinfo.Get(), Instance: "none"}, nil
}
