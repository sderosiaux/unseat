package cli

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/sderosiaux/unseat/internal/auth"
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
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	db, err := openStore()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	states, err := db.ListSyncStates(cmd.Context())
	if err != nil {
		return err
	}
	syncState := make(map[string]store.SyncState, len(states))
	for _, s := range states {
		syncState[s.Provider] = s
	}

	activity, err := provider.ActivityReportingProviders(cfg)
	if err != nil {
		return err
	}

	// List every configured provider, not just those already synced: a
	// provider with credentials but no sync yet is the normal starting state,
	// and hiding it made the tool look empty on first run.
	type row struct {
		Provider     string `json:"provider"`
		Credential   string `json:"credential"`
		Mapped       bool   `json:"mapped"`
		ReportsUsage bool   `json:"reports_usage"`
		LastSynced   string `json:"last_synced,omitempty"`
		Users        int    `json:"users"`
	}

	names := sortedProviderNames(cfg)
	out := make([]row, 0, len(names))
	for _, name := range names {
		r := row{
			Provider:     name,
			Credential:   "missing",
			Mapped:       len(cfg.GroupsForProvider(name)) > 0,
			ReportsUsage: slices.Contains(activity, name),
			LastSynced:   "never",
		}
		if cfg.Providers[name].APIKey != "" {
			r.Credential = "ok"
		}
		if s, ok := syncState[name]; ok {
			r.LastSynced = s.LastSyncedAt.Format("2006-01-02 15:04")
			r.Users = s.UserCount
		}
		out = append(out, r)
	}

	if jsonOutput {
		return printJSON(out)
	}

	if len(out) == 0 {
		fmt.Println("No providers configured. Add one with `unseat providers add <name>`,")
		fmt.Println("or list what is available with `unseat providers supported`.")
		return nil
	}

	rows := make([][]string, len(out))
	for i, r := range out {
		rows[i] = []string{
			r.Provider,
			r.Credential,
			yesNo(r.Mapped),
			yesNo(r.ReportsUsage),
			r.LastSynced,
			fmt.Sprintf("%d", r.Users),
		}
	}
	printTable([]string{"PROVIDER", "CREDENTIAL", "MAPPED", "USAGE DATA", "LAST SYNCED", "USERS"}, rows)
	return nil
}

// missingCredentialHelp explains why a provider is unusable and what to do,
// distinguishing a typo from a provider that simply has no credential yet.
func missingCredentialHelp(name string) string {
	known, ok := auth.KnownProviders[name]
	if !ok {
		return fmt.Sprintf("unknown provider %q — run `unseat providers supported` to list valid names", name)
	}

	envHint := strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(name)) + "_API_KEY"
	how := fmt.Sprintf("no credential — run `unseat providers add %s`, "+
		"or set providers.%s.api_key in %s (e.g. \"${%s}\" with %s exported or in .env)",
		name, name, configFile, envHint, envHint)

	if known.Instructions != "" {
		how += "\n" + strings.Repeat(" ", 24) + known.Instructions
	}
	return how
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func runProvidersUsers(cmd *cobra.Command, args []string) error {
	name := args[0]

	db, err := openStore()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	users, err := db.GetProviderUsers(cmd.Context(), name)
	if err != nil {
		return err
	}

	if jsonOutput {
		return printJSON(users)
	}

	if len(users) == 0 {
		fmt.Printf("No users cached for %s. Run `unseat sync plan` to populate the cache.\n", name)
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
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	// The identity source is a provider too, and the one most likely to be
	// misconfigured — domain-wide delegation fails in ways that only show up on
	// a real call. Skipping it made the single most fragile connection the one
	// thing `providers test` could not check.
	//
	// Its initialisation is allowed to fail: a broken directory must not stop
	// the other providers from being tested. The error is kept and reported
	// against the provider it belongs to.
	ctx := cmd.Context()

	reg, _, err := provider.BuildRegistry(ctx, cfg)
	var identityErr error
	if err != nil {
		identityErr = err
		reg, _, err = provider.BuildRegistryWithIdentity(cfg, nil)
		if err != nil {
			return err
		}
	}

	var results []map[string]any

	for _, name := range args {
		p, err := reg.Get(name)
		if err != nil {
			// "not registered" is true but useless: a provider is absent from
			// the registry precisely when it has no credential, which is the
			// normal state on first run. Say what to do instead.
			diag := missingCredentialHelp(name)
			// Unless it is the identity source, which failed to initialise for
			// a reason we captured — that is far more useful than "no credential".
			if identityErr != nil && name == cfg.IdentitySource.Provider {
				diag = identityErr.Error()
			}
			if jsonOutput {
				results = append(results, map[string]any{"provider": name, "status": "not_configured", "error": diag})
			} else {
				fmt.Printf("%-15s  SKIP   %s\n", name, diag)
			}
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
