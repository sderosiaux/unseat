package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/provider"
	"github.com/sderosiaux/unseat/internal/store"
	syncer "github.com/sderosiaux/unseat/internal/sync"
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

var syncWatchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Run continuous reconciliation (daemon mode)",
	RunE:  runSyncWatch,
}

var (
	autoConfirm   bool
	watchInterval time.Duration
)

func init() {
	syncRunCmd.Flags().BoolVar(&autoConfirm, "yes", false, "Skip confirmation prompt")
	syncWatchCmd.Flags().DurationVar(&watchInterval, "interval", 0, "Override sync interval (e.g. 5m, 1h)")
	syncCmd.AddCommand(syncDryRunCmd, syncRunCmd, syncWatchCmd)
	rootCmd.AddCommand(syncCmd)
}

func runSyncDryRun(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg.Policies.DryRun = true
	return runSync(cmd.Context(), cfg)
}

func runSyncRun(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if !autoConfirm && !cfg.Policies.DryRun {
		fmt.Println("This will execute real add/remove actions on your SaaS providers.")
		fmt.Println("Use --yes to skip this prompt, or run 'sync dry-run' to preview.")
		return nil
	}

	return runSync(cmd.Context(), cfg)
}

func runSyncWatch(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Resolve interval: flag > config > default 5m.
	interval := 5 * time.Minute
	if cfg.Policies.SyncInterval > 0 {
		interval = cfg.Policies.SyncInterval
	}
	if watchInterval > 0 {
		interval = watchInterval
	}

	db, err := store.NewSQLite("unseat.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	reg, identity, err := provider.BuildRegistry(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Provider initialization failed: %v\n", err)
		return err
	}

	rec := syncer.NewReconciler(db, cfg, reg, identity)
	scheduler := syncer.NewScheduler(rec, interval)

	return scheduler.Start(ctx)
}

func runSync(ctx context.Context, cfg *config.Config) error {
	db, err := store.NewSQLite("unseat.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	reg, identity, err := provider.BuildRegistry(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Provider initialization failed: %v\n", err)
		fmt.Fprintln(os.Stderr, "Ensure your identity source credentials and provider API keys are configured.")
		return err
	}

	rec := syncer.NewReconciler(db, cfg, reg, identity)
	plans, err := rec.Run(ctx)
	if err != nil {
		return fmt.Errorf("reconciliation: %w", err)
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(plans)
	}

	for _, plan := range plans {
		mode := ""
		if plan.DryRun {
			mode = " [DRY-RUN]"
		}
		fmt.Printf("\n=== %s%s ===\n", plan.ProviderName, mode)
		fmt.Printf("  Unchanged: %d\n", plan.Unchanged)

		if len(plan.ToAdd) > 0 {
			fmt.Printf("  To add (%d):\n", len(plan.ToAdd))
			for _, a := range plan.ToAdd {
				fmt.Printf("    + %s (role: %s)\n", a.Email, a.Role)
			}
		}
		if len(plan.ToRemove) > 0 {
			fmt.Printf("  To remove (%d):\n", len(plan.ToRemove))
			for _, r := range plan.ToRemove {
				fmt.Printf("    - %s\n", r.Email)
			}
		}
		if len(plan.ToAdd) == 0 && len(plan.ToRemove) == 0 {
			fmt.Println("  Everything in sync.")
		}
	}

	if len(plans) == 0 {
		fmt.Println("No providers found in mappings. Check your unseat.yaml configuration.")
	}

	return nil
}
