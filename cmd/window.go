//go:build (linux && (cgo || purego)) || windows || darwin

package cmd

import (
	"github.com/spf13/cobra"

	"github.com/jniltinho/go-microsip/internal/gui"
)

// This file links the desktop window. A Linux build with neither cgo nor the purego tag leaves it
// out and keeps the command line and the MCP server.
func init() {
	runWindow = func(_ *cobra.Command, s *session) error {
		return gui.Run(gui.Options{
			SafeGPU: safeGPU, SelfTest: selfTest, Trace: uiTrace,
			Core: s.options(), Audio: s.platform.Audio, UserAgent: "go-microsip/" + s.version,
		})
	}
}
