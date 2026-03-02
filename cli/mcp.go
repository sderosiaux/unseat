package cli

import (
	"context"
	"fmt"

	mcpserver "github.com/sderosiaux/unseat/api/mcp"
	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/store"
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
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := store.NewSQLite("unseat.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	srv := mcpserver.New(db, cfg)
	return srv.Run(context.Background())
}
