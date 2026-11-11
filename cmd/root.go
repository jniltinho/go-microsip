// Package cmd is the command line: one file per command, built with Cobra. Nothing here imports the
// desktop toolkit; the window is reached only through the gui command's function value, so every
// other command runs without a display.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jniltinho/go-microsip/internal/buildinfo"
)

// Window flags, read by the build that links the window.
var (
	safeGPU  bool
	selfTest bool
)

var rootCmd = &cobra.Command{
	Use:           "go-microsip",
	Short:         "Compact SIP softphone: desktop window, command line and MCP server",
	SilenceUsage:  true,
	SilenceErrors: true,
	Args:          cobra.NoArgs,
	RunE:          func(c *cobra.Command, _ []string) error { return runWindow(c) },
}

// runWindow opens the desktop window. It is replaced by the build that links the GUI; a build
// without it (CGO_ENABLED=0 on Linux, for instance) still provides every other command.
var runWindow = func(c *cobra.Command) error {
	return fmt.Errorf("this build has no desktop window; see `%s --help` for the commands", c.CommandPath())
}

// Execute runs the command line and exits with its status: 0 done, 2 invalid usage, 1 otherwise.
func Execute(version, commit string) {
	buildinfo.Set(version, commit)
	rootCmd.Flags().BoolVar(&safeGPU, "safe-gpu", false, "start the window without GPU acceleration")
	rootCmd.Flags().BoolVar(&selfTest, "self-test", false, "open the window, wait for the page to load and exit 0")
	rootCmd.Version = version
	rootCmd.SetVersionTemplate("go-microsip {{.Version}} (" + commit + ")\n")
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version and the commit it was built from",
		Args:  cobra.NoArgs,
		Run: func(c *cobra.Command, _ []string) {
			fmt.Fprintf(c.OutOrStdout(), "go-microsip %s (%s)\n", version, commit)
		},
	})
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "go-microsip:", err)
		os.Exit(1)
	}
}
