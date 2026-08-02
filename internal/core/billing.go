package core

import "time"

// BillingSource records which provider API surface produced the billing facts.
//
// Manual pricing is deliberately absent. If a vendor will not state a price or
// billed-seat count through an API, unseat reports the quantity it can prove and
// leaves the money unknown rather than asking the operator to type a number.
type BillingSource string

const (
	// BillingSourceAPIInvoice: invoice or billing line items from the vendor.
	BillingSourceAPIInvoice BillingSource = "api_invoice"
	// BillingSourceAPISubscription: subscription metadata from the vendor.
	BillingSourceAPISubscription BillingSource = "api_subscription"
	// BillingSourceAPIBillingPortal: billing portal API data from the vendor.
	BillingSourceAPIBillingPortal BillingSource = "api_billing_portal"
	// BillingSourceAPISeatCount: the API exposed seat counts but no money.
	BillingSourceAPISeatCount BillingSource = "api_seat_count"
	// BillingSourceUnavailable: no billing API was available or authorized.
	BillingSourceUnavailable BillingSource = "unavailable"
)

// BillingConfidence describes how complete the billing snapshot is.
type BillingConfidence string

const (
	// BillingConfidenceExact means the vendor API stated money and seat counts
	// directly enough to put the figure in front of Finance.
	BillingConfidenceExact BillingConfidence = "exact"
	// BillingConfidencePartial means the vendor reported useful billing facts,
	// but not enough to compute spend.
	BillingConfidencePartial BillingConfidence = "partial"
	// BillingConfidenceUnavailable means no billing facts could be read.
	BillingConfidenceUnavailable BillingConfidence = "unavailable"
)

// BillingLine is one vendor-reported invoice/subscription line.
type BillingLine struct {
	ID          string     `json:"id,omitempty"`
	Description string     `json:"description,omitempty"`
	Quantity    *int       `json:"quantity,omitempty"`
	AmountMinor *int64     `json:"amount_minor,omitempty"`
	Currency    string     `json:"currency,omitempty"`
	PeriodStart *time.Time `json:"period_start,omitempty"`
	PeriodEnd   *time.Time `json:"period_end,omitempty"`
}

// BillingSnapshot is what a provider's API can tell us about its own
// subscription at one point in time.
//
// Money is stored in minor units (cents, pence, etc.) to avoid float drift in
// reports. Every field is optional: connectors report what their API exposes
// and leave the rest nil.
type BillingSnapshot struct {
	Provider  string    `json:"provider,omitempty"`
	AccountID string    `json:"account_id,omitempty"`
	FetchedAt time.Time `json:"fetched_at,omitempty"`
	// Plan is the vendor's own identifier for the tier, e.g. "business_yearly_14".
	Plan string `json:"plan,omitempty"`
	// BilledSeats is how many seats the vendor says it is charging for, which
	// is authoritative and may differ from what ListUsers returns.
	BilledSeats *int `json:"billed_seats,omitempty"`
	// FilledSeats is how many of those the vendor considers occupied. It is
	// authoritative where ListUsers is not: GitHub counts outside
	// collaborators and pending invitations as filled, and they are billed
	// even though they never appear in the member list.
	FilledSeats *int `json:"filled_seats,omitempty"`
	// MonthlyAmountMinor is the monthly amount the vendor stated for the seat
	// pool, when the API exposes one.
	MonthlyAmountMinor *int64 `json:"monthly_amount_minor,omitempty"`
	// CostPerSeatMinor is a vendor-stated monthly unit price. Do not infer this
	// from public price pages or plan names.
	CostPerSeatMinor *int64 `json:"cost_per_seat_minor,omitempty"`
	// Currency as reported by the vendor; empty when unknown.
	Currency  string        `json:"currency,omitempty"`
	LineItems []BillingLine `json:"line_items,omitempty"`
	// Source records which provider API surface produced this snapshot.
	Source BillingSource `json:"source,omitempty"`
	// Confidence tells callers whether money is exact, partial, or unavailable.
	Confidence BillingConfidence `json:"confidence,omitempty"`
	// UnavailableReason is set when the provider cannot expose money or billing
	// facts with the current plan/scopes.
	UnavailableReason string     `json:"unavailable_reason,omitempty"`
	PeriodStart       *time.Time `json:"period_start,omitempty"`
	PeriodEnd         *time.Time `json:"period_end,omitempty"`
	// NextBillingAt is the next renewal date, when the API exposes it.
	NextBillingAt *time.Time `json:"next_billing_at,omitempty"`
}

// Billing is kept as an alias during the migration from the old API. New code
// should use BillingSnapshot.
type Billing = BillingSnapshot

// IntPtr returns a pointer to n. It keeps provider billing constructors clear
// without repeating tiny local helpers.
func IntPtr(n int) *int { return &n }

// Int64Ptr returns a pointer to n.
func Int64Ptr(n int64) *int64 { return &n }

func ptrIntValue(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// BilledSeatCount returns the vendor-stated purchased seat count, or zero.
func (b *BillingSnapshot) BilledSeatCount() int {
	if b == nil {
		return 0
	}
	return ptrIntValue(b.BilledSeats)
}

// FilledSeatCount returns the vendor-stated occupied seat count, or zero.
func (b *BillingSnapshot) FilledSeatCount() int {
	if b == nil {
		return 0
	}
	return ptrIntValue(b.FilledSeats)
}

// HasMoney reports whether this snapshot carries vendor-stated money.
func (b *BillingSnapshot) HasMoney() bool {
	return b != nil && (b.MonthlyAmountMinor != nil || b.CostPerSeatMinor != nil)
}
