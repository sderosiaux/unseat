package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider"
	"github.com/spf13/cobra"
)

var auditCredentialsCmd = &cobra.Command{
	Use:   "credentials",
	Short: "Classify the non-human access each provider has granted (apps, integrations, webhooks)",
	Long: `Seats are attached to people; these are not.

An app installed by an engineer who has since left keeps its permissions. An
integration authorised under someone's OAuth grant keeps running under it.
Suspending their directory account touches neither, and no seat report will
ever mention them — which is precisely why they accumulate.

Read-only. It lists and classifies; it never revokes.`,
	RunE: runAuditCredentials,
}

func init() {
	auditCmd.AddCommand(auditCredentialsCmd)
}

// credentialAudit is every provider's credentials, classified, plus the ones
// that could not be read.
type credentialAudit struct {
	Credentials []core.ClassifiedCredential `json:"credentials"`
	Summary     []core.CredentialSummary    `json:"summary"`
	Failed      map[string]string           `json:"failed,omitempty"`
	Warnings    map[string]string           `json:"warnings,omitempty"`
	// Skipped names the configured providers whose API exposes no credential
	// listing at all. Leaving them out silently would let an empty section read
	// as "nothing to find" when it means "never looked".
	Skipped []string `json:"not_supported,omitempty"`
	cache   []credentialCacheEntry
}

func classifyCredentials(ctx context.Context, cfg *config.Config) (*credentialAudit, error) {
	var (
		reg      *provider.Registry
		identity provider.IdentityProvider
		err      error
	)
	if cfg.IdentitySource.Provider == "" {
		reg, identity, err = provider.BuildRegistryWithIdentity(cfg, nil)
	} else {
		reg, identity, err = provider.BuildRegistry(ctx, cfg)
	}
	if err != nil {
		return nil, fmt.Errorf("initialize providers: %w", err)
	}

	// The directory is what turns a list of integrations into a verdict. Without
	// it nothing is orphaned — the audit degrades to an inventory rather than
	// accusing an org of access it cannot actually prove is stale.
	var directory map[string]core.DirectoryUser
	var aliasIndex map[string]string

	if identity != nil {
		directoryUsers, err := identity.ListUsers(ctx)
		if err != nil {
			return nil, fmt.Errorf("list directory users: %w", err)
		}
		directory = make(map[string]core.DirectoryUser, len(directoryUsers))
		knownEmails := make([]string, 0, len(directoryUsers))
		for _, u := range directoryUsers {
			key := strings.ToLower(u.Email)
			directory[key] = core.DirectoryUser{Email: key, Suspended: u.Status == core.StatusSuspended}
			knownEmails = append(knownEmails, key)
		}
		aliasIndex = core.BuildAliasIndex(cfg.Aliases, knownEmails)
	}

	audit := &credentialAudit{Failed: make(map[string]string), Warnings: make(map[string]string)}

	for _, name := range sortedProviderNames(cfg) {
		if auditProvider != "" && name != auditProvider {
			continue
		}
		p, err := reg.Get(name)
		if err != nil {
			audit.Failed[name] = err.Error()
			continue
		}

		entry, classified := inspectProviderCredentials(ctx, name, p, directory, cfg.CorporateDomain(), aliasIndex)
		audit.cache = append(audit.cache, entry)

		switch entry.Status {
		case credentialSyncNotSupported:
			audit.Skipped = append(audit.Skipped, name)
			continue
		case credentialSyncFailed:
			audit.Failed[name] = entry.Message
			continue
		}
		if entry.Message != "" {
			audit.Warnings[name] = entry.Message
		}

		audit.Credentials = append(audit.Credentials, classified...)
		audit.Summary = append(audit.Summary,
			core.SummarizeCredentials(name, classified, p.Capabilities().ReportsCredentialUsage))
	}

	sort.Strings(audit.Skipped)
	return audit, nil
}

func inspectProviderCredentials(
	ctx context.Context,
	name string,
	p provider.Provider,
	directory map[string]core.DirectoryUser,
	domain string,
	aliasIndex map[string]string,
) (credentialCacheEntry, []core.ClassifiedCredential) {
	entry := credentialCacheEntry{Provider: name}
	cp, ok := p.(provider.CredentialProvider)
	if !ok {
		entry.Status = credentialSyncNotSupported
		entry.Message = "provider API exposes no credential listing"
		return entry, nil
	}

	var (
		creds    []core.Credential
		warnings []string
		err      error
	)
	if ip, ok := p.(provider.CredentialInventoryProvider); ok {
		var inventory core.CredentialInventory
		inventory, err = ip.ListCredentialInventory(ctx)
		creds = inventory.Credentials
		warnings = inventory.Warnings
	} else {
		creds, err = cp.ListCredentials(ctx)
	}
	if err != nil {
		entry.Status = credentialSyncFailed
		entry.Message = err.Error()
		return entry, nil
	}

	classified := core.ClassifyCredentials(core.ClassifyCredentialsInput{
		Credentials: creds,
		Directory:   directory,
		Domain:      domain,
		AliasIndex:  aliasIndex,
	})
	entry.Status = credentialSyncOK
	entry.Credentials = classified
	entry.UsageKnown = p.Capabilities().ReportsCredentialUsage
	if len(warnings) > 0 {
		entry.Message = strings.Join(warnings, "; ")
	}
	return entry, classified
}

func runAuditCredentials(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	audit, err := classifyCredentials(cmd.Context(), cfg)
	if err != nil {
		return err
	}

	cacheCredentialSnapshots(cmd.Context(), audit.cache)

	if jsonOutput {
		return printJSON(audit)
	}

	printCredentialAudit(audit, cfg)
	return nil
}

func printCredentialAudit(audit *credentialAudit, cfg *config.Config) {
	if len(audit.Summary) == 0 && len(audit.Failed) == 0 {
		fmt.Println("No configured provider exposes its non-human access.")
		return
	}

	fmt.Println("NON-HUMAN ACCESS")
	fmt.Println(strings.Repeat("─", 72))
	fmt.Printf("%-18s %8s %9s %8s %8s %6s\n", "PROVIDER", "ORPHANED", "DORMANT", "UNOWNED", "LIVE", "TOTAL")
	for _, s := range audit.Summary {
		fmt.Printf("%-18s %8d %9d %8d %8d %6d\n", s.Provider, s.Orphaned, s.Dormant, s.Unowned, s.Live, s.Total)
	}

	printCredentialGroup(audit.Credentials, core.CredentialOrphaned,
		"AUTHORISED BY SOMEONE WHO IS GONE",
		"This access outlived its owner. Offboarding did not touch it and could not.")

	printCredentialGroup(audit.Credentials, core.CredentialDormant,
		"INSTALLED BUT SWITCHED OFF",
		"Inert today, one click from live. Uninstalling is free.")

	if over := filterOverreaching(audit.Credentials); len(over) > 0 {
		fmt.Printf("\nWRITE ACCESS TO EVERYTHING (%d)\n", len(over))
		fmt.Println(strings.Repeat("─", 72))
		for _, c := range over {
			// A suspended app cannot act, so listing it here unmarked would
			// inflate the alarm. It stays in the list because its permissions
			// survive the suspension and any admin can lift it.
			state := ""
			if c.Class == core.CredentialDormant {
				state = " (switched off)"
			}
			fmt.Printf("  %-14s %-24s %s%s\n", c.Credential.Provider, c.Credential.Label,
				strings.Join(c.Credential.PrivilegedScopes, ", "), state)
		}
	}

	if unowned := countClass(audit.Credentials, core.CredentialUnowned); unowned > 0 {
		fmt.Printf("\n%d credential(s) have no reported author, so none of them can be judged.\n", unowned)
		fmt.Println("  The provider does not say who authorised them — that is a gap in the API, not a clean result.")
	}

	// The distinction that keeps this honest: nothing above claims a credential
	// is unused. None of these APIs report usage.
	if reporting := usageReportingProviders(audit.Summary); len(reporting) == 0 {
		fmt.Println("\nNo provider here reports when a credential was last used, so none is called unused.")
		fmt.Println("  Age and ownership are what can be known; activity is not.")
	}

	for _, name := range audit.Skipped {
		fmt.Printf("\n%s: no credential listing in its API — not checked, not clean.\n", name)
	}
	for name, msg := range audit.Warnings {
		fmt.Printf("\n%s: partial credential coverage — %s\n", name, msg)
	}
	for name, msg := range audit.Failed {
		fmt.Printf("\n%s: could not be read — %s\n", name, msg)
	}

	if cfg.IdentitySource.Provider == "" {
		fmt.Println("\nNo identity source configured, so nothing can be called orphaned.")
		fmt.Println("  Connect one and this report gains its only verdict that matters.")
	}
}

func printCredentialGroup(creds []core.ClassifiedCredential, class core.CredentialClass, title, why string) {
	group := make([]core.ClassifiedCredential, 0)
	for _, c := range creds {
		if c.Class == class {
			group = append(group, c)
		}
	}
	if len(group) == 0 {
		return
	}

	fmt.Printf("\n%s (%d)\n", title, len(group))
	fmt.Println(strings.Repeat("─", 72))
	fmt.Println("  " + why)
	for _, c := range group {
		fmt.Printf("  %-14s %-24s %s\n", c.Credential.Provider, c.Credential.Label, credentialAge(c.Credential))
		fmt.Printf("  %-14s %-24s %s\n", "", "", c.Reason)
	}
}

// credentialAge renders how long this has been in place, which is the argument
// an operator carries — and renders nothing rather than a fabricated date when
// the provider gave none.
func credentialAge(c core.Credential) string {
	if c.CreatedAt == nil {
		return "created date unknown"
	}
	return "since " + c.CreatedAt.Format("2006-01-02")
}

func filterOverreaching(creds []core.ClassifiedCredential) []core.ClassifiedCredential {
	out := make([]core.ClassifiedCredential, 0)
	for _, c := range creds {
		if c.Overreaching {
			out = append(out, c)
		}
	}
	return out
}

func countClass(creds []core.ClassifiedCredential, class core.CredentialClass) int {
	n := 0
	for _, c := range creds {
		if c.Class == class {
			n++
		}
	}
	return n
}

func usageReportingProviders(summaries []core.CredentialSummary) []string {
	out := make([]string, 0)
	for _, s := range summaries {
		if s.UsageKnown {
			out = append(out, s.Provider)
		}
	}
	return out
}
