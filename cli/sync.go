package cli

import (
	"fmt"

	"github.com/sderosiaux/saas-watcher/config"
	"github.com/sderosiaux/saas-watcher/internal/store"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Run reconciliation between desired and actual state",
}

var syncDryRunCmd = &cobra.Command{
	Use:   "dry-run",
	Short: "Preview sync actions without executing them",
	RunE:  runSyncDryRun,
}

var syncRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Execute one-shot reconciliation",
	RunE:  runSyncRun,
}

var autoConfirm bool

func init() {
	syncRunCmd.Flags().BoolVar(&autoConfirm, "yes", false, "Skip confirmation prompt")
	syncCmd.AddCommand(syncDryRunCmd, syncRunCmd)
	rootCmd.AddCommand(syncCmd)
}

func runSyncDryRun(cmd *cobra.Command, args []string) error {
	_, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	_, err = store.NewSQLite("saas-watcher.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	fmt.Println("Dry-run mode: no actions will be taken.")
	fmt.Println("(Full sync requires provider connections. Configure providers in saas-watcher.yaml)")
	return nil
}

func runSyncRun(cmd *cobra.Command, args []string) error {
	_, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	_, err = store.NewSQLite("saas-watcher.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	fmt.Println("Sync requires provider connections. Configure providers in saas-watcher.yaml")
	return nil
}
