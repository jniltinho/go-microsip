package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jniltinho/go-microsip/internal/buildinfo"
	"github.com/jniltinho/go-microsip/internal/core/app"
)

// doctorReport is what `doctor` prints: the build, the session and the sanitized configuration.
type doctorReport struct {
	Build buildinfo.Info `json:"build"`
	// Display tells whether a desktop session is visible to this process.
	Display       bool             `json:"display"`
	Configuration app.DoctorReport `json:"configuration"`
}

// doctor never registers or dials, and exits 0 also when the configuration is invalid: the report
// says so.
func (s *session) doctor() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Print a sanitized report of the build, the session and the configuration",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			build := buildinfo.Get()
			build.Version, build.Commit = s.version, s.commit
			r := doctorReport{
				Build:         build,
				Display:       s.getenv("WAYLAND_DISPLAY") != "" || s.getenv("DISPLAY") != "",
				Configuration: app.Doctor(s.options()),
			}
			if asJSON {
				enc := json.NewEncoder(c.OutOrStdout())
				enc.SetEscapeHTML(false)
				enc.SetIndent("", "  ")
				return enc.Encode(r)
			}
			fmt.Fprintf(c.OutOrStdout(), "go-microsip %s (%s) %s/%s\nwindow backend: %s\ndisplay: %t\n%s",
				r.Build.Version, r.Build.Commit, r.Build.OS, r.Build.Arch, r.Build.Window, r.Display, r.Configuration)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON")
	return c
}
