package cli

import (
	"context"
	"fmt"

	"github.com/sderosiaux/saas-watcher/internal/store"
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

func init() {
	providersCmd.AddCommand(providersListCmd, providersUsersCmd)
	rootCmd.AddCommand(providersCmd)
}

func runProvidersList(cmd *cobra.Command, args []string) error {
	db, err := store.NewSQLite("saas-watcher.db")
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
		fmt.Println("No providers synced yet. Run `saas-watcher sync run` first.")
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

	db, err := store.NewSQLite("saas-watcher.db")
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
		fmt.Printf("No users cached for %s. Run `saas-watcher sync run` first.\n", provider)
		return nil
	}

	rows := make([][]string, len(users))
	for i, u := range users {
		rows[i] = []string{u.Email, u.DisplayName, u.Role, u.Status}
	}
	printTable([]string{"EMAIL", "NAME", "ROLE", "STATUS"}, rows)
	return nil
}
