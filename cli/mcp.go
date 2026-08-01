package cli

import (
	mcpserver "github.com/sderosiaux/unseat/api/mcp"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start MCP server (stdio transport) for LLM agent integration",
	RunE:  runMCP,
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}

func runMCP(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	db, err := openStore()
	if err != nil {
		return err
	}
	defer db.Close()

	srv := mcpserver.New(db, cfg)
	return srv.Run(cmd.Context())
}
