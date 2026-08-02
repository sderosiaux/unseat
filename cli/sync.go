package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider"
	syncer "github.com/sderosiaux/unseat/internal/sync"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Reconcile desired state (groups) with actual state (SaaS)",
}

// The verb decides whether anything is mutated, not a config flag. The old
// `run` command silently did nothing when policies.dry_run was left at its
// default, which is the wrong failure mode for a destructive operation.
var syncPlanCmd = &cobra.Command{
	Use:   "plan",
	Short: "Show what reconciliation would change — never mutates anything",
	RunE:  runSyncPlan,
}

var syncApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Execute reconciliation after showing the plan and confirming",
	RunE:  runSyncApply,
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
	syncApplyCmd.Flags().BoolVar(&autoConfirm, "yes", false, "Skip the confirmation prompt")
	syncWatchCmd.Flags().DurationVar(&watchInterval, "interval", 0, "Override sync interval (e.g. 5m, 1h)")
	syncCmd.AddCommand(syncPlanCmd, syncApplyCmd, syncWatchCmd)
	rootCmd.AddCommand(syncCmd)
}

func runSyncPlan(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cfg.Policies.DryRun = true
	return runSync(cmd.Context(), cfg)
}

func runSyncApply(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	// policies.dry_run is a safety lock, not a mode selector: it can forbid
	// apply, never silently downgrade it into a no-op.
	if cfg.Policies.DryRun {
		return fmt.Errorf("policies.dry_run is true in %s — remove it to allow `sync apply`", configFile)
	}

	preview := *cfg
	preview.Policies.DryRun = true
	plans, err := computePlans(cmd.Context(), &preview)
	if err != nil {
		return err
	}

	printPlans(plans)

	if totalActions(plans) == 0 {
		fmt.Println("\nNothing to apply.")
		return nil
	}

	if !autoConfirm {
		ok, err := confirm(fmt.Sprintf("\nApply these %d action(s) to your live SaaS providers?", totalActions(plans)))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("Aborted. Nothing was changed.")
			return nil
		}
	}

	return runSync(cmd.Context(), cfg)
}

func runSyncWatch(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	// Resolve interval: flag > config > default 5m.
	interval := 5 * time.Minute
	if cfg.Policies.SyncInterval > 0 {
		interval = cfg.Policies.SyncInterval
	}
	if watchInterval > 0 {
		interval = watchInterval
	}

	db, err := openStore()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	reg, identity, err := provider.BuildRegistry(ctx, cfg)
	if err != nil {
		return fmt.Errorf("initialize providers: %w", err)
	}

	// watch is the only unattended path that can write, so it states which mode
	// it is in on every start. Announcing only the harmless case meant the
	// dangerous one was the silent one.
	if cfg.Policies.DryRun {
		fmt.Printf("Dry-run (policies.dry_run: true) — planning every %s, applying nothing.\n", interval)
	} else {
		fmt.Printf("LIVE MODE (policies.dry_run: false) — this daemon will add and remove real SaaS seats every %s.\n", interval)
		fmt.Println("Departed identities will be deprovisioned without further confirmation.")
		if cfg.Policies.GracePeriod > 0 {
			fmt.Printf("Grace period: %s from first detection.\n", cfg.Policies.GracePeriod)
		} else {
			fmt.Println("No grace period configured: removals take effect on the first detection.")
		}
	}

	rec := syncer.NewReconciler(db, cfg, reg, identity)
	scheduler := syncer.NewScheduler(rec, interval)

	return scheduler.Start(ctx)
}

func computePlans(ctx context.Context, cfg *config.Config) ([]*core.ReconcilePlan, error) {
	if err := requireIdentitySource(cfg); err != nil {
		return nil, err
	}

	db, err := openStore()
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	reg, identity, err := provider.BuildRegistry(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("initialize providers: %w\nCheck your identity source credentials and provider API keys", err)
	}

	rec := syncer.NewReconciler(db, cfg, reg, identity)
	plans, err := rec.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("reconciliation: %w", err)
	}
	return plans, nil
}

func runSync(ctx context.Context, cfg *config.Config) error {
	plans, err := computePlans(ctx, cfg)
	if err != nil {
		return err
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(plans)
	}

	printPlans(plans)
	return nil
}

func printPlans(plans []*core.ReconcilePlan) {
	if len(plans) == 0 {
		fmt.Println("No providers found in mappings. Check the `mappings` section of your config.")
		return
	}

	for _, plan := range plans {
		mode := ""
		if plan.DryRun {
			mode = " [PLAN — nothing applied]"
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
		if len(plan.ToReview) > 0 {
			fmt.Printf("  To review (%d) — reported, never removed automatically:\n", len(plan.ToReview))
			for _, r := range plan.ToReview {
				fmt.Printf("    ? %s [%s] %s\n", r.Email, r.Class, r.Reason)
			}
		}
		if len(plan.AlreadyDeactivated) > 0 {
			fmt.Printf("  Already deactivated (%d) — no action left, but usually still billed:\n", len(plan.AlreadyDeactivated))
			for _, a := range plan.AlreadyDeactivated {
				fmt.Printf("    · %s\n", a.Email)
			}
		}
		if len(plan.ToAdd) == 0 && len(plan.ToRemove) == 0 {
			fmt.Println("  Nothing to apply.")
		}
	}
}

func totalActions(plans []*core.ReconcilePlan) int {
	n := 0
	for _, p := range plans {
		n += len(p.ToAdd) + len(p.ToRemove)
	}
	return n
}

func confirm(prompt string) (bool, error) {
	fmt.Printf("%s [y/N]: ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false, nil
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}
