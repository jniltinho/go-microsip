package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jniltinho/go-microsip/internal/buildinfo"
	"github.com/jniltinho/go-microsip/internal/control"
	"github.com/jniltinho/go-microsip/internal/core/app"
)

// doctorReport is what `doctor` prints: the build, the session and the sanitized configuration.
type doctorReport struct {
	Build buildinfo.Info `json:"build"`
	// Display tells whether a desktop session is visible to this process.
	Display bool        `json:"display"`
	Audio   audioReport `json:"audio"`
	// Paths are where this run looks without --config; an empty one has no home in this session.
	Paths         pathsReport      `json:"paths"`
	Configuration app.DoctorReport `json:"configuration"`
}

// pathsReport names the default configuration file, the database and the folder of agent
// control, whether or not they exist.
type pathsReport struct {
	Configuration string `json:"configuration"`
	Database      string `json:"database"`
	Control       string `json:"control"`
}

func (p pathsReport) String() string {
	or := func(path string) string {
		if path == "" {
			return "none"
		}
		return path
	}
	return fmt.Sprintf("configuration %s, database %s, control %s", or(p.Configuration), or(p.Database), or(p.Control))
}

// paths asks the core and the control endpoint where they look; nothing is created.
func (s *session) paths() pathsReport {
	var p pathsReport
	p.Configuration, p.Database = s.options().Paths()
	p.Control, _ = control.RuntimeDir(s.getenv)
	return p
}

// audioReport says whether a call would have sound, and why not.
type audioReport struct {
	Available bool `json:"available"`
	// Error names what is missing when Available is false.
	Error   string `json:"error,omitempty"`
	Inputs  int    `json:"inputs"`
	Outputs int    `json:"outputs"`
}

func (a audioReport) String() string {
	if !a.Available {
		return "unavailable (" + a.Error + ")"
	}
	return fmt.Sprintf("available (%d inputs, %d outputs)", a.Inputs, a.Outputs)
}

// audio asks the sound service for its devices; it opens none.
func (s *session) audio() audioReport {
	if s.platform.Audio == nil {
		return audioReport{Error: "this build has no sound backend"}
	}
	inputs, err := s.platform.Audio.Inputs()
	if err != nil {
		return audioReport{Error: err.Error()}
	}
	outputs, err := s.platform.Audio.Outputs()
	if err != nil {
		return audioReport{Error: err.Error()}
	}
	return audioReport{Available: true, Inputs: len(inputs), Outputs: len(outputs)}
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
				Audio:         s.audio(),
				Paths:         s.paths(),
				Configuration: app.Doctor(s.options()),
			}
			if asJSON {
				enc := json.NewEncoder(c.OutOrStdout())
				enc.SetEscapeHTML(false)
				enc.SetIndent("", "  ")
				return enc.Encode(r)
			}
			fmt.Fprintf(c.OutOrStdout(), "go-microsip %s (%s) %s/%s\nwindow backend: %s\ndisplay: %t\naudio: %s\npaths: %s\n%s",
				r.Build.Version, r.Build.Commit, r.Build.OS, r.Build.Arch, r.Build.Window, r.Display, r.Audio, r.Paths, r.Configuration)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON")
	return c
}
