// go-microsip is a compact SIP softphone: one binary that is the desktop window (the default), the
// command line and the local MCP server.
package main

import (
	"github.com/jniltinho/go-microsip/cmd"
	"github.com/jniltinho/go-microsip/internal/platform/audio"
	"github.com/jniltinho/go-microsip/internal/platform/files"
	"github.com/jniltinho/go-microsip/internal/platform/keyring"
	"github.com/jniltinho/go-microsip/internal/platform/lock"
	"github.com/jniltinho/go-microsip/internal/platform/notify"
)

// version and commit are set at build time:
// go build -ldflags "-X main.version=0.2.0 -X main.commit=<sha>".
var (
	version = "dev"
	commit  = "unknown"
)

// main is the only place that wires operating-system adapters into the front ends.
func main() {
	cmd.Execute(cmd.Platform{
		Files: files.Access{}, Audio: &audio.Devices{}, Locks: lock.Files{},
		Secrets: keyring.Store{}, LegacySecrets: keyring.Legacy{}, Notice: notify.New(),
	}, version, commit)
}
