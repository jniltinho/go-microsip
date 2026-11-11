// Package cmd is the command line: one file per command, built with Cobra. Nothing here imports the
// desktop toolkit; the window is reached only through the gui command's function value, so every
// other command runs without a display.
package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/jniltinho/go-microsip/internal/buildinfo"
	"github.com/jniltinho/go-microsip/internal/core/app"
	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// Platform is what main wires in from the operating-system adapters.
type Platform struct {
	// Files reports the ownership and permissions of configuration files.
	Files ports.FileAccess
	// Audio opens the sound devices; nil when this build has no sound service.
	Audio ports.AudioDevices
	// Locks takes instance locks; nil when this build cannot.
	Locks ports.Locker
}

// Window flags, read by the build that links the window.
var (
	safeGPU  bool
	selfTest bool
)

// runWindow opens the desktop window. It is replaced by the build that links the GUI; a build
// without it (CGO_ENABLED=0 on Linux, for instance) still provides every other command.
var runWindow = func(c *cobra.Command) error {
	return fmt.Errorf("this build has no desktop window; see `%s --help` for the commands", c.CommandPath())
}

// failure ends the process with a status other than the usage error: 1 unless said otherwise. Its
// message is printed as it is; an empty one prints nothing.
type failure struct {
	message string
	code    int
}

func (f *failure) Error() string { return f.message }

// failed is the status-1 failure of a command, prefixed with the program name.
func failed(err error) error { return &failure{message: "go-microsip: " + err.Error(), code: 1} }

// invalidConfiguration is how every command reports a configuration it cannot use.
func invalidConfiguration(err error) error {
	return &failure{message: "configuration: INVALID\nerror: " + err.Error(), code: 1}
}

// session is one run of the command line: the platform, where to write, and the global options.
type session struct {
	platform        Platform
	env             map[string]string // nil: the process environment
	stdout, stderr  io.Writer
	version, commit string
	configPath      string
	envFile         string
}

func (s *session) getenv(name string) string {
	if s.env != nil {
		return s.env[name]
	}
	return os.Getenv(name)
}

// options are the core's options for this run.
func (s *session) options() app.Options {
	return app.Options{
		ConfigPath: s.configPath, EnvFile: s.envFile, Files: s.platform.Files, Env: s.env, Locks: s.platform.Locks,
		Warn: func(message string) { fmt.Fprintln(s.stderr, "go-microsip:", message) },
	}
}

func (s *session) root() *cobra.Command {
	root := &cobra.Command{
		Use:   "go-microsip",
		Short: "Compact SIP softphone: desktop window, command line and MCP server",
		Long: `Compact SIP softphone: desktop window, command line and MCP server.

Without a command it opens the window. Passwords are never accepted as arguments: use
account.password_env, a private account.password_file, or the prompt of the window.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := runWindow(c); err != nil {
				return failed(err)
			}
			return nil
		},
	}
	root.PersistentFlags().StringVar(&s.configPath, "config", "", "use this TOML `file` instead of the default one")
	root.PersistentFlags().StringVar(&s.envFile, "env-file", "", "load settings from this .env `file` (never discovered implicitly)")
	root.Flags().BoolVar(&safeGPU, "safe-gpu", false, "start the window without GPU acceleration")
	root.Flags().BoolVar(&selfTest, "self-test", false, "open the window, wait for the page to load and exit 0")
	root.Version = s.version
	root.SetVersionTemplate("go-microsip {{.Version}} (" + s.commit + ")\n")
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version and the commit it was built from",
		Args:  cobra.NoArgs,
		Run: func(c *cobra.Command, _ []string) {
			fmt.Fprintf(c.OutOrStdout(), "go-microsip %s (%s)\n", s.version, s.commit)
		},
	}, s.doctor(), s.config(), s.importCommand(), s.run(), mcpCommand())
	return root
}

// execute runs one command line and returns its status: 0 done, 2 invalid usage, 1 otherwise (a
// command may document more). Usage errors never echo an argument: it may be a secret.
func execute(s *session, args []string) int {
	// Before Cobra sees the arguments: a password must never be parsed, echoed or logged.
	if err := app.CheckArguments(args); err != nil {
		fmt.Fprintln(s.stderr, "go-microsip:", err)
		return 2
	}
	root := s.root()
	root.SetArgs(args)
	root.SetOut(s.stdout)
	root.SetErr(s.stderr)
	err := root.Execute()
	var f *failure
	switch {
	case err == nil:
		return 0
	case errors.As(err, &f):
		if f.message != "" {
			fmt.Fprintln(s.stderr, f.message)
		}
		return f.code
	}
	fmt.Fprintln(s.stderr, "go-microsip: invalid or unsupported arguments; see --help")
	return 2
}

// Execute runs the command line of the process and exits with its status.
func Execute(p Platform, version, commit string) {
	buildinfo.Set(version, commit)
	os.Exit(execute(&session{platform: p, stdout: os.Stdout, stderr: os.Stderr, version: version, commit: commit}, os.Args[1:]))
}
