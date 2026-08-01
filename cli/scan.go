package cli

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sderosiaux/unseat/config"
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

	results := scanAll(cmd.Context(), cfg, reg, targets, domain)

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
		return printJSON(map[string]any{
			"currency":       cfg.CurrencyLabel(),
			"threshold_days": scanDays,
			"providers":      payload,
		})
	}

	cacheScan(cmd.Context(), targets, results)
	printScanReport(cfg, targets, results)
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
	defer db.Close()

	for _, name := range targets {
		r := results[name]
		// A provider that failed keeps whatever was cached before rather than
		// being wiped: UpsertProviderUsers deletes before inserting, so caching
		// an empty result would turn a fetch error into a confident "no seats".
		if r.err != nil || len(r.users) == 0 {
			continue
		}
		if err := db.UpsertProviderUsers(ctx, name, r.users); err != nil {
			slog.Debug("cache provider users failed", "provider", name, "error", err)
			continue
		}
		if err := db.UpdateSyncState(ctx, name, len(r.users), r.reportsActivity); err != nil {
			slog.Debug("update sync state failed", "provider", name, "error", err)
		}
	}
}

// scanAll queries every target provider concurrently. Providers are independent
// remote systems, so a slow or broken one must not delay the rest.
func scanAll(ctx context.Context, cfg *config.Config, reg *provider.Registry, targets []string, domain string) map[string]scanResult {
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

			// Ask the provider about its own subscription. A price unseat can
			// read is a price nobody has to type. A failure here must not sink
			// the scan: the seat findings stand on their own.
			var sub *core.Billing
			if bp, ok := p.(provider.BillingProvider); ok {
				b, err := bp.Billing(ctx)
				if err != nil {
					slog.Debug("billing lookup failed", "provider", name, "error", err)
				} else {
					sub = b
				}
			}

			suspendedBilling := caps.SuspendedBilling
			if billed, overridden := config.SuspendedBillingOverride(cfg.Providers[name]); overridden {
				suspendedBilling = core.SuspendedBillingReleased
				if billed {
					suspendedBilling = core.SuspendedBillingCharged
				}
			}

			set(scanResult{users: users, reportsActivity: caps.ReportsActivity, scan: core.Scan(core.ScanInput{
				Provider:          name,
				Users:             users,
				Domain:            domain,
				ReportsActivity:   caps.ReportsActivity,
				SuspendedBilling:  suspendedBilling,
				InactiveThreshold: threshold,
				CostPerSeat:       cfg.Providers[name].CostPerSeat,
				MonthlySpend:      cfg.Providers[name].MonthlySpend,
				Billing:           sub,
				Now:               now,
			})})
		}(name)
	}

	wg.Wait()
	return results
}

func printScanReport(cfg *config.Config, targets []string, results map[string]scanResult) {
	currency := cfg.CurrencyLabel()

	var totalSeats int
	var totalCost, totalWaste, totalExposure float64
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
		totalSeats += s.Total
		totalCost += s.MonthlyCost
		totalWaste += s.MonthlyWaste
		totalExposure += s.SuspendedExposure
		if s.CostPerSeat == 0 {
			unpriced = append(unpriced, name)
		}

		// The rate carries a marker for how it was established: a figure read
		// from a vendor API and one typed by hand must not look alike.
		perSeat := money(s.CostPerSeat, s.CostPerSeat) + rateMarker(s.RateSource)

		rows = append(rows, []string{
			name,
			fmt.Sprintf("%d", s.Active),
			fmt.Sprintf("%d", s.Suspended),
			fmt.Sprintf("%d", s.Admins),
			perSeat,
			money(s.MonthlyCost, s.CostPerSeat),
			money(s.MonthlyWaste, s.CostPerSeat),
			money(s.SuspendedExposure, s.CostPerSeat),
		})
	}

	if len(rows) > 0 {
		printTable([]string{"PROVIDER", "ACTIVE", "SUSPENDED", "ADMINS", "PER SEAT", "MONTHLY COST", "WASTED", "SUSPENDED €"}, rows)
		printRateLegend(targets, results)
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

	fmt.Printf("\n%d seats across %d provider(s).\n", totalSeats, len(rows))
	if totalCost > 0 {
		fmt.Printf("Active spend: %.2f %s/month.\n", totalCost, currency)
	}
	if totalWaste > 0 {
		fmt.Printf("Confirmed waste (billed, no recorded usage): %.2f %s/month — %.2f %s/year.\n",
			totalWaste, currency, totalWaste*12, currency)
	}
	if totalExposure > 0 {
		fmt.Printf("Deactivated seats: %.2f %s/month IF your contract bills them until deletion.\n",
			totalExposure, currency)
		fmt.Println("Vendors differ — Linear releases them at the next cycle, others bill until the user is deleted.")
	}

	if len(unpriced) > 0 {
		sort.Strings(unpriced)
		fmt.Printf("\nUnpriced (%d): %s\n", len(unpriced), strings.Join(unpriced, ", "))
		fmt.Println("Set `cost_per_seat` on these providers to turn seat counts into money.")
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

// rateMarkers annotate each rate with how it was established. Principle 3:
// never state a number more precisely than it is known.
var rateMarkers = map[core.BillingSource]string{
	core.BillingSourceAPI:     "",
	core.BillingSourcePlan:    "~",
	core.BillingSourceInvoice: "*",
	core.BillingSourceConfig:  "",
}

var rateLegend = map[core.BillingSource]string{
	core.BillingSourcePlan:    "~ inferred from the plan identifier reported by the vendor's API — confirm against your invoice",
	core.BillingSourceInvoice: "* derived from monthly_spend / active seats",
}

func rateMarker(s core.BillingSource) string { return rateMarkers[s] }

func printRateLegend(targets []string, results map[string]scanResult) {
	seen := map[core.BillingSource]bool{}
	for _, name := range targets {
		if r := results[name]; r.err == nil {
			seen[r.scan.RateSource] = true
		}
	}
	var lines []string
	for src, text := range rateLegend {
		if seen[src] {
			lines = append(lines, text)
		}
	}
	sort.Strings(lines)
	for _, l := range lines {
		fmt.Println("\n" + l)
	}
}

// money renders an amount, or a dash when the provider has no configured price
// — printing 0.00 for an unpriced provider would read as "this costs nothing".
func money(amount, costPerSeat float64) string {
	if costPerSeat == 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f", amount)
}
