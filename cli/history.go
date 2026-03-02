package cli

import (
	"context"
	"fmt"

	"github.com/sderosiaux/saas-watcher/internal/store"
	"github.com/spf13/cobra"
)

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "View event timeline and trends",
}

var historyEventsCmd = &cobra.Command{
	Use:   "events",
	Short: "List recent events",
	RunE:  runHistoryEvents,
}

var eventsLimit int

func init() {
	historyEventsCmd.Flags().IntVar(&eventsLimit, "limit", 50, "Max events to show")
	historyCmd.AddCommand(historyEventsCmd)
	rootCmd.AddCommand(historyCmd)
}

func runHistoryEvents(cmd *cobra.Command, args []string) error {
	db, err := store.NewSQLite("saas-watcher.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	events, err := db.ListEvents(context.Background(), store.EventFilter{Limit: eventsLimit})
	if err != nil {
		return err
	}

	if jsonOutput {
		return printJSON(events)
	}

	if len(events) == 0 {
		fmt.Println("No events recorded yet.")
		return nil
	}

	rows := make([][]string, len(events))
	for i, e := range events {
		rows[i] = []string{e.OccurredAt.Format("2006-01-02 15:04:05"), string(e.Type), e.Provider, e.Email, e.Trigger}
	}
	printTable([]string{"TIME", "TYPE", "PROVIDER", "EMAIL", "TRIGGER"}, rows)
	return nil
}
