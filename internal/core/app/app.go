// Package app is the part of the core that front ends call besides its domain and ports: the
// command line, the window and the MCP server reach configuration, storage and the engine only
// through it.
package app

import "github.com/jniltinho/go-microsip/internal/core/config"

// CheckArguments refuses a command line that carries a password-like option (any option whose name
// contains "pass"). Run it on the raw arguments before anything parses or echoes them; the error
// never contains the argument.
func CheckArguments(args []string) error { return config.CheckArguments(args) }
