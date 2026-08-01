package core

import (
	"regexp"
	"strconv"
	"time"
)

// BillingSource records how a price was established. It is displayed with the
// figure, because a rate read from a billing API and one typed by hand carry
// very different confidence and a budget conversation depends on knowing which
// is which.
type BillingSource string

const (
	// BillingSourceAPI: the vendor's API stated the amount outright.
	BillingSourceAPI BillingSource = "api"
	// BillingSourcePlan: inferred from a plan identifier the API returned,
	// e.g. Linear's "business_yearly_14". Automatic, but an inference.
	BillingSourcePlan BillingSource = "plan"
	// BillingSourceInvoice: derived from a monthly_spend the operator entered.
	BillingSourceInvoice BillingSource = "invoice"
	// BillingSourceConfig: a cost_per_seat the operator stated directly.
	BillingSourceConfig BillingSource = "config"
)

// Billing is what a provider's API can tell us about its own subscription.
// Every field is optional: connectors report what they can and leave the rest.
type Billing struct {
	// Plan is the vendor's own identifier for the tier, e.g. "business_yearly_14".
	Plan string `json:"plan,omitempty"`
	// BilledSeats is how many seats the vendor says it is charging for, which
	// is authoritative and may differ from what ListUsers returns.
	BilledSeats int `json:"billed_seats,omitempty"`
	// FilledSeats is how many of those the vendor considers occupied. It is
	// authoritative where ListUsers is not: GitHub counts outside
	// collaborators and pending invitations as filled, and they are billed
	// even though they never appear in the member list.
	FilledSeats int `json:"filled_seats,omitempty"`
	// CostPerSeat is the monthly per-seat amount, when known or inferable.
	CostPerSeat float64 `json:"cost_per_seat,omitempty"`
	// Currency as reported by the vendor; empty when unknown.
	Currency string `json:"currency,omitempty"`
	// Source records how CostPerSeat was established.
	Source BillingSource `json:"source,omitempty"`
	// NextBillingAt is the next renewal date, when the API exposes it.
	NextBillingAt *time.Time `json:"next_billing_at,omitempty"`
}

// planPriceSuffix matches a trailing amount in a plan identifier, as used by
// Linear ("business_yearly_14", "standard_monthly_10").
var planPriceSuffix = regexp.MustCompile(`_(\d+(?:\.\d+)?)$`)

// PriceFromPlanIdentifier extracts the per-seat amount encoded at the end of a
// plan identifier.
//
// This is an inference, not a documented contract: vendors are free to put
// anything there. Callers must mark the result BillingSourcePlan so it is
// never mistaken for a figure the vendor actually stated.
func PriceFromPlanIdentifier(plan string) (float64, bool) {
	m := planPriceSuffix.FindStringSubmatch(plan)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}
