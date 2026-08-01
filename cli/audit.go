package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider"
	"github.com/spf13/cobra"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Inspect current state — orphans, inactivity, drift",
}

var auditSeatsCmd = &cobra.Command{
	Use:   "seats",
	Short: "Classify every SaaS seat against the directory (managed, unmapped, orphan, external)",
	RunE:  runAuditSeats,
}

var auditOrphansCmd = &cobra.Command{
	Use:   "orphans",
	Short: "List seats with no active identity in Google Workspace",
	RunE:  runAuditOrphans,
}

var auditInactiveCmd = &cobra.Command{
	Use:   "inactive",
	Short: "List users with no activity in the last N days",
	RunE:  runAuditInactive,
}

var auditDriftCmd = &cobra.Command{
	Use:   "drift",
	Short: "Show the diff between desired state (groups) and actual state (SaaS)",
	RunE:  runAuditDrift,
}

var (
	inactiveDays  int
	auditProvider string
)

func init() {
	auditInactiveCmd.Flags().IntVar(&inactiveDays, "days", 30, "Inactivity threshold in days")
	auditCmd.PersistentFlags().StringVar(&auditProvider, "provider", "", "Restrict the audit to a single provider")
	auditCmd.AddCommand(auditSeatsCmd, auditOrphansCmd, auditDriftCmd, auditInactiveCmd)
	rootCmd.AddCommand(auditCmd)
}

// seatAudit holds the classified seats of every provider, plus the providers
// that could not be reached.
type seatAudit struct {
	Seats   []core.ClassifiedSeat
	Summary []core.ClassSummary
	Failed  map[string]error
}

// classifySeats fetches live provider state and the directory, then classifies
// every seat. It reads from the providers directly rather than the SQLite
// cache: an audit that silently reports stale data is worse than a slow one.
func classifySeats(ctx context.Context, cfg *config.Config) (*seatAudit, error) {
	if err := requireIdentitySource(cfg); err != nil {
		return nil, err
	}

	reg, identity, err := provider.BuildRegistry(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("initialize providers: %w", err)
	}

	directoryUsers, err := identity.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list directory users: %w", err)
	}
	directory := make(map[string]core.DirectoryUser, len(directoryUsers))
	for _, u := range directoryUsers {
		key := strings.ToLower(u.Email)
		directory[key] = core.DirectoryUser{Email: key, Suspended: u.Status == "suspended"}
	}

	// Desired membership per provider, and the alias index over all of it.
	desiredByProvider := make(map[string]map[string]bool)
	var allDesiredEmails []string
	groupCache := make(map[string][]core.User)

	for _, m := range cfg.Mappings {
		members, cached := groupCache[m.Group]
		if !cached {
			members, err = identity.ListGroupMembers(ctx, m.Group)
			if err != nil {
				return nil, fmt.Errorf("list members of %s: %w", m.Group, err)
			}
			groupCache[m.Group] = members
			for _, u := range members {
				allDesiredEmails = append(allDesiredEmails, u.Email)
			}
		}
		for _, pm := range m.Providers {
			if desiredByProvider[pm.Name] == nil {
				desiredByProvider[pm.Name] = make(map[string]bool)
			}
			for _, u := range members {
				desiredByProvider[pm.Name][strings.ToLower(u.Email)] = true
			}
		}
	}

	aliasIndex := core.BuildAliasIndex(cfg.Aliases, allDesiredEmails)

	audit := &seatAudit{Failed: make(map[string]error)}

	for _, name := range sortedProviderNames(cfg) {
		if auditProvider != "" && name != auditProvider {
			continue
		}
		p, err := reg.Get(name)
		if err != nil {
			audit.Failed[name] = err
			continue
		}

		users, err := p.ListUsers(ctx)
		if err != nil {
			audit.Failed[name] = err
			continue
		}

		exceptions := make(map[string]bool)
		for _, ex := range cfg.Policies.Exceptions {
			for _, prov := range ex.Providers {
				if prov == "*" || prov == name {
					exceptions[strings.ToLower(ex.Email)] = true
				}
			}
		}

		seats := core.ClassifySeats(core.ClassifyInput{
			ProviderName:  name,
			ActualUsers:   users,
			Directory:     directory,
			DesiredEmails: desiredByProvider[name],
			Domain:        cfg.IdentitySource.Domain,
			AliasIndex:    aliasIndex,
			Exceptions:    exceptions,
		})

		audit.Seats = append(audit.Seats, seats...)
		audit.Summary = append(audit.Summary, core.SummarizeSeats(name, seats))
	}

	return audit, nil
}

func sortedProviderNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func reportFailures(failed map[string]error) {
	if len(failed) == 0 {
		return
	}
	names := make([]string, 0, len(failed))
	for n := range failed {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Printf("\n%d provider(s) could not be read — their seats are NOT included above:\n", len(names))
	for _, n := range names {
		fmt.Printf("  %-16s %v\n", n, failed[n])
	}
}

func runAuditSeats(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	audit, err := classifySeats(cmd.Context(), cfg)
	if err != nil {
		return err
	}

	if jsonOutput {
		return printJSON(map[string]any{"summary": audit.Summary, "seats": audit.Seats})
	}

	if len(audit.Summary) == 0 {
		fmt.Println("No providers configured. Add credentials with `unseat providers add <name>`.")
		reportFailures(audit.Failed)
		return nil
	}

	rows := make([][]string, len(audit.Summary))
	for i, s := range audit.Summary {
		rows[i] = []string{
			s.Provider,
			fmt.Sprintf("%d", s.Total),
			fmt.Sprintf("%d", s.Managed),
			fmt.Sprintf("%d", s.Unmapped),
			fmt.Sprintf("%d", s.Orphan),
			fmt.Sprintf("%d", s.External),
			fmt.Sprintf("%d", s.Unresolved),
		}
	}
	printTable([]string{"PROVIDER", "SEATS", "MANAGED", "UNMAPPED", "ORPHAN", "EXTERNAL", "UNRESOLVED"}, rows)

	fmt.Println("\nmanaged    = active employee in a mapped group")
	fmt.Println("unmapped   = active employee, no mapped group grants this provider — fix your mappings, do not remove")
	fmt.Println("orphan     = no active directory identity — safe to reclaim")
	fmt.Println("external   = outside the corporate domain — needs a human decision")
	fmt.Println("unresolved = username with no email and no alias — add an alias to judge it")

	reportFailures(audit.Failed)
	return nil
}

func runAuditOrphans(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	audit, err := classifySeats(cmd.Context(), cfg)
	if err != nil {
		return err
	}

	var orphans []core.ClassifiedSeat
	for _, s := range audit.Seats {
		if s.Class == core.SeatOrphan {
			orphans = append(orphans, s)
		}
	}

	if jsonOutput {
		return printJSON(orphans)
	}

	if len(orphans) == 0 {
		fmt.Println("No orphaned seats: every seat maps to an active directory identity.")
		reportFailures(audit.Failed)
		return nil
	}

	rows := make([][]string, len(orphans))
	for i, o := range orphans {
		protected := ""
		if o.Protected {
			protected = "protected"
		}
		rows[i] = []string{o.Provider, o.Email, o.User.DisplayName, o.Reason, protected}
	}
	printTable([]string{"PROVIDER", "EMAIL", "NAME", "REASON", "POLICY"}, rows)
	fmt.Printf("\n%d orphaned seat(s). Preview the cleanup with `unseat sync plan`.\n", len(orphans))

	reportFailures(audit.Failed)
	return nil
}

func runAuditInactive(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	reporting, err := provider.ActivityReportingProviders(cfg)
	if err != nil {
		return err
	}
	silent, err := provider.NonActivityReportingProviders(cfg)
	if err != nil {
		return err
	}

	if auditProvider != "" {
		reporting = filterTo(reporting, auditProvider)
		silent = filterTo(silent, auditProvider)
	}

	db, err := openStore()
	if err != nil {
		return err
	}
	defer db.Close()

	since := time.Now().AddDate(0, 0, -inactiveDays)
	users, err := db.GetInactiveUsers(cmd.Context(), since, reporting)
	if err != nil {
		return err
	}

	if jsonOutput {
		return printJSON(map[string]any{
			"threshold_days":      inactiveDays,
			"evaluated_providers": reporting,
			"unevaluable":         silent,
			"users":               users,
		})
	}

	if len(reporting) == 0 {
		fmt.Println("None of your configured providers exposes activity data — inactivity cannot be assessed.")
		reportUnevaluable(silent)
		return nil
	}

	if len(users) == 0 {
		fmt.Printf("No user inactive for %d+ days across: %s\n", inactiveDays, strings.Join(reporting, ", "))
		reportUnevaluable(silent)
		return nil
	}

	rows := make([][]string, len(users))
	for i, u := range users {
		lastActive := "never seen"
		if u.LastActivityAt != nil {
			lastActive = u.LastActivityAt.Format("2006-01-02")
		}
		rows[i] = []string{u.Provider, u.Email, u.DisplayName, lastActive, u.Status}
	}
	printTable([]string{"PROVIDER", "EMAIL", "NAME", "LAST ACTIVE", "STATUS"}, rows)
	fmt.Printf("\n%d user(s) inactive for %d+ days across: %s\n", len(users), inactiveDays, strings.Join(reporting, ", "))

	reportUnevaluable(silent)
	return nil
}

func reportUnevaluable(silent []string) {
	if len(silent) == 0 {
		return
	}
	fmt.Printf("\nNot evaluated (%d provider(s) expose no activity API): %s\n", len(silent), strings.Join(silent, ", "))
	fmt.Println("Their seats are absent from this list because the data does not exist, not because they are active.")
}

func filterTo(names []string, want string) []string {
	for _, n := range names {
		if n == want {
			return []string{n}
		}
	}
	return nil
}

func runAuditDrift(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cfg.Policies.DryRun = true
	return runSync(cmd.Context(), cfg)
}
