package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// verifiedProviders is the exact set confirmed against a live tenant. Any
// change to this set is a claim about production access and must be a
// deliberate edit here, not a side effect of touching providers.go.
var verifiedProviders = []string{"google-directory", "linear", "github", "hubspot"}

func TestVerifiedProviders_ExactSet(t *testing.T) {
	var got []string
	for name, p := range KnownProviders {
		if p.Verification == Verified {
			got = append(got, name)
		}
	}
	assert.ElementsMatch(t, verifiedProviders, got,
		"the set of Verified connectors changed — this requires evidence (a real ListUsers call against a live tenant), not just a code edit")
}

func TestVerifiedProviders_Count(t *testing.T) {
	count := 0
	for _, p := range KnownProviders {
		if p.Verification == Verified {
			count++
		}
	}
	assert.Equal(t, len(verifiedProviders), count)
}

func TestVerification_DefaultsToUnverified(t *testing.T) {
	// Zero value must mean Unverified: any provider entry that never sets
	// Verification explicitly should not silently become Verified.
	var v Verification
	assert.Equal(t, Unverified, v)
}

func TestVerification_String(t *testing.T) {
	assert.Equal(t, "unverified", Unverified.String())
	assert.Equal(t, "verified", Verified.String())
}
