package cli

import (
	"context"
	"fmt"
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
	err  error
}

func runScan(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	domain := scanDomain
	if domain == "" {
		domain = cfg.IdentitySource.Domain
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

	printScanReport(cfg, targets, results)
	return nil
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

			billing := caps.SuspendedBilling
			if billed, overridden := config.SuspendedBillingOverride(cfg.Providers[name]); overridden {
				billing = core.SuspendedBillingReleased
				if billed {
					billing = core.SuspendedBillingCharged
				}
			}

			set(scanResult{scan: core.Scan(core.ScanInput{
				Provider:          name,
				Users:             users,
				Domain:            domain,
				ReportsActivity:   caps.ReportsActivity,
				SuspendedBilling:  billing,
				InactiveThreshold: threshold,
				CostPerSeat:       cfg.Providers[name].CostPerSeat,
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

		rows = append(rows, []string{
			name,
			fmt.Sprintf("%d", s.Active),
			fmt.Sprintf("%d", s.Suspended),
			fmt.Sprintf("%d", s.Admins),
			money(s.MonthlyCost, s.CostPerSeat),
			money(s.MonthlyWaste, s.CostPerSeat),
			money(s.SuspendedExposure, s.CostPerSeat),
		})
	}

	if len(rows) > 0 {
		printTable([]string{"PROVIDER", "ACTIVE", "SUSPENDED", "ADMINS", "MONTHLY COST", "WASTED", "SUSPENDED €"}, rows)
	}

	for _, name := range targets {
		r := results[name]
		if r.err != nil || len(r.scan.Findings) == 0 {
			continue
		}
		fmt.Printf("\n%s\n", strings.ToUpper(name))
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

// money renders an amount, or a dash when the provider has no configured price
// — printing 0.00 for an unpriced provider would read as "this costs nothing".
func money(amount, costPerSeat float64) string {
	if costPerSeat == 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f", amount)
}
