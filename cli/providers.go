package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/provider"
	"github.com/sderosiaux/unseat/internal/store"
	"github.com/spf13/cobra"
)

var providersCmd = &cobra.Command{
	Use:   "providers",
	Short: "Manage and inspect SaaS provider connections",
}

var providersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured providers and their sync status",
	RunE:  runProvidersList,
}

var providersUsersCmd = &cobra.Command{
	Use:   "users [provider]",
	Short: "List cached users for a specific provider",
	Args:  cobra.ExactArgs(1),
	RunE:  runProvidersUsers,
}

var providersTestCmd = &cobra.Command{
	Use:   "test [provider...]",
	Short: "Test connectivity by calling ListUsers on each provider",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runProvidersTest,
}

func init() {
	providersCmd.AddCommand(providersListCmd, providersUsersCmd, providersTestCmd)
	rootCmd.AddCommand(providersCmd)
}

func runProvidersList(cmd *cobra.Command, args []string) error {
	db, err := store.NewSQLite("unseat.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	states, err := db.ListSyncStates(context.Background())
	if err != nil {
		return err
	}

	if jsonOutput {
		return printJSON(states)
	}

	if len(states) == 0 {
		fmt.Println("No providers synced yet. Run `unseat sync run` first.")
		return nil
	}

	rows := make([][]string, len(states))
	for i, s := range states {
		rows[i] = []string{s.Provider, s.LastSyncedAt.Format("2006-01-02 15:04:05"), fmt.Sprintf("%d", s.UserCount), s.Status}
	}
	printTable([]string{"PROVIDER", "LAST SYNCED", "USERS", "STATUS"}, rows)
	return nil
}

func runProvidersUsers(cmd *cobra.Command, args []string) error {
	provider := args[0]

	db, err := store.NewSQLite("unseat.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	users, err := db.GetProviderUsers(context.Background(), provider)
	if err != nil {
		return err
	}

	if jsonOutput {
		return printJSON(users)
	}

	if len(users) == 0 {
		fmt.Printf("No users cached for %s. Run `unseat sync run` first.\n", provider)
		return nil
	}

	rows := make([][]string, len(users))
	for i, u := range users {
		rows[i] = []string{u.Email, u.DisplayName, u.Role, u.Status}
	}
	printTable([]string{"EMAIL", "NAME", "ROLE", "STATUS"}, rows)
	return nil
}

func runProvidersTest(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return err
	}

	// Build a registry with only the requested providers (skip identity source).
	reg, _, err := provider.BuildRegistryWithIdentity(cfg, nil)
	if err != nil {
		return err
	}

	ctx := context.Background()
	var results []map[string]any

	for _, name := range args {
		p, err := reg.Get(name)
		if err != nil {
			fmt.Printf("%-15s  ERROR  %s\n", name, err)
			continue
		}

		start := time.Now()
		users, err := p.ListUsers(ctx)
		elapsed := time.Since(start)

		if err != nil {
			if jsonOutput {
				results = append(results, map[string]any{"provider": name, "status": "error", "error": err.Error()})
			} else {
				fmt.Printf("%-15s  ERROR  %s (%s)\n", name, err, elapsed.Round(time.Millisecond))
			}
			continue
		}

		if jsonOutput {
			results = append(results, map[string]any{"provider": name, "status": "ok", "users": len(users), "elapsed_ms": elapsed.Milliseconds()})
		} else {
			fmt.Printf("%-15s  OK     %d users (%s)\n", name, len(users), elapsed.Round(time.Millisecond))
		}
	}

	if jsonOutput {
		return printJSON(results)
	}
	return nil
}
