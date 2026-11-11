package cmd

import (
	"github.com/spf13/cobra"

	"github.com/jniltinho/go-microsip/internal/mcp"
)

func mcpCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP server for AI agents on standard input and output",
		Long: `Runs a Model Context Protocol server on standard input and output, for AI agents such as
Claude Code or Codex. Register it once, for example:

  claude mcp add go-microsip -- go-microsip mcp
  codex mcp add go-microsip -- go-microsip mcp

It opens no network port and needs no display.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := mcp.Serve(c.Context()); err != nil {
				return failed(err)
			}
			return nil
		},
	}
}
