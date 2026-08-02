package cli

import (
	"fmt"
	"net/http"

	"github.com/sderosiaux/unseat/api"
	"github.com/spf13/cobra"
)

var (
	servePort int
	serveHost string
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the web API server and dashboard",
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", 8080, "Port to listen on")
	// The API and dashboard expose every employee's email with no
	// authentication, so they bind to loopback only. Widening the bind address
	// has to be a deliberate act.
	serveCmd.Flags().StringVar(&serveHost, "host", "127.0.0.1", "Address to bind (the API has no authentication)")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	db, err := openStore()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	srv := api.NewServer(db, cfg)

	addr := fmt.Sprintf("%s:%d", serveHost, servePort)
	if serveHost != "127.0.0.1" && serveHost != "localhost" {
		fmt.Printf("WARNING: binding to %s — the API has no authentication and serves every user's email.\n", serveHost)
	}
	fmt.Printf("unseat dashboard on http://%s\n", addr)
	return http.ListenAndServe(addr, srv.Handler())
}
