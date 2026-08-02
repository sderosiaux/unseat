package core

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var scanNow = time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

func daysAgo(n int) *time.Time {
	t := scanNow.AddDate(0, 0, -n)
	return &t
}

func findingOf(t *testing.T, s ProviderScan, kind FindingKind) *Finding {
	t.Helper()
	for i := range s.Findings {
		if s.Findings[i].Kind == kind {
			return &s.Findings[i]
		}
	}
	return nil
}

func TestScanCountsSeats(t *testing.T) {
	s := Scan(ScanInput{
		Provider: "figma",
		Users: []User{
			{Email: "a@co.com", Status: "active"},
			{Email: "b@co.com", Status: "active"},
			{Email: "c@co.com", Status: "suspended"},
		},
		Domain: "co.com",
		Now:    scanNow,
	})

	assert.Equal(t, 3, s.Total)
	assert.Equal(t, 2, s.Active)
	assert.Equal(t, 1, s.Suspended)
}

func TestScanFlagsSuspendedButBilled(t *testing.T) {
	s := Scan(ScanInput{
		Provider: "figma",
		Users: []User{
			{Email: "a@co.com", Status: "active"},
			{Email: "gone@co.com", Status: "suspended"},
		},
		Domain:      "co.com",
		CostPerSeat: 10,
		Now:         scanNow,
	})

	// Billing behaviour unverified for this vendor: report the accounts, flag
	// the cost as conditional, claim no saving.
	f := findingOf(t, s, FindingSuspendedAccounts)
	require.NotNil(t, f, "a deactivated account still holding a seat must be reported")
	assert.Equal(t, 1, f.Count)
	assert.Equal(t, []string{"gone@co.com"}, f.Subjects)
	assert.Zero(t, f.MonthlyWaste)
	assert.Nil(t, findingOf(t, s, FindingSuspendedBilled))

	assert.Zero(t, s.SuspendedExposure, "manual cost_per_seat is ignored")
	assert.Nil(t, s.SuspendedExposureMinor)
	assert.Zero(t, s.MonthlyWaste)
	assert.Zero(t, s.MonthlyCost)
}

// Linear releases suspended seats at the next billing cycle. Pricing them
// would make the largest line of the report money that was never spent, and
// bury the seats that genuinely cost something.
func TestScanSuspendedSeatsNotBilledCostNothing(t *testing.T) {
	s := Scan(ScanInput{
		Provider: "linear",
		Users: []User{
			{Email: "a@co.com", Status: StatusActive},
			{Email: "gone@co.com", Status: StatusSuspended},
			{Email: "left@co.com", Status: StatusSuspended},
		},
		Domain:           "co.com",
		CostPerSeat:      8,
		SuspendedBilling: SuspendedBillingReleased,
		Now:              scanNow,
	})

	f := findingOf(t, s, FindingSuspendedAccounts)
	require.NotNil(t, f, "still worth an access review even when free")
	assert.Equal(t, SeverityInfo, f.Severity, "no money at stake, so it must not compete with real findings")
	assert.Zero(t, f.MonthlyWaste)

	assert.Zero(t, s.SuspendedExposure, "a released seat costs nothing")
	assert.Zero(t, s.MonthlyCost, "manual cost_per_seat is ignored")
	assert.Nil(t, findingOf(t, s, FindingSuspendedBilled))
}

// Where the vendor is known to charge until deletion, this is real money and
// should outrank everything else.
func TestScanSuspendedSeatsBilledAreWaste(t *testing.T) {
	s := Scan(ScanInput{
		Provider: "hubspot",
		Users: []User{
			{Email: "a@co.com", Status: StatusActive},
			{Email: "gone@co.com", Status: StatusSuspended},
		},
		Domain:           "co.com",
		CostPerSeat:      90,
		SuspendedBilling: SuspendedBillingCharged,
		Now:              scanNow,
	})

	f := findingOf(t, s, FindingSuspendedBilled)
	require.NotNil(t, f)
	assert.Equal(t, SeverityHigh, f.Severity)
	assert.Zero(t, f.MonthlyWaste, "manual cost_per_seat is ignored")
	assert.Nil(t, f.MonthlyWasteMinor)
	assert.Zero(t, s.SuspendedExposure)
	assert.Nil(t, findingOf(t, s, FindingSuspendedAccounts))
}

func TestScanFlagsExternalIdentities(t *testing.T) {
	s := Scan(ScanInput{
		Provider: "figma",
		Users: []User{
			{Email: "a@co.com", Status: "active"},
			{Email: "freelance@agency.com", Status: "active"},
		},
		Domain: "co.com",
		Now:    scanNow,
	})

	f := findingOf(t, s, FindingExternal)
	require.NotNil(t, f)
	assert.Equal(t, []string{"freelance@agency.com"}, f.Subjects)
	// An external identity is a decision to make, not money to reclaim.
	assert.Zero(t, f.MonthlyWaste)
}

// Without a configured domain there is no way to tell inside from outside, so
// nothing may be reported as external.
func TestScanWithoutDomainReportsNoExternals(t *testing.T) {
	s := Scan(ScanInput{
		Provider: "figma",
		Users: []User{
			{Email: "a@co.com", Status: "active"},
			{Email: "freelance@agency.com", Status: "active"},
		},
		Now: scanNow,
	})
	assert.Nil(t, findingOf(t, s, FindingExternal))
}

func TestScanInactivityOnlyWhenInstrumented(t *testing.T) {
	users := []User{
		{Email: "fresh@co.com", Status: "active", LastActivityAt: daysAgo(2)},
		{Email: "stale@co.com", Status: "active", LastActivityAt: daysAgo(200)},
		{Email: "never@co.com", Status: "active"},
	}

	t.Run("instrumented provider reports inactivity", func(t *testing.T) {
		s := Scan(ScanInput{
			Provider:          "linear",
			Users:             users,
			Domain:            "co.com",
			ReportsActivity:   true,
			InactiveThreshold: 60 * 24 * time.Hour,
			Now:               scanNow,
		})
		f := findingOf(t, s, FindingInactive)
		require.NotNil(t, f)
		// "never seen" is the strongest inactivity signal, not an unknown.
		assert.ElementsMatch(t, []string{"never@co.com", "stale@co.com"}, f.Subjects)
		assert.Nil(t, findingOf(t, s, FindingNoActivityData))
	})

	t.Run("silent provider reports unknown, never inactivity", func(t *testing.T) {
		s := Scan(ScanInput{
			Provider:          "figma",
			Users:             users,
			Domain:            "co.com",
			ReportsActivity:   false,
			InactiveThreshold: 60 * 24 * time.Hour,
			Now:               scanNow,
		})
		assert.Nil(t, findingOf(t, s, FindingInactive),
			"a null last-activity on an uninstrumented provider means unknown, not inactive")
		require.NotNil(t, findingOf(t, s, FindingNoActivityData))
	})
}

// A deactivated account is trivially unused. Reporting it as inactivity too
// would double-count both the seat and its money.
func TestScanDoesNotCountSuspendedSeatAsInactive(t *testing.T) {
	s := Scan(ScanInput{
		Provider: "linear",
		Users: []User{
			{Email: "gone@co.com", Status: StatusSuspended, LastActivityAt: daysAgo(300)},
		},
		Domain:            "co.com",
		ReportsActivity:   true,
		InactiveThreshold: 60 * 24 * time.Hour,
		CostPerSeat:       10,
		Now:               scanNow,
	})

	require.NotNil(t, findingOf(t, s, FindingSuspendedAccounts))
	assert.Nil(t, findingOf(t, s, FindingInactive), "a deactivated seat is not an inactivity finding")
	assert.Zero(t, s.MonthlyWaste)
	assert.Zero(t, s.SuspendedExposure)
	assert.Zero(t, s.MonthlyCost, "no active seats")
}

func TestScanAdminSprawl(t *testing.T) {
	t.Run("flags a privileged majority", func(t *testing.T) {
		s := Scan(ScanInput{
			Provider: "figma",
			Users: []User{
				{Email: "a@co.com", Status: "active", Role: "admin"},
				{Email: "b@co.com", Status: "active", Role: "Owner"},
				{Email: "c@co.com", Status: "active", Role: "member"},
			},
			Domain: "co.com",
			Now:    scanNow,
		})
		assert.Equal(t, 2, s.Admins)
		require.NotNil(t, findingOf(t, s, FindingAdminSprawl))
	})

	t.Run("a lone admin is normal, not sprawl", func(t *testing.T) {
		users := []User{{Email: "a@co.com", Status: "active", Role: "admin"}}
		for i := 0; i < 3; i++ {
			users = append(users, User{Email: string(rune('b'+i)) + "@co.com", Status: "active", Role: "member"})
		}
		s := Scan(ScanInput{Provider: "figma", Users: users, Domain: "co.com", Now: scanNow})
		assert.Nil(t, findingOf(t, s, FindingAdminSprawl))
	})
}

func TestIsPrivilegedRole(t *testing.T) {
	for _, role := range []string{"admin", "ADMIN", "Owner", "org:admin", "workspace_owner", "superuser", "root"} {
		assert.True(t, IsPrivilegedRole(role), role)
	}
	for _, role := range []string{"member", "viewer", "editor", "guest", "", "sales-rep"} {
		assert.False(t, IsPrivilegedRole(role), role)
	}
}

func TestScanCostFromProviderAPI(t *testing.T) {
	s := Scan(ScanInput{
		Provider: "figma",
		Users: []User{
			{Email: "a@co.com", Status: "active"},
			{Email: "b@co.com", Status: "active"},
			{Email: "c@co.com", Status: "suspended"},
		},
		Domain: "co.com",
		Billing: &Billing{
			Provider:         "figma",
			CostPerSeatMinor: Int64Ptr(1200),
			Source:           BillingSourceAPIInvoice,
			Confidence:       BillingConfidenceExact,
		},
		SuspendedBilling: SuspendedBillingCharged,
		Now:              scanNow,
	})
	assert.InDelta(t, 24.0, s.MonthlyCost, 0.001, "two active seats, the suspended one is priced separately")
	require.NotNil(t, s.MonthlyCostMinor)
	assert.Equal(t, int64(2400), *s.MonthlyCostMinor)
	assert.InDelta(t, 12.0, s.SuspendedExposure, 0.001)
	require.NotNil(t, s.SuspendedExposureMinor)
	assert.Equal(t, int64(1200), *s.SuspendedExposureMinor)
	assert.InDelta(t, 12.0, s.MonthlyWaste, 0.001, "a connector-known charged suspended seat is confirmed waste")
	require.NotNil(t, s.MonthlyWasteMinor)
	assert.Equal(t, int64(1200), *s.MonthlyWasteMinor)
}

// An unpriced provider must still report counts, and must report zero money
// rather than inventing a number.
func TestScanUnpricedReportsCountsWithoutMoney(t *testing.T) {
	s := Scan(ScanInput{
		Provider: "figma",
		Users: []User{
			{Email: "gone@co.com", Status: "suspended"},
		},
		Domain: "co.com",
		Now:    scanNow,
	})
	f := findingOf(t, s, FindingSuspendedAccounts)
	require.NotNil(t, f)
	assert.Equal(t, 1, f.Count)
	assert.Zero(t, s.MonthlyCost)
	assert.Zero(t, s.MonthlyWaste)
}

func TestScanFindingsSortedBySeverity(t *testing.T) {
	s := Scan(ScanInput{
		Provider: "figma",
		Users: []User{
			{Email: "gone@co.com", Status: "suspended"},
			{Email: "ext@agency.com", Status: "active"},
		},
		Domain: "co.com",
		Now:    scanNow,
	})

	require.GreaterOrEqual(t, len(s.Findings), 2)
	for i := 1; i < len(s.Findings); i++ {
		assert.LessOrEqual(t,
			severityRank[s.Findings[i-1].Severity],
			severityRank[s.Findings[i].Severity],
			"findings must be ordered most severe first")
	}
}

func TestScanCapsSubjects(t *testing.T) {
	var users []User
	for i := 0; i < maxSubjects+10; i++ {
		users = append(users, User{Email: "u" + strconv.Itoa(i) + "@co.com", Status: "suspended"})
	}
	s := Scan(ScanInput{Provider: "figma", Users: users, Domain: "co.com", Now: scanNow})

	f := findingOf(t, s, FindingSuspendedAccounts)
	require.NotNil(t, f)
	assert.Equal(t, maxSubjects+10, f.Count, "the count must reflect reality")
	assert.Len(t, f.Subjects, maxSubjects, "only the display list is capped")
}

func TestScanEmptyProvider(t *testing.T) {
	s := Scan(ScanInput{Provider: "figma", Domain: "co.com", CostPerSeat: 10, Now: scanNow})
	assert.Zero(t, s.Total)
	assert.Zero(t, s.MonthlyCost)
	assert.Zero(t, s.MonthlyWaste)
	assert.Nil(t, findingOf(t, s, FindingAdminSprawl), "no seats cannot be sprawl")
}

func TestScanIgnoresManualPricing(t *testing.T) {
	users := []User{
		{Email: "a@co.com", Status: StatusActive},
		{Email: "b@co.com", Status: StatusActive},
		{Email: "gone@co.com", Status: StatusSuspended},
		{Email: "left@co.com", Status: StatusSuspended},
	}

	s := Scan(ScanInput{
		Provider: "linear", Users: users, Domain: "co.com",
		CostPerSeat: 10, MonthlySpend: 32, SuspendedBilling: SuspendedBillingCharged, Now: scanNow,
	})
	assert.Empty(t, s.RateSource)
	assert.Zero(t, s.CostPerSeat)
	assert.Nil(t, s.CostPerSeatMinor)
	assert.Zero(t, s.MonthlyCost)
	assert.Zero(t, s.SuspendedExposure)

	idle := Scan(ScanInput{
		Provider: "linear",
		Users: []User{
			{Email: "a@co.com", Status: StatusActive, LastActivityAt: daysAgo(2)},
			{Email: "idle@co.com", Status: StatusActive, LastActivityAt: daysAgo(300)},
		},
		Domain: "co.com", MonthlySpend: 32,
		ReportsActivity: true, InactiveThreshold: 60 * 24 * time.Hour, Now: scanNow,
	})
	f := findingOf(t, idle, FindingInactive)
	require.NotNil(t, f)
	assert.Zero(t, f.MonthlyWaste)
	assert.Nil(t, f.MonthlyWasteMinor)
}

func TestScanUsesOnlyProviderBillingForMoney(t *testing.T) {
	users := []User{
		{Email: "a@co.com", Status: StatusActive},
		{Email: "b@co.com", Status: StatusActive},
	}
	renewal := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	sub := &Billing{
		Plan: "business", BilledSeats: IntPtr(2), CostPerSeatMinor: Int64Ptr(1400),
		Source: BillingSourceAPIInvoice, Confidence: BillingConfidenceExact, NextBillingAt: &renewal,
	}

	t.Run("provider API supplies the rate", func(t *testing.T) {
		s := Scan(ScanInput{Provider: "linear", Users: users, Domain: "co.com", Billing: sub, Now: scanNow})
		assert.InDelta(t, 14.0, s.CostPerSeat, 0.001)
		require.NotNil(t, s.CostPerSeatMinor)
		assert.Equal(t, int64(1400), *s.CostPerSeatMinor)
		assert.Equal(t, BillingSourceAPIInvoice, s.RateSource)
		assert.InDelta(t, 28.0, s.MonthlyCost, 0.001)
		assert.Equal(t, "business", s.Plan)
		assert.Equal(t, 2, s.BilledSeats)
		require.NotNil(t, s.NextBillingAt)
	})

	t.Run("manual invoice is ignored", func(t *testing.T) {
		s := Scan(ScanInput{Provider: "linear", Users: users, Domain: "co.com",
			Billing: sub, MonthlySpend: 40, Now: scanNow})
		assert.InDelta(t, 14.0, s.CostPerSeat, 0.001)
		assert.Equal(t, BillingSourceAPIInvoice, s.RateSource)
	})

	t.Run("manual unit price is ignored", func(t *testing.T) {
		s := Scan(ScanInput{Provider: "linear", Users: users, Domain: "co.com",
			Billing: sub, MonthlySpend: 40, CostPerSeat: 9, Now: scanNow})
		assert.InDelta(t, 14.0, s.CostPerSeat, 0.001)
		assert.Equal(t, BillingSourceAPIInvoice, s.RateSource)
	})

	t.Run("plan metadata survives even when the API knows no price", func(t *testing.T) {
		s := Scan(ScanInput{Provider: "linear", Users: users, Domain: "co.com",
			Billing: &Billing{Plan: "free", BilledSeats: IntPtr(0), Source: BillingSourceAPISeatCount}, Now: scanNow})
		assert.Zero(t, s.CostPerSeat)
		assert.Equal(t, "free", s.Plan)
	})
}

// Purchased seats that nobody occupies are the most expensive thing a scan can
// find, and they are invisible from the user list — only the subscription
// knows how many were bought.
func TestScanFlagsUnusedPurchasedSeats(t *testing.T) {
	users := []User{
		{Email: "a@co.com", Status: StatusActive},
		{Email: "b@co.com", Status: StatusActive},
	}

	t.Run("gap uses the vendor's own filled count when reported", func(t *testing.T) {
		// GitHub counts outside collaborators and pending invitations as
		// filled; they are billed but never appear in the member list, so our
		// own tally would overstate the gap.
		s := Scan(ScanInput{
			Provider: "github", Users: users, Domain: "co.com",
			Billing: &Billing{
				Plan: "enterprise", BilledSeats: IntPtr(80), FilledSeats: IntPtr(41),
				CostPerSeatMinor: Int64Ptr(2100), Source: BillingSourceAPIInvoice,
			},
			Now: scanNow,
		})
		f := findingOf(t, s, FindingOverProvisioned)
		require.NotNil(t, f)
		assert.Equal(t, 39, f.Count, "80 purchased minus 41 filled, not minus our 2 listed")
		assert.Equal(t, SeverityHigh, f.Severity)
		assert.InDelta(t, 39*21.0, f.MonthlyWaste, 0.001)
		require.NotNil(t, f.MonthlyWasteMinor)
		assert.Equal(t, int64(39*2100), *f.MonthlyWasteMinor)
		// Cost is what is purchased, not what is used.
		assert.InDelta(t, 80*21.0, s.MonthlyCost, 0.001)
	})

	t.Run("falls back to active seats when the vendor reports no filled count", func(t *testing.T) {
		s := Scan(ScanInput{
			Provider: "acme", Users: users, Domain: "co.com",
			Billing: &Billing{BilledSeats: IntPtr(5), Source: BillingSourceAPISeatCount},
			Now:     scanNow,
		})
		f := findingOf(t, s, FindingOverProvisioned)
		require.NotNil(t, f)
		assert.Equal(t, 3, f.Count)
		assert.Zero(t, f.MonthlyWaste)
		assert.Nil(t, f.MonthlyWasteMinor)
	})

	t.Run("no finding when every purchased seat is taken", func(t *testing.T) {
		s := Scan(ScanInput{
			Provider: "linear", Users: users, Domain: "co.com",
			Billing: &Billing{BilledSeats: IntPtr(2), FilledSeats: IntPtr(2), CostPerSeatMinor: Int64Ptr(1400)},
			Now:     scanNow,
		})
		assert.Nil(t, findingOf(t, s, FindingOverProvisioned))
		assert.InDelta(t, 28.0, s.MonthlyCost, 0.001)
	})

	t.Run("more filled than purchased is not negative waste", func(t *testing.T) {
		s := Scan(ScanInput{
			Provider: "acme", Users: users, Domain: "co.com",
			Billing: &Billing{BilledSeats: IntPtr(2), FilledSeats: IntPtr(5), CostPerSeatMinor: Int64Ptr(1000)},
			Now:     scanNow,
		})
		assert.Nil(t, findingOf(t, s, FindingOverProvisioned))
		assert.Zero(t, s.MonthlyWaste)
	})
}
