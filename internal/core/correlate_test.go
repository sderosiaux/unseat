package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindOffboardingGaps(t *testing.T) {
	// The real shape of the problem: someone left, one tool was cleaned up and
	// another was not. Neither vendor can see this on its own.
	seats := SeatsByProvider{
		"linear": {
			{Email: "gone@co.com", Status: StatusSuspended},
			{Email: "alice@co.com", Status: StatusActive},
		},
		"hubspot": {
			{Email: "gone@co.com", Status: StatusActive},
			{Email: "alice@co.com", Status: StatusActive},
		},
		"github": {
			{Email: "gone@co.com", Status: StatusActive},
		},
	}

	gaps := FindOffboardingGaps(seats)
	require.Len(t, gaps, 1, "alice is active everywhere and is not a gap")

	g := gaps[0]
	assert.Equal(t, "gone@co.com", g.Email)
	assert.Equal(t, []string{"linear"}, g.DeactivatedIn)
	assert.Equal(t, []string{"github", "hubspot"}, g.StillActiveIn)
}

func TestFindOffboardingGapsIgnoresConsistentState(t *testing.T) {
	t.Run("suspended everywhere is a finished offboarding", func(t *testing.T) {
		gaps := FindOffboardingGaps(SeatsByProvider{
			"linear":  {{Email: "gone@co.com", Status: StatusSuspended}},
			"hubspot": {{Email: "gone@co.com", Status: StatusSuspended}},
		})
		assert.Empty(t, gaps)
	})

	t.Run("absent from a provider is not a gap", func(t *testing.T) {
		// Never having had an account is not the same as having one revoked.
		gaps := FindOffboardingGaps(SeatsByProvider{
			"linear":  {{Email: "alice@co.com", Status: StatusActive}},
			"hubspot": {},
		})
		assert.Empty(t, gaps)
	})
}

// Usernames cannot be correlated: "jdoe" in two tenants may be two people, and
// naming the wrong person in an offboarding report is worse than saying nothing.
func TestFindOffboardingGapsSkipsUsernames(t *testing.T) {
	gaps := FindOffboardingGaps(SeatsByProvider{
		"github": {{Email: "jdoe", Status: StatusSuspended}},
		"linear": {{Email: "jdoe", Status: StatusActive}},
	})
	assert.Empty(t, gaps)
}

func TestFindOffboardingGapsIsCaseInsensitive(t *testing.T) {
	gaps := FindOffboardingGaps(SeatsByProvider{
		"linear":  {{Email: "Gone@Co.com", Status: StatusSuspended}},
		"hubspot": {{Email: "gone@co.com", Status: StatusActive}},
	})
	require.Len(t, gaps, 1)
	assert.Equal(t, "gone@co.com", gaps[0].Email)
}

// Most exposed first: the more places still granting access, the more urgent.
func TestFindOffboardingGapsOrderedByExposure(t *testing.T) {
	gaps := FindOffboardingGaps(SeatsByProvider{
		"linear":  {{Email: "one@co.com", Status: StatusSuspended}, {Email: "many@co.com", Status: StatusSuspended}},
		"hubspot": {{Email: "one@co.com", Status: StatusActive}, {Email: "many@co.com", Status: StatusActive}},
		"github":  {{Email: "many@co.com", Status: StatusActive}},
	})
	require.Len(t, gaps, 2)
	assert.Equal(t, "many@co.com", gaps[0].Email)
	assert.Len(t, gaps[0].StillActiveIn, 2)
	assert.Equal(t, "one@co.com", gaps[1].Email)
}

func TestFindOffboardingGapsEmptyInput(t *testing.T) {
	assert.Empty(t, FindOffboardingGaps(nil))
	assert.Empty(t, FindOffboardingGaps(SeatsByProvider{"linear": nil}))
}
