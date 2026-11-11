package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jniltinho/go-microsip/internal/buildinfo"
)

// doctorReport is what `doctor` prints. G0 scope: the build and the session; the configuration
// layers with their provenance arrive with the core (M1).
type doctorReport struct {
	Build buildinfo.Info `json:"build"`
	// Display tells whether a desktop session is visible to this process.
	Display bool `json:"display"`
}

func init() {
	var asJSON bool
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Print a sanitized report of the build and the environment",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			r := doctorReport{
				Build:   buildinfo.Get(),
				Display: os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("DISPLAY") != "",
			}
			if asJSON {
				enc := json.NewEncoder(c.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(r)
			}
			fmt.Fprintf(c.OutOrStdout(), "go-microsip %s (%s) %s/%s\nwindow backend: %s\ndisplay: %t\n",
				r.Build.Version, r.Build.Commit, r.Build.OS, r.Build.Arch, r.Build.Window, r.Display)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON")
	rootCmd.AddCommand(c)
}
