//go:build (linux && (cgo || purego)) || windows || darwin

package cmd

import (
	"github.com/spf13/cobra"

	"github.com/jniltinho/go-microsip/internal/gui"
)

// This file links the desktop window. A Linux build with neither cgo nor the purego tag leaves it
// out and keeps the command line and the MCP server.
func init() {
	runWindow = func(*cobra.Command) error {
		return gui.Run(gui.Options{SafeGPU: safeGPU, SelfTest: selfTest})
	}
}
