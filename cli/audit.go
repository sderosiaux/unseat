package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/store"
	"github.com/spf13/cobra"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Inspect current state — orphans, costs, drift",
}

var auditOrphansCmd = &cobra.Command{
	Use:   "orphans",
	Short: "List seats with no matching Google Workspace user",
	RunE:  runAuditOrphans,
}

var inactiveDays int

var auditInactiveCmd = &cobra.Command{
	Use:   "inactive",
	Short: "List users with no activity in the last N days",
	RunE:  runAuditInactive,
}

var auditDriftCmd = &cobra.Command{
	Use:   "drift",
	Short: "Show diff between desired state (groups) and actual state (SaaS)",
	RunE:  runAuditDrift,
}

func init() {
	auditInactiveCmd.Flags().IntVar(&inactiveDays, "days", 30, "Inactivity threshold in days")
	auditCmd.AddCommand(auditOrphansCmd, auditDriftCmd, auditInactiveCmd)
	rootCmd.AddCommand(auditCmd)
}

func runAuditOrphans(cmd *cobra.Command, args []string) error {
	_, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := store.NewSQLite("unseat.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	ctx := context.Background()
	states, err := db.ListSyncStates(ctx)
	if err != nil {
		return err
	}

	type orphan struct {
		Provider  string `json:"provider"`
		Email     string `json:"email"`
		ExpiresAt string `json:"expires_at"`
	}
	var orphans []orphan

	for _, ss := range states {
		removals, err := db.GetPendingRemovals(ctx, ss.Provider)
		if err != nil {
			return err
		}
		for _, r := range removals {
			orphans = append(orphans, orphan{
				Provider:  r.Provider,
				Email:     r.Email,
				ExpiresAt: r.ExpiresAt.Format("2006-01-02 15:04:05"),
			})
		}
	}

	if jsonOutput {
		return printJSON(orphans)
	}

	if len(orphans) == 0 {
		fmt.Println("No orphans detected. Run `unseat sync run` first to populate cache.")
		return nil
	}

	rows := make([][]string, len(orphans))
	for i, o := range orphans {
		rows[i] = []string{o.Provider, o.Email, o.ExpiresAt}
	}
	printTable([]string{"PROVIDER", "EMAIL", "EXPIRES AT"}, rows)
	return nil
}

func runAuditInactive(cmd *cobra.Command, args []string) error {
	_, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := store.NewSQLite("unseat.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	ctx := context.Background()
	since := time.Now().AddDate(0, 0, -inactiveDays)
	users, err := db.GetInactiveUsers(ctx, since)
	if err != nil {
		return err
	}

	if jsonOutput {
		return printJSON(users)
	}

	if len(users) == 0 {
		fmt.Printf("No inactive users (threshold: %d days). Run `unseat sync run` first to populate cache.\n", inactiveDays)
		return nil
	}

	rows := make([][]string, len(users))
	for i, u := range users {
		lastActive := "never"
		if u.LastActivityAt != nil {
			lastActive = u.LastActivityAt.Format("2006-01-02")
		}
		rows[i] = []string{u.Provider, u.Email, u.DisplayName, lastActive, u.Status}
	}
	printTable([]string{"PROVIDER", "EMAIL", "NAME", "LAST ACTIVE", "STATUS"}, rows)
	return nil
}

func runAuditDrift(cmd *cobra.Command, args []string) error {
	fmt.Println("Drift detection requires a sync. Use `unseat sync dry-run` to preview actions.")
	return nil
}
