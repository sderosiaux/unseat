package cli

import (
	"fmt"
	"net/http"

	"github.com/sderosiaux/saas-watcher/api"
	"github.com/sderosiaux/saas-watcher/config"
	"github.com/sderosiaux/saas-watcher/internal/store"
	"github.com/spf13/cobra"
)

var servePort int

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the web API server",
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", 8080, "Port to listen on")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := store.NewSQLite("saas-watcher.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	srv := api.NewServer(db, cfg)

	addr := fmt.Sprintf(":%d", servePort)
	fmt.Printf("Starting saas-watcher API on %s\n", addr)
	return http.ListenAndServe(addr, srv.Handler())
}
