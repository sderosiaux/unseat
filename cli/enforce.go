package cli

import (
	"fmt"
	"strings"

	"github.com/sderosiaux/unseat/internal/enforce"
	"github.com/sderosiaux/unseat/internal/provider"
	"github.com/sderosiaux/unseat/internal/store"
	"github.com/spf13/cobra"
)

var enforceCmd = &cobra.Command{
	Use:   "enforce",
	Short: "Execute explicitly approved decisions inside a narrow provider/action scope",
}

var enforcePlanCmd = &cobra.Command{
	Use:   "plan",
	Short: "List approved decisions that Enforce can or cannot execute",
	RunE:  runEnforcePlan,
}

var enforceApplyCmd = &cobra.Command{
	Use:   "apply <decision-id>",
	Short: "Execute one approved decision after confirmation",
	Args:  cobra.ExactArgs(1),
	RunE:  runEnforceApply,
}

var enforceProvider string

func init() {
	enforcePlanCmd.Flags().StringVar(&enforceProvider, "provider", "", "Filter by provider")
	enforceApplyCmd.Flags().BoolVar(&autoConfirm, "yes", false, "Skip the confirmation prompt")
	enforceCmd.AddCommand(enforcePlanCmd, enforceApplyCmd)
	rootCmd.AddCommand(enforceCmd)
}

func runEnforcePlan(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	db, err := openStore()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	reg, _, err := provider.BuildRegistryWithIdentity(cfg, nil)
	if err != nil {
		return fmt.Errorf("initialize providers: %w", err)
	}
	filter := store.DecisionFilter{}
	if enforceProvider != "" {
		providerName := strings.ToLower(strings.TrimSpace(enforceProvider))
		filter.Provider = &providerName
	}
	candidates, err := enforce.New(db, reg).Plan(cmd.Context(), filter)
	if err != nil {
		return err
	}
	if jsonOutput {
		return printJSON(candidates)
	}
	printEnforcePlan(candidates)
	return nil
}

func runEnforceApply(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if cfg.Policies.DryRun {
		return fmt.Errorf("policies.dry_run is true in %s — remove it to allow `enforce apply`", configFile)
	}
	if !autoConfirm {
		ok, err := confirm("Execute this approved decision against the live provider?")
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("Aborted. Nothing was changed.")
			return nil
		}
	}

	db, err := openStore()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	reg, _, err := provider.BuildRegistryWithIdentity(cfg, nil)
	if err != nil {
		return fmt.Errorf("initialize providers: %w", err)
	}
	decision, err := enforce.New(db, reg).Apply(cmd.Context(), args[0], "cli")
	if err != nil {
		return err
	}
	if jsonOutput {
		return printJSON(decision)
	}
	fmt.Printf("Executed %s on %s.\n", decision.ID, decision.Provider)
	return nil
}

func printEnforcePlan(candidates []enforce.Candidate) {
	if len(candidates) == 0 {
		fmt.Println("No approved decisions are waiting for Enforce.")
		return
	}
	rows := make([][]string, 0, len(candidates))
	for _, c := range candidates {
		state := "blocked"
		if c.Executable {
			state = "executable"
		}
		rows = append(rows, []string{
			c.Decision.ID,
			c.Decision.Subject,
			c.Decision.Provider,
			string(c.Decision.ActionClass),
			state,
			strings.Join(c.BlockedBy, ","),
		})
	}
	printTable([]string{"ID", "SUBJECT", "PROVIDER", "ACTION", "ENFORCE", "BLOCKED BY"}, rows)
}
