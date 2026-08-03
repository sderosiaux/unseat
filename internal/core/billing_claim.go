package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

type BillingClaimType string

const (
	BillingClaimSavingVerified            BillingClaimType = "saving_verified"
	BillingClaimSavingEstimatedByProvider BillingClaimType = "saving_estimated_by_provider"
	BillingClaimSeatReclaimVerified       BillingClaimType = "seat_reclaim_verified"
	BillingClaimRenewalOpportunity        BillingClaimType = "renewal_opportunity"
	BillingClaimMoneyUnknown              BillingClaimType = "money_unknown"
	BillingClaimProcurementRequired       BillingClaimType = "procurement_required"
)

type BillingClaim struct {
	ID              string           `json:"id"`
	Provider        string           `json:"provider"`
	Type            BillingClaimType `json:"type"`
	Subject         string           `json:"subject,omitempty"`
	SeatCountKnown  bool             `json:"seat_count_known"`
	BilledSeatKnown bool             `json:"billed_seat_count_known"`
	UnitPriceKnown  bool             `json:"unit_price_known"`
	ContractUnknown bool             `json:"contract_unknown"`
	Verified        bool             `json:"verified"`
	AmountMinor     *int64           `json:"amount_minor,omitempty"`
	Currency        string           `json:"currency,omitempty"`
	Reason          string           `json:"reason"`
	Source          BillingSource    `json:"source,omitempty"`
}

type BillingClaimInput struct {
	Provider             string
	Subject              string
	Billing              *Billing
	ReclaimableSeatCount int
}

func BuildBillingClaims(in BillingClaimInput) []BillingClaim {
	if in.Billing == nil {
		return []BillingClaim{newBillingClaim(in.Provider, in.Subject, BillingClaimMoneyUnknown, "provider connector does not expose billing API data", nil, "")}
	}

	b := in.Billing
	if b.HasMoney() && in.ReclaimableSeatCount > 0 {
		amount := reclaimAmountMinor(b, in.ReclaimableSeatCount)
		claim := newBillingClaim(in.Provider, in.Subject, BillingClaimSavingVerified, "provider API exposed money and reclaimable seats", amount, b.Currency)
		claim.Verified = true
		claim.UnitPriceKnown = b.CostPerSeatMinor != nil
		claim.SeatCountKnown = true
		claim.BilledSeatKnown = b.BilledSeats != nil
		claim.Source = b.Source
		return []BillingClaim{claim}
	}

	if b.BilledSeats != nil {
		if b.HasMoney() {
			claim := newBillingClaim(in.Provider, in.Subject, BillingClaimRenewalOpportunity, "provider API exposed billed seats, but no immediate reclaimable subject seat was proven", nil, b.Currency)
			claim.UnitPriceKnown = b.CostPerSeatMinor != nil || b.MonthlyAmountMinor != nil
			claim.SeatCountKnown = true
			claim.BilledSeatKnown = true
			claim.Source = b.Source
			return []BillingClaim{claim}
		}
		claim := newBillingClaim(in.Provider, in.Subject, BillingClaimMoneyUnknown, "provider API exposed seat counts but not contracted price", nil, b.Currency)
		claim.SeatCountKnown = true
		claim.BilledSeatKnown = true
		claim.ContractUnknown = true
		claim.Source = b.Source
		return []BillingClaim{claim}
	}

	reason := b.UnavailableReason
	if reason == "" {
		reason = "provider API did not return billing facts"
	}
	claim := newBillingClaim(in.Provider, in.Subject, BillingClaimProcurementRequired, reason, nil, b.Currency)
	claim.ContractUnknown = true
	claim.Source = b.Source
	return []BillingClaim{claim}
}

func reclaimAmountMinor(b *Billing, seats int) *int64 {
	if b == nil || seats <= 0 {
		return nil
	}
	if b.CostPerSeatMinor != nil {
		amount := *b.CostPerSeatMinor * int64(seats)
		return &amount
	}
	if b.MonthlyAmountMinor != nil && b.BilledSeats != nil && *b.BilledSeats > 0 {
		amount := (*b.MonthlyAmountMinor / int64(*b.BilledSeats)) * int64(seats)
		return &amount
	}
	return nil
}

func newBillingClaim(provider, subject string, typ BillingClaimType, reason string, amount *int64, currency string) BillingClaim {
	key := fmt.Sprintf("%s:%s:%s:%s", provider, subject, typ, reason)
	sum := sha256.Sum256([]byte(key))
	return BillingClaim{
		ID:              "bill_" + hex.EncodeToString(sum[:])[:16],
		Provider:        provider,
		Type:            typ,
		Subject:         subject,
		AmountMinor:     amount,
		Currency:        currency,
		Reason:          reason,
		SeatCountKnown:  false,
		BilledSeatKnown: false,
		UnitPriceKnown:  false,
		ContractUnknown: false,
	}
}
