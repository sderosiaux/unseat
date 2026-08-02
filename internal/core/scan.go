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
	// MonthlyWaste is retained for legacy JSON consumers. It is populated only
	// when the provider API stated money; manual config never feeds it.
	MonthlyWaste float64 `json:"monthly_waste"`
	// MonthlyWasteMinor is the same amount in minor units, preferred by new
	// callers. Nil means "unknown", not zero.
	MonthlyWasteMinor *int64 `json:"monthly_waste_minor,omitempty"`
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
	// Deprecated: manual pricing is ignored. Billing reports must come from the
	// provider API, or stay unpriced.
	CostPerSeat float64
	// Deprecated: manual invoice totals are ignored for the same reason as
	// CostPerSeat.
	MonthlySpend float64
	// Billing is whatever the provider's own API reported. It is the only
	// accepted source for price and billed-seat facts.
	Billing *Billing
	// SuspendedBilling mirrors Capabilities.SuspendedBilling. It is connector
	// knowledge, not operator-entered billing config.
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
	Provider  string    `json:"provider"`
	Total     int       `json:"total_seats"`
	Active    int       `json:"active"`
	Suspended int       `json:"suspended"`
	Admins    int       `json:"admins"`
	Findings  []Finding `json:"findings"`
	Billing   *Billing  `json:"billing,omitempty"`
	// CostPerSeat is retained for legacy JSON consumers. It is populated only
	// when the provider API stated a unit price.
	CostPerSeat      float64 `json:"cost_per_seat"`
	CostPerSeatMinor *int64  `json:"cost_per_seat_minor,omitempty"`
	// MonthlyCost is retained for legacy JSON consumers. New callers should use
	// MonthlyCostMinor.
	MonthlyCost float64 `json:"monthly_cost"`
	// MonthlyCostMinor is nil when the provider API did not state money.
	MonthlyCostMinor *int64 `json:"monthly_cost_minor,omitempty"`
	// RateSource records which provider API surface produced the billing facts.
	RateSource BillingSource `json:"billing_source,omitempty"`
	// Plan, BilledSeats and NextBillingAt are whatever the provider's own
	// subscription API reported. BilledSeats is authoritative and worth
	// comparing against the seat counts unseat derived itself.
	Plan          string     `json:"plan,omitempty"`
	BilledSeats   int        `json:"billed_seats,omitempty"`
	FilledSeats   int        `json:"filled_seats,omitempty"`
	NextBillingAt *time.Time `json:"next_billing_at,omitempty"`
	// BillingUnavailableReason explains why money is missing while seat counts
	// may still be present.
	BillingUnavailableReason string `json:"billing_unavailable_reason,omitempty"`
	// MonthlyWaste is retained for legacy JSON consumers. It is populated only
	// when the provider API stated money.
	MonthlyWaste      float64 `json:"monthly_waste"`
	MonthlyWasteMinor *int64  `json:"monthly_waste_minor,omitempty"`
	// SuspendedExposure is retained for legacy JSON consumers. It is populated
	// only when the provider API stated money and billing behavior allows it.
	SuspendedExposure      float64 `json:"suspended_exposure"`
	SuspendedExposureMinor *int64  `json:"suspended_exposure_minor,omitempty"`
}

// occupiedSeats returns how many purchased seats are actually taken, and the
// unused remainder.
//
// The vendor's own filled count wins when it reports one: it counts things our
// user listing cannot see — outside collaborators, pending invitations — and
// those are billed. Using our tally instead would overstate the gap and put a
// number in front of a finance conversation that the vendor would contradict.
func (s ProviderScan) occupiedSeats() (occupied, unused int) {
	occupied = s.FilledSeats
	if occupied == 0 {
		occupied = s.Active
	}
	return occupied, s.BilledSeats - occupied
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

	var rateMinor *int64
	if in.Billing != nil {
		billing := *in.Billing
		if billing.Provider == "" {
			billing.Provider = in.Provider
		}
		if billing.FetchedAt.IsZero() {
			billing.FetchedAt = now.UTC()
		}
		if billing.Source == "" {
			billing.Source = BillingSourceAPISeatCount
		}
		if billing.Confidence == "" {
			if billing.HasMoney() {
				billing.Confidence = BillingConfidenceExact
			} else {
				billing.Confidence = BillingConfidencePartial
			}
		}
		if !billing.HasMoney() && billing.UnavailableReason == "" {
			billing.UnavailableReason = "provider billing API did not return price or spend"
		}

		scan.Billing = &billing
		scan.Plan = billing.Plan
		scan.BilledSeats = billing.BilledSeatCount()
		scan.FilledSeats = billing.FilledSeatCount()
		scan.NextBillingAt = billing.NextBillingAt
		scan.RateSource = billing.Source
		scan.BillingUnavailableReason = billing.UnavailableReason
		rateMinor = billingUnitMinor(&billing)
		if rateMinor != nil {
			scan.CostPerSeatMinor = rateMinor
			scan.CostPerSeat = minorToMajor(*rateMinor)
		}
		if billing.MonthlyAmountMinor != nil {
			amount := *billing.MonthlyAmountMinor
			scan.MonthlyCostMinor = &amount
			scan.MonthlyCost = minorToMajor(amount)
		} else if rateMinor != nil {
			seatCount := scan.Active
			if scan.BilledSeats > 0 {
				seatCount = scan.BilledSeats
			}
			amount := int64(seatCount) * *rateMinor
			scan.MonthlyCostMinor = &amount
			scan.MonthlyCost = minorToMajor(amount)
		}
	}

	// Only a connector-known charged suspended seat may be priced. An unknown
	// contract is an access-review signal, not a savings claim.
	if in.SuspendedBilling == SuspendedBillingCharged && rateMinor != nil {
		exposure := int64(scan.Suspended) * *rateMinor
		scan.SuspendedExposureMinor = &exposure
		scan.SuspendedExposure = minorToMajor(exposure)
	}

	// The vendor's own purchased-seat count against the seats it considers
	// occupied. A gap here is money leaving with nothing attached to it, and
	// it is invisible from the user list alone — only the subscription knows.
	//
	// The occupied count comes from the vendor when it reports one: GitHub
	// counts outside collaborators and pending invitations as filled, so our
	// own active tally would overstate the gap.
	if occupied, gap := scan.occupiedSeats(); scan.BilledSeats > 0 && gap > 0 {
		msg := "the vendor bills " + strconv.Itoa(scan.BilledSeats) + " seats but only " +
			strconv.Itoa(occupied) + " are occupied — a prepaid block or plan minimum worth revisiting"
		finding := Finding{
			Kind:     FindingOverProvisioned,
			Severity: SeverityHigh,
			Count:    gap,
			Message:  msg,
		}
		if rateMinor != nil {
			waste := int64(gap) * *rateMinor
			finding.MonthlyWasteMinor = &waste
			finding.MonthlyWaste = minorToMajor(waste)
		} else {
			finding.Message += "; price unknown because the provider API did not state the seat rate"
		}
		scan.Findings = append(scan.Findings, finding)
	}

	if len(suspended) > 0 {
		scan.Findings = append(scan.Findings, suspendedFinding(in.SuspendedBilling, suspended, rateMinor))
	}

	if len(inactive) > 0 {
		days := int(in.InactiveThreshold.Hours() / 24)
		finding := Finding{
			Kind:     FindingInactive,
			Severity: SeverityHigh,
			Count:    len(inactive),
			Subjects: capSubjects(inactive),
			Message:  "no recorded activity in the last " + strconv.Itoa(days) + " days",
		}
		if rateMinor != nil {
			waste := int64(len(inactive)) * *rateMinor
			finding.MonthlyWasteMinor = &waste
			finding.MonthlyWaste = minorToMajor(waste)
		} else {
			finding.Message += "; price unknown because the provider API did not state the seat rate"
		}
		scan.Findings = append(scan.Findings, finding)
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

	var wasteMinor int64
	var wasteKnown bool
	for _, finding := range scan.Findings {
		if finding.MonthlyWasteMinor == nil {
			continue
		}
		wasteMinor += *finding.MonthlyWasteMinor
		wasteKnown = true
	}
	if wasteKnown {
		scan.MonthlyWasteMinor = &wasteMinor
		scan.MonthlyWaste = minorToMajor(wasteMinor)
	}

	sortFindings(scan.Findings)
	return scan
}

// suspendedFinding describes deactivated seats according to what is actually
// known about the vendor's billing, rather than assuming the expensive case.
func suspendedFinding(billing SuspendedBilling, subjects []string, costPerSeatMinor *int64) Finding {
	f := Finding{
		Count:    len(subjects),
		Subjects: capSubjects(subjects),
	}

	switch billing {
	case SuspendedBillingCharged:
		f.Kind = FindingSuspendedBilled
		f.Severity = SeverityHigh
		f.Message = "deactivated accounts still billed — this vendor charges for a seat until the user is fully deleted"
		if costPerSeatMinor != nil {
			waste := int64(len(subjects)) * *costPerSeatMinor
			f.MonthlyWasteMinor = &waste
			f.MonthlyWaste = minorToMajor(waste)
		} else {
			f.Message += "; price unknown because the provider API did not state the seat rate"
		}

	case SuspendedBillingReleased:
		f.Kind = FindingSuspendedAccounts
		f.Severity = SeverityInfo
		f.Message = "deactivated accounts, not billed by this vendor — no saving to make, but they remain reactivatable and are worth an access review"

	default:
		f.Kind = FindingSuspendedAccounts
		f.Severity = SeverityMed
		f.Message = "deactivated accounts still holding a seat — billing depends on the vendor and your contract; " +
			"unseat will not price them unless the provider API states they are billed"
	}

	return f
}

func billingUnitMinor(b *Billing) *int64 {
	if b == nil {
		return nil
	}
	if b.CostPerSeatMinor != nil {
		return b.CostPerSeatMinor
	}
	if b.MonthlyAmountMinor != nil && b.BilledSeatCount() > 0 {
		unit := (*b.MonthlyAmountMinor + int64(b.BilledSeatCount()/2)) / int64(b.BilledSeatCount())
		return &unit
	}
	return nil
}

func minorToMajor(amount int64) float64 {
	return float64(amount) / 100
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
