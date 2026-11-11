package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jniltinho/go-microsip/internal/core/app"
)

// importCommand brings the Rust application's data over. It exits 0 when done and 1 when the import
// cannot run or was undone; nothing is imported without --apply.
func (s *session) importCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "import",
		Short: "Import settings and data from another application",
		Args:  cobra.NoArgs,
		RunE:  func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	var apply, credential bool
	rust := &cobra.Command{
		Use:   "rust-microsip",
		Short: "Preview what rust-microsip holds; with --apply, bring it into go-microsip",
		Long: `Prints what the Rust application (rust-microsip) holds: its account without the password and
how many contacts and calls. With --apply its configuration file and its database replace
go-microsip's, which are kept beside them as backups. The rust-microsip files are only read, and
rust-microsip and go-microsip must not be running. An interrupted import is undone at the next
start. Nothing is ever imported without this command.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			text, err := app.ImportRust(s.options(), apply, credential)
			var after *app.ImportError
			if errors.As(err, &after) {
				fmt.Fprint(c.OutOrStdout(), after.Preview)
			}
			if err != nil {
				return failed(err)
			}
			fmt.Fprint(c.OutOrStdout(), text)
			return nil
		},
	}
	rust.Flags().BoolVar(&apply, "apply", false, "import after the preview")
	rust.Flags().BoolVar(&credential, "credential", false, "with --apply: also copy the password from the rust-microsip keyring")
	c.AddCommand(rust)
	return c
}
