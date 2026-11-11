package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jniltinho/go-microsip/internal/core/app"
)

// config validates, exports and imports the configuration. None of its commands touches SIP or
// audio; each exits 0 when done and 1 when the configuration or the file cannot be used.
func (s *session) config() *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Validate, export or import the configuration",
		Args:  cobra.NoArgs, // so an unknown subcommand is a usage error, not the help text
		RunE:  func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	var apply bool
	imp := &cobra.Command{
		Use:   "import FILE",
		Short: "Preview the settings of a TOML file; with --apply, store them in the local database",
		Long: `Prints a sanitized preview of the settings in FILE. With --apply it then replaces the settings
stored in the local database, in one transaction. FILE is not modified and a password in it is
never imported.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			text, err := app.Import(s.options(), args[0], apply)
			var after *app.ImportError
			if errors.As(err, &after) {
				fmt.Fprint(c.OutOrStdout(), after.Preview)
				return failed(err)
			}
			if err != nil {
				return invalidConfiguration(err)
			}
			fmt.Fprint(c.OutOrStdout(), text)
			return nil
		},
	}
	imp.Flags().BoolVar(&apply, "apply", false, "store the settings after the preview")
	c.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Validate the effective configuration",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := app.Validate(s.options()); err != nil {
				return invalidConfiguration(err)
			}
			fmt.Fprintln(c.OutOrStdout(), "configuration: OK")
			return nil
		},
	}, &cobra.Command{
		Use:   "export",
		Short: "Print the effective configuration as TOML, without secrets",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			text, err := app.Export(s.options())
			if err != nil {
				return invalidConfiguration(err)
			}
			fmt.Fprint(c.OutOrStdout(), text)
			return nil
		},
	}, imp)
	return c
}
