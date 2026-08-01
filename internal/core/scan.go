package core

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// FindingKind identifies a category of seat problem detectable from a single
// provider's user list, with no identity source and no group mappings.
//
// This is deliberately the cheapest possible analysis: it is what unseat can
// tell you before you have configured anything beyond an API key.
type FindingKind string

const (
	// FindingSuspendedBilled: the account is deactivated and the vendor is
	// known to keep billing it until deletion. Real, reclaimable money.
	FindingSuspendedBilled FindingKind = "suspended_but_billed"
	// FindingSuspendedAccounts: deactivated accounts that are not known to
	// cost anything. A hygiene and access-review signal, not a saving.
	FindingSuspendedAccounts FindingKind = "suspended_accounts"
	// FindingExternal: the identity is outside the corporate domain.
	FindingExternal FindingKind = "external_identity"
	// FindingAdminSprawl: an unusually large share of privileged accounts.
	FindingAdminSprawl FindingKind = "admin_sprawl"
	// FindingInactive: no activity within the threshold, on a provider whose
	// API actually reports activity.
	FindingInactive FindingKind = "inactive"
	// FindingNoActivityData: the provider cannot answer the usage question at all.
	FindingNoActivityData FindingKind = "no_activity_data"
	// FindingOverProvisioned: the vendor says it bills more seats than there
	// are live accounts — a prepaid block or a plan minimum nobody revisited.
	FindingOverProvisioned FindingKind = "billed_seats_unused"
)

// Severity ranks findings for display order.
type Severity string

const (
	SeverityHigh Severity = "high"
	SeverityMed  Severity = "medium"
	SeverityInfo Severity = "info"
)

// Finding is one actionable observation about a provider's seats.
type Finding struct {
	Kind     FindingKind `json:"kind"`
	Severity Severity    `json:"severity"`
	Count    int         `json:"count"`
	// Subjects lists the affected identities, capped for display.
	Subjects []string `json:"subjects,omitempty"`
	Message  string   `json:"message"`
	// MonthlyWaste is Count * cost_per_seat, zero when the provider is unpriced
	// or when the finding does not represent a reclaimable seat.
	MonthlyWaste float64 `json:"monthly_waste"`
}

// ScanInput describes one provider's seats to analyse.
type ScanInput struct {
	Provider string
	Users    []User
	// Domain is the corporate domain. Empty disables the external-identity check.
	Domain string
	// ReportsActivity mirrors Capabilities.ReportsActivity. When false, a nil
	// LastActivityAt is unknown, not inactive.
	ReportsActivity bool
	// InactiveThreshold is how long without activity counts as inactive.
	InactiveThreshold time.Duration
	// CostPerSeat is the monthly price of a seat; zero leaves money unreported.
	CostPerSeat float64
	// MonthlySpend is the real invoice total for this provider. When
	// CostPerSeat is unset, the rate is derived from it.
	MonthlySpend float64
	// Billing is whatever the provider's own API reported. It is used when
	// config states no price, so a connector that can read its subscription
	// needs no configuration at all.
	Billing *Billing
	// SuspendedBilling mirrors Capabilities.SuspendedBilling, optionally
	// overridden per provider in config for a non-standard contract.
	SuspendedBilling SuspendedBilling
	// Now is the reference time, injected for deterministic tests.
	Now time.Time
	// AdminRatioThreshold is the share of privileged accounts above which
	// admin sprawl is flagged. Zero falls back to defaultAdminRatio.
	AdminRatioThreshold float64
}

const defaultAdminRatio = 0.25

// maxSubjects caps how many identities a finding carries, so a scan of a large
// tenant stays readable and the JSON stays a summary rather than a dump.
const maxSubjects = 20

// ProviderScan is the analysis of one provider.
type ProviderScan struct {
	Provider    string    `json:"provider"`
	Total       int       `json:"total_seats"`
	Active      int       `json:"active"`
	Suspended   int       `json:"suspended"`
	Admins      int       `json:"admins"`
	Findings    []Finding `json:"findings"`
	CostPerSeat float64   `json:"cost_per_seat"`
	// MonthlyCost prices ACTIVE seats only. Vendors disagree on deactivated
	// seats — Linear drops them at the next billing cycle, HubSpot and others
	// bill until deletion — so counting them here would overstate spend for
	// half the connectors.
	MonthlyCost float64 `json:"monthly_cost"`
	// RateSource records how CostPerSeat was established, so a figure read
	// from a vendor API is never displayed with the same confidence as one
	// inferred from a plan name.
	RateSource BillingSource `json:"rate_source,omitempty"`
	// Plan, BilledSeats and NextBillingAt are whatever the provider's own
	// subscription API reported. BilledSeats is authoritative and worth
	// comparing against the seat counts unseat derived itself.
	Plan          string     `json:"plan,omitempty"`
	BilledSeats   int        `json:"billed_seats,omitempty"`
	NextBillingAt *time.Time `json:"next_billing_at,omitempty"`
	// MonthlyWaste is spend that is certainly wasted: active, billed seats
	// with no recorded usage.
	MonthlyWaste float64 `json:"monthly_waste"`
	// SuspendedExposure prices deactivated seats. Whether it is real money
	// depends on the contract, so it is reported apart from MonthlyWaste
	// rather than summed into a number that would be wrong either way.
	SuspendedExposure float64 `json:"suspended_exposure"`
}

// adminRoleTokens are substrings that mark a role as privileged.
//
// Role naming is provider-specific and unbounded ("admin", "owner",
// "org:admin", "Administrator", "workspace_owner"...), so this is a heuristic.
// It over-reports rather than under-reports: a false admin is a prompt to look,
// a missed admin is a blind spot.
var adminRoleTokens = []string{"admin", "owner", "superuser", "super_user", "root"}

// IsPrivilegedRole reports whether a provider role string looks privileged.
func IsPrivilegedRole(role string) bool {
	r := strings.ToLower(role)
	for _, token := range adminRoleTokens {
		if strings.Contains(r, token) {
			return true
		}
	}
	return false
}

// Scan analyses one provider's seats and returns findings ordered by severity.
func Scan(in ScanInput) ProviderScan {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	domain := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(in.Domain), "@"))

	scan := ProviderScan{
		Provider: in.Provider,
		Total:    len(in.Users),
	}

	var suspended, external, admins, inactive []string

	for _, u := range in.Users {
		email := strings.ToLower(strings.TrimSpace(u.Email))
		label := email
		if label == "" {
			label = u.DisplayName
		}

		if u.Status == StatusSuspended {
			scan.Suspended++
			suspended = append(suspended, label)
		} else {
			scan.Active++
		}

		if IsPrivilegedRole(u.Role) {
			scan.Admins++
			admins = append(admins, label)
		}

		if domain != "" && strings.Contains(email, "@") && !strings.HasSuffix(email, "@"+domain) {
			external = append(external, label)
		}

		// Inactivity only means something for a seat that is still live: a
		// deactivated account is obviously unused, and reporting it twice
		// inflates both the count and the money.
		if in.ReportsActivity && in.InactiveThreshold > 0 && u.Status != StatusSuspended {
			// A nil LastActivityAt on an instrumented provider means the API
			// has never seen this user active — the strongest signal there is.
			if u.LastActivityAt == nil || now.Sub(*u.LastActivityAt) > in.InactiveThreshold {
				inactive = append(inactive, label)
			}
		}
	}

	// Resolve the rate. A stated price wins; otherwise it is derived from the
	// invoice total divided by ACTIVE seats.
	//
	// Active is the right denominator because it is well defined whatever the
	// vendor bills: it answers "what does one person with access cost me",
	// which is the question every downstream number depends on. Dividing by
	// total seats would quietly deflate the rate on a tenant like Linear,
	// where 131 of 168 seats are deactivated.
	// Precedence: what the operator stated, then their invoice, then whatever
	// the provider's own API knows. The API path is what makes a priced report
	// possible with nothing but a key.
	var rate float64
	var source BillingSource

	switch {
	case in.CostPerSeat > 0:
		rate, source = in.CostPerSeat, BillingSourceConfig
	case in.MonthlySpend > 0 && scan.Active > 0:
		rate, source = in.MonthlySpend/float64(scan.Active), BillingSourceInvoice
	case in.Billing != nil && in.Billing.CostPerSeat > 0:
		rate, source = in.Billing.CostPerSeat, in.Billing.Source
	}

	scan.CostPerSeat = rate
	scan.RateSource = source

	if in.Billing != nil {
		scan.Plan = in.Billing.Plan
		scan.BilledSeats = in.Billing.BilledSeats
		scan.NextBillingAt = in.Billing.NextBillingAt
	}

	scan.MonthlyCost = float64(scan.Active) * rate

	// A released seat costs nothing, so it must not be priced — otherwise the
	// biggest number in the report is money that was never spent.
	if in.SuspendedBilling != SuspendedBillingReleased {
		scan.SuspendedExposure = float64(scan.Suspended) * rate
	}

	// The vendor's own billed-seat count against the accounts that actually
	// exist. A gap here is money leaving with nothing attached to it, and it
	// is invisible from the user list alone — only the subscription API knows.
	if gap := scan.BilledSeats - scan.Active; scan.BilledSeats > 0 && gap > 0 {
		scan.Findings = append(scan.Findings, Finding{
			Kind:     FindingOverProvisioned,
			Severity: SeverityHigh,
			Count:    gap,
			Message: "the vendor bills " + strconv.Itoa(scan.BilledSeats) + " seats but only " +
				strconv.Itoa(scan.Active) + " accounts are active — a prepaid block or plan minimum worth revisiting",
			MonthlyWaste: float64(gap) * rate,
		})
		scan.MonthlyCost = float64(scan.BilledSeats) * rate
	}

	if len(suspended) > 0 {
		scan.Findings = append(scan.Findings, suspendedFinding(in.SuspendedBilling, suspended, rate))
	}

	if len(inactive) > 0 {
		days := int(in.InactiveThreshold.Hours() / 24)
		scan.Findings = append(scan.Findings, Finding{
			Kind:         FindingInactive,
			Severity:     SeverityHigh,
			Count:        len(inactive),
			Subjects:     capSubjects(inactive),
			Message:      "no recorded activity in the last " + strconv.Itoa(days) + " days",
			MonthlyWaste: float64(len(inactive)) * rate,
		})
	}

	if len(external) > 0 {
		scan.Findings = append(scan.Findings, Finding{
			Kind:     FindingExternal,
			Severity: SeverityMed,
			Count:    len(external),
			Subjects: capSubjects(external),
			Message:  "identities outside " + domain + " — guests, contractors or personal accounts",
		})
	}

	ratio := in.AdminRatioThreshold
	if ratio <= 0 {
		ratio = defaultAdminRatio
	}
	if scan.Total > 0 && float64(scan.Admins)/float64(scan.Total) > ratio && scan.Admins > 1 {
		scan.Findings = append(scan.Findings, Finding{
			Kind:     FindingAdminSprawl,
			Severity: SeverityMed,
			Count:    scan.Admins,
			Subjects: capSubjects(admins),
			Message:  "privileged accounts exceed " + strconv.Itoa(int(ratio*100)) + "% of all seats (role names are matched heuristically — verify before acting)",
		})
	}

	if !in.ReportsActivity {
		scan.Findings = append(scan.Findings, Finding{
			Kind:     FindingNoActivityData,
			Severity: SeverityInfo,
			Message:  "this provider's API exposes no activity data — unused seats cannot be detected here",
		})
	}

	// Waste counts only live, billed seats with no usage, plus any seats the
	// vendor bills that no account occupies. Deactivated seats are priced into
	// SuspendedExposure instead, because whether they cost anything is a
	// contract question this tool cannot answer.
	reclaimable := make(map[string]bool, len(inactive))
	for _, e := range inactive {
		reclaimable[e] = true
	}
	scan.MonthlyWaste = float64(len(reclaimable)) * rate
	if gap := scan.BilledSeats - scan.Active; scan.BilledSeats > 0 && gap > 0 {
		scan.MonthlyWaste += float64(gap) * rate
	}

	sortFindings(scan.Findings)
	return scan
}

// suspendedFinding describes deactivated seats according to what is actually
// known about the vendor's billing, rather than assuming the expensive case.
func suspendedFinding(billing SuspendedBilling, subjects []string, costPerSeat float64) Finding {
	f := Finding{
		Count:    len(subjects),
		Subjects: capSubjects(subjects),
	}

	switch billing {
	case SuspendedBillingCharged:
		f.Kind = FindingSuspendedBilled
		f.Severity = SeverityHigh
		f.Message = "deactivated accounts still billed — this vendor charges for a seat until the user is fully deleted"
		f.MonthlyWaste = float64(len(subjects)) * costPerSeat

	case SuspendedBillingReleased:
		f.Kind = FindingSuspendedAccounts
		f.Severity = SeverityInfo
		f.Message = "deactivated accounts, not billed by this vendor — no saving to make, but they remain reactivatable and are worth an access review"

	default:
		f.Kind = FindingSuspendedAccounts
		f.Severity = SeverityMed
		f.Message = "deactivated accounts still holding a seat — billing depends on the vendor and your contract; " +
			"set bills_suspended_seats on this provider once you know which it is"
	}

	return f
}

func capSubjects(s []string) []string {
	sort.Strings(s)
	if len(s) > maxSubjects {
		return s[:maxSubjects]
	}
	return s
}

var severityRank = map[Severity]int{SeverityHigh: 0, SeverityMed: 1, SeverityInfo: 2}

func sortFindings(f []Finding) {
	sort.SliceStable(f, func(i, j int) bool {
		if severityRank[f[i].Severity] != severityRank[f[j].Severity] {
			return severityRank[f[i].Severity] < severityRank[f[j].Severity]
		}
		return f[i].Count > f[j].Count
	})
}
