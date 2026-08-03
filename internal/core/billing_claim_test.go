package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildBillingClaimsVerifiedSavingFromProviderMoney(t *testing.T) {
	claims := BuildBillingClaims(BillingClaimInput{
		Provider:             "linear",
		Subject:              "alice@co.com",
		ReclaimableSeatCount: 2,
		Billing: &Billing{
			BilledSeats:      IntPtr(10),
			CostPerSeatMinor: Int64Ptr(1400),
			Currency:         "USD",
			Source:           BillingSourceAPIInvoice,
		},
	})

	require.Len(t, claims, 1)
	assert.Equal(t, BillingClaimSavingVerified, claims[0].Type)
	assert.True(t, claims[0].Verified)
	require.NotNil(t, claims[0].AmountMinor)
	assert.Equal(t, int64(2800), *claims[0].AmountMinor)
	assert.True(t, claims[0].UnitPriceKnown)
	assert.True(t, claims[0].BilledSeatKnown)
}

func TestBuildBillingClaimsMoneyUnknownWhenNoProviderPrice(t *testing.T) {
	claims := BuildBillingClaims(BillingClaimInput{
		Provider:             "github-copilot",
		Subject:              "alice@co.com",
		ReclaimableSeatCount: 1,
		Billing: &Billing{
			BilledSeats: IntPtr(10),
			FilledSeats: IntPtr(10),
			Source:      BillingSourceAPISeatCount,
		},
	})

	require.Len(t, claims, 1)
	assert.Equal(t, BillingClaimMoneyUnknown, claims[0].Type)
	assert.True(t, claims[0].BilledSeatKnown)
	assert.True(t, claims[0].ContractUnknown)
	assert.Nil(t, claims[0].AmountMinor)
}

func TestBuildBillingClaimsDoesNotInventMoneyWithoutBilling(t *testing.T) {
	claims := BuildBillingClaims(BillingClaimInput{
		Provider:             "figma",
		Subject:              "alice@co.com",
		ReclaimableSeatCount: 3,
	})

	require.Len(t, claims, 1)
	assert.Equal(t, BillingClaimMoneyUnknown, claims[0].Type)
	assert.Nil(t, claims[0].AmountMinor)
}
