package cli

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider"
	"github.com/spf13/cobra"
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Read-only audit of your SaaS seats — no identity source, no mappings required",
	Long: `Scan reports what is wrong with your SaaS seats using nothing but provider API keys.

It never writes to a provider. It needs no Google Workspace connection and no
group mappings, so it works before any of unseat's reconciliation is set up.`,
	RunE: runScan,
}

var (
	scanProviders []string
	scanDomain    string
	scanDays      int
)

func init() {
	scanCmd.Flags().StringSliceVar(&scanProviders, "provider", nil, "Restrict the scan to these providers (default: all configured)")
	scanCmd.Flags().StringVar(&scanDomain, "domain", "", "Corporate domain for the external-identity check (default: identity_source.domain)")
	scanCmd.Flags().IntVar(&scanDays, "days", 60, "Inactivity threshold in days")
	rootCmd.AddCommand(scanCmd)
}

type scanResult struct {
	scan core.ProviderScan
	// users is retained so seats can be correlated across providers, which is
	// the one question no single provider can answer.
	users []core.User
	// credentialCache carries the non-human access inventory read during scan.
	// It is cached for API/MCP/dashboard consumers even though scan itself does
	// not require a directory and therefore cannot prove orphaned ownership.
	credentialCache credentialCacheEntry
	// reportsActivity is the capability AS OBSERVED on the live provider. It
	// cannot be recomputed later: GitHub only learns it by calling the audit
	// log, so a freshly constructed provider answers false.
	reportsActivity bool
	err             error
}

func runScan(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	domain := scanDomain
	if domain == "" {
		domain = cfg.CorporateDomain()
	}

	// No identity source is built: scan must work with API keys alone.
	reg, _, err := provider.BuildRegistryWithIdentity(cfg, nil)
	if err != nil {
		return fmt.Errorf("initialize providers: %w", err)
	}

	targets := scanProviders
	if len(targets) == 0 {
		targets = configuredProviders(cfg)
	}
	sort.Strings(targets)

	if len(targets) == 0 {
		fmt.Println("No providers have credentials yet.")
		fmt.Println("Add one with `unseat providers add <name>`, or set its api_key in your config.")
		fmt.Println("See what is available with `unseat providers supported`.")
		return nil
	}

	results := scanAll(cmd.Context(), reg, targets, domain)

	// Cached before the output branch: a scripted --json run left the cache
	// untouched, so a later `audit inactive` answered from whatever the last
	// interactive run had written, or from nothing.
	cacheScan(cmd.Context(), targets, results)

	if jsonOutput {
		payload := make([]any, 0, len(results))
		for _, name := range targets {
			r := results[name]
			if r.err != nil {
				payload = append(payload, map[string]any{"provider": name, "error": r.err.Error()})
				continue
			}
			payload = append(payload, r.scan)
		}
		// The cross-provider correlation is the one finding no single vendor
		// can produce, and it was reachable only by reading the terminal.
		seats := core.SeatsByProvider{}
		for _, name := range targets {
			if r := results[name]; r.err == nil {
				seats[name] = r.users
			}
		}
		return printJSON(map[string]any{
			"threshold_days":         scanDays,
			"providers":              payload,
			"credential_inventory":   scanCredentialPayload(targets, results),
			"incomplete_offboarding": core.FindOffboardingGaps(seats),
		})
	}

	printScanReport(targets, results)
	return nil
}

// cacheScan stores the seat inventory scan just read.
//
// Nothing else populated it. provider_users was only ever written by the
// reconciler, so on a read-only, audit-only deployment the table stays empty —
// and every store-backed view (audit inactive, the MCP tools, the REST API and
// the dashboard) answered "nothing found" from an empty cache. A tool that
// reports a clean result it never computed is the failure mode this codebase
// exists to avoid.
//
// This writes to the LOCAL SQLite file only. No provider is touched.
//
// It is best-effort by design: scan's value is that it needs no state, and an
// unwritable cache must degrade the stale views, never the scan itself.
func cacheScan(ctx context.Context, targets []string, results map[string]scanResult) {
	db, err := openStore()
	if err != nil {
		slog.Debug("scan cache unavailable", "error", err)
		return
	}
	defer func() { _ = db.Close() }()

	for _, name := range targets {
		r := results[name]
		if r.credentialCache.Provider != "" {
			writeCredentialSnapshots(ctx, db, []credentialCacheEntry{r.credentialCache})
		}
		if r.err != nil {
			continue
		}
		if r.scan.Billing != nil {
			if err := db.InsertBillingSnapshot(ctx, *r.scan.Billing); err != nil {
				slog.Debug("cache billing snapshot failed", "provider", name, "error", err)
			}
		}
		// A provider that returned no users keeps whatever user cache existed
		// before rather than being wiped: UpsertProviderUsers deletes before
		// inserting, so caching an empty result could turn a fetch anomaly into
		// a confident "no seats".
		if len(r.users) > 0 {
			if err := db.UpsertProviderUsers(ctx, name, r.users); err != nil {
				slog.Debug("cache provider users failed", "provider", name, "error", err)
				continue
			}
			if err := db.UpdateSyncState(ctx, name, len(r.users), r.reportsActivity); err != nil {
				slog.Debug("update sync state failed", "provider", name, "error", err)
			}
		}
	}
}

// scanAll queries every target provider concurrently. Providers are independent
// remote systems, so a slow or broken one must not delay the rest.
func scanAll(ctx context.Context, reg *provider.Registry, targets []string, domain string) map[string]scanResult {
	threshold := time.Duration(scanDays) * 24 * time.Hour
	now := time.Now()

	results := make(map[string]scanResult, len(targets))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, name := range targets {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()

			set := func(r scanResult) {
				mu.Lock()
				results[name] = r
				mu.Unlock()
			}

			p, err := reg.Get(name)
			if err != nil {
				set(scanResult{err: err})
				return
			}

			users, err := p.ListUsers(ctx)
			if err != nil {
				set(scanResult{err: err})
				return
			}

			caps := p.Capabilities()
			credentialCache, _ := inspectProviderCredentials(ctx, name, p, nil, domain, nil)

			// Ask the provider about its own subscription. A price unseat can
			// read is a price nobody has to type. A failure here must not sink
			// the scan: the seat findings stand on their own.
			var sub *core.Billing
			if bp, ok := p.(provider.BillingProvider); ok {
				b, err := bp.Billing(ctx)
				if err != nil {
					slog.Debug("billing lookup failed", "provider", name, "error", err)
					sub = unavailableBilling(name, now, "billing API unavailable or missing scope: "+err.Error())
				} else {
					sub = b
				}
				if sub == nil {
					sub = unavailableBilling(name, now, "provider billing API returned no subscription data")
				}
			} else {
				sub = unavailableBilling(name, now, "provider connector does not expose billing API data yet")
			}

			set(scanResult{users: users, credentialCache: credentialCache, reportsActivity: caps.ReportsActivity, scan: core.Scan(core.ScanInput{
				Provider:          name,
				Users:             users,
				Domain:            domain,
				ReportsActivity:   caps.ReportsActivity,
				SuspendedBilling:  caps.SuspendedBilling,
				InactiveThreshold: threshold,
				Billing:           sub,
				Now:               now,
			})})
		}(name)
	}

	wg.Wait()
	return results
}

func unavailableBilling(provider string, fetchedAt time.Time, reason string) *core.Billing {
	return &core.Billing{
		Provider:          provider,
		FetchedAt:         fetchedAt.UTC(),
		Source:            core.BillingSourceUnavailable,
		Confidence:        core.BillingConfidenceUnavailable,
		UnavailableReason: reason,
	}
}

func printScanReport(targets []string, results map[string]scanResult) {
	var totalSeats int
	totalCost := map[string]int64{}
	totalWaste := map[string]int64{}
	totalExposure := map[string]int64{}
	var failures []string
	var unpriced []string

	rows := make([][]string, 0, len(targets))
	for _, name := range targets {
		r := results[name]
		if r.err != nil {
			failures = append(failures, fmt.Sprintf("  %-16s %v", name, r.err))
			continue
		}
		s := r.scan
		currency := scanCurrency(s)
		totalSeats += s.Total
		addMoney(totalCost, currency, s.MonthlyCostMinor)
		addMoney(totalWaste, currency, s.MonthlyWasteMinor)
		addMoney(totalExposure, currency, s.SuspendedExposureMinor)
		if !scanHasMoney(s) {
			unpriced = append(unpriced, name)
		}

		rows = append(rows, []string{
			name,
			fmt.Sprintf("%d", s.Active),
			fmt.Sprintf("%d", s.Suspended),
			fmt.Sprintf("%d", s.Admins),
			moneyMinor(s.CostPerSeatMinor, currency),
			moneyMinor(s.MonthlyCostMinor, currency),
			moneyMinor(s.MonthlyWasteMinor, currency),
			moneyMinor(s.SuspendedExposureMinor, currency),
		})
	}

	if len(rows) > 0 {
		printTable([]string{"PROVIDER", "ACTIVE", "SUSPENDED", "ADMINS", "PER SEAT", "MONTHLY COST", "WASTED", "SUSPENDED WASTE"}, rows)
	}

	for _, name := range targets {
		r := results[name]
		if r.err != nil || len(r.scan.Findings) == 0 {
			continue
		}
		fmt.Printf("\n%s", strings.ToUpper(name))
		// Subscription facts read straight from the vendor, printed so the
		// figures above can be checked against the account they came from.
		if r.scan.Plan != "" {
			fmt.Printf("  [plan %s", r.scan.Plan)
			if r.scan.BilledSeats > 0 {
				fmt.Printf(", %d seats billed", r.scan.BilledSeats)
			}
			if r.scan.NextBillingAt != nil {
				fmt.Printf(", renews %s", r.scan.NextBillingAt.Format("2006-01-02"))
			}
			fmt.Print("]")
		}
		fmt.Println()
		for _, f := range r.scan.Findings {
			marker := map[core.Severity]string{
				core.SeverityHigh: "!!",
				core.SeverityMed:  " !",
				core.SeverityInfo: "  ",
			}[f.Severity]

			if f.Count > 0 {
				fmt.Printf("  %s %d × %s — %s\n", marker, f.Count, f.Kind, f.Message)
			} else {
				fmt.Printf("  %s %s — %s\n", marker, f.Kind, f.Message)
			}
			if len(f.Subjects) > 0 {
				shown := f.Subjects
				suffix := ""
				if f.Count > len(shown) {
					suffix = fmt.Sprintf(" (+%d more)", f.Count-len(shown))
				}
				fmt.Printf("       %s%s\n", strings.Join(shown, ", "), suffix)
			}
		}
	}

	printOffboardingGaps(targets, results)
	printCredentialScanSummary(targets, results)

	fmt.Printf("\n%d seats across %d provider(s).\n", totalSeats, len(rows))
	printMoneyTotals("Active spend", totalCost, "/month")
	printWasteTotals(totalWaste)
	printMoneyTotals("Deactivated billed-seat exposure", totalExposure, "/month")

	if len(unpriced) > 0 {
		sort.Strings(unpriced)
		fmt.Printf("\nPrice/spend unknown via provider API (%d): %s\n", len(unpriced), strings.Join(unpriced, ", "))
		fmt.Println("No YAML price is used for this report; when the API cannot state spend, unseat reports counts and the unavailable reason.")
		for _, name := range unpriced {
			if reason := results[name].scan.BillingUnavailableReason; reason != "" {
				fmt.Printf("  %-16s %s\n", name, reason)
			}
		}
	}

	if len(failures) > 0 {
		fmt.Printf("\n%d provider(s) could not be read:\n", len(failures))
		for _, f := range failures {
			fmt.Println(f)
		}
	}
}

// printOffboardingGaps reports identities one provider has deactivated while
// others still grant access. It needs at least two providers to say anything,
// and it is the one question no single vendor can answer.
func printOffboardingGaps(targets []string, results map[string]scanResult) {
	seats := core.SeatsByProvider{}
	for _, name := range targets {
		if r := results[name]; r.err == nil {
			seats[name] = r.users
		}
	}
	if len(seats) < 2 {
		return
	}

	gaps := core.FindOffboardingGaps(seats)
	if len(gaps) == 0 {
		return
	}

	fmt.Printf("\nINCOMPLETE OFFBOARDING (%d)\n", len(gaps))
	fmt.Println("  Deactivated somewhere, still holding a live seat elsewhere.")

	shown := gaps
	if len(shown) > maxGapsShown {
		shown = shown[:maxGapsShown]
	}
	for _, g := range shown {
		fmt.Printf("  !! %-38s off in %-18s still active in %s\n",
			g.Email, strings.Join(g.DeactivatedIn, ","), strings.Join(g.StillActiveIn, ","))
	}
	if len(gaps) > len(shown) {
		fmt.Printf("     (+%d more — use --json for the full list)\n", len(gaps)-len(shown))
	}
}

// maxGapsShown keeps the terminal report readable; --json carries everything.
const maxGapsShown = 25

func printCredentialScanSummary(targets []string, results map[string]scanResult) {
	rows := make([][]string, 0, len(targets))
	var caveats []string
	for _, name := range targets {
		entry := results[name].credentialCache
		if entry.Provider == "" {
			continue
		}
		if entry.Message != "" {
			caveats = append(caveats, fmt.Sprintf("  %-16s %s", name, entry.Message))
		}
		summary := core.SummarizeCredentials(name, entry.Credentials, entry.UsageKnown)
		total := summary.Total
		if entry.Status != credentialSyncOK {
			total = len(entry.Credentials)
		}
		rows = append(rows, []string{
			name,
			entry.Status,
			fmt.Sprintf("%d", total),
			fmt.Sprintf("%d", summary.External),
			fmt.Sprintf("%d", summary.Dormant),
			fmt.Sprintf("%d", summary.Unowned),
			fmt.Sprintf("%d", summary.Overreaching),
			yesNo(entry.UsageKnown),
		})
	}
	if len(rows) == 0 {
		return
	}

	fmt.Println("\nNON-HUMAN ACCESS")
	printTable([]string{"PROVIDER", "STATUS", "TOTAL", "EXTERNAL", "DORMANT", "UNOWNED", "OVERREACH", "USAGE DATA"}, rows)
	fmt.Println("  Cached from provider APIs during scan. Without an identity source, owner employment is not judged.")
	for _, caveat := range caveats {
		fmt.Println(caveat)
	}
}

type scanCredentialInventory struct {
	Provider        string                 `json:"provider"`
	Status          string                 `json:"status"`
	CredentialCount int                    `json:"credential_count"`
	UsageKnown      bool                   `json:"usage_known"`
	Message         string                 `json:"message,omitempty"`
	Summary         core.CredentialSummary `json:"summary,omitempty"`
}

func scanCredentialPayload(targets []string, results map[string]scanResult) []scanCredentialInventory {
	out := make([]scanCredentialInventory, 0, len(targets))
	for _, name := range targets {
		entry := results[name].credentialCache
		if entry.Provider == "" {
			continue
		}
		out = append(out, scanCredentialInventory{
			Provider:        name,
			Status:          entry.Status,
			CredentialCount: len(entry.Credentials),
			UsageKnown:      entry.UsageKnown,
			Message:         entry.Message,
			Summary:         core.SummarizeCredentials(name, entry.Credentials, entry.UsageKnown),
		})
	}
	if out == nil {
		out = []scanCredentialInventory{}
	}
	return out
}

func scanCurrency(s core.ProviderScan) string {
	if s.Billing == nil {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(s.Billing.Currency))
}

func scanHasMoney(s core.ProviderScan) bool {
	return s.MonthlyCostMinor != nil || s.CostPerSeatMinor != nil
}

func addMoney(totals map[string]int64, currency string, amount *int64) {
	if amount == nil {
		return
	}
	totals[currency] += *amount
}

// moneyMinor renders an amount, or a dash when the provider API did not state
// money. Printing 0.00 for an unknown price would read as "this costs nothing".
func moneyMinor(amount *int64, currency string) string {
	if amount == nil {
		return "—"
	}
	return formatMoneyMinor(*amount, currency)
}

func formatMoneyMinor(amount int64, currency string) string {
	value := fmt.Sprintf("%.2f", float64(amount)/100)
	if currency == "" {
		return value
	}
	return value + " " + currency
}

func printMoneyTotals(label string, totals map[string]int64, suffix string) {
	for _, currency := range sortedMoneyCurrencies(totals) {
		amount := totals[currency]
		if amount == 0 {
			continue
		}
		fmt.Printf("%s: %s%s.\n", label, formatMoneyMinor(amount, currency), suffix)
	}
}

func printWasteTotals(totals map[string]int64) {
	for _, currency := range sortedMoneyCurrencies(totals) {
		amount := totals[currency]
		if amount == 0 {
			continue
		}
		fmt.Printf("Confirmed waste (billed, no recorded usage): %s/month — %s/year.\n",
			formatMoneyMinor(amount, currency), formatMoneyMinor(amount*12, currency))
	}
}

func sortedMoneyCurrencies(totals map[string]int64) []string {
	currencies := make([]string, 0, len(totals))
	for currency := range totals {
		currencies = append(currencies, currency)
	}
	sort.Strings(currencies)
	return currencies
}
