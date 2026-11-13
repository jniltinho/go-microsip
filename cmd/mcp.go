package cmd

import (
	"github.com/spf13/cobra"

	"github.com/jniltinho/go-microsip/internal/mcp"
)

func (s *session) mcpCommand() *cobra.Command {
	var grantFile string
	c := &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP server for AI agents on standard input and output",
		Long: `Runs a Model Context Protocol server on standard input and output, for AI agents such as
Claude Code or Codex. Register it once, for example:

  claude mcp add go-microsip -- go-microsip mcp
  codex mcp add go-microsip -- go-microsip mcp

Its tools talk to the go-microsip that is running and follow the rules of the command line: a
change waits for Allow in the window, personal data needs a grant, and no tool approves anything.
It never starts a softphone, opens no network port and needs no display.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			capability, err := s.capability(grantFile)
			if err != nil {
				return err
			}
			if err := mcp.Serve(c.Context(), s.reach, capability); err != nil {
				return failed(err)
			}
			return nil
		},
	}
	c.Flags().StringVar(&grantFile, "grant-file", "", "present the grant in this private `file` with every request")
	return c
}
