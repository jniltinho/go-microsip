// go-microsip is a compact SIP softphone: one binary that is the desktop window (the default), the
// command line and the local MCP server.
package main

import "github.com/jniltinho/go-microsip/cmd"

// version and commit are set at build time:
// go build -ldflags "-X main.version=0.2.0 -X main.commit=<sha>".
var (
	version = "dev"
	commit  = "unknown"
)

func main() { cmd.Execute(version, commit) }
