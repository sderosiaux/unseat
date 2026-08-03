package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveCanonicalIdentityMatchesDirectoryAndExplicitAlias(t *testing.T) {
	id := ResolveCanonicalIdentity(ResolveIdentityInput{
		Subject:   "alice@co.com",
		Directory: []User{{Email: "alice@co.com", Status: StatusSuspended}},
		Aliases: map[string][]string{
			"alice@co.com": {"alice-dev", "alice@gmail.com"},
		},
		ProviderUsers: map[string][]User{
			"github": {{Email: "alice-dev"}},
			"linear": {{Email: "alice@co.com"}},
		},
	})

	assert.Equal(t, "alice@co.com", id.PrimaryEmail)
	assert.Equal(t, StatusSuspended, id.DirectoryStatus)
	assert.Equal(t, IdentityMatched, id.Status)
	assert.Equal(t, IdentityConfidenceStrong, id.Confidence)
	assert.True(t, id.Matches("alice-dev"))
	assert.True(t, id.Matches("alice@gmail.com"))
	require.NotEmpty(t, id.Links)
}

func TestResolveCanonicalIdentityKeepsWeakPersonalEmailAmbiguous(t *testing.T) {
	id := ResolveCanonicalIdentity(ResolveIdentityInput{
		Subject:   "alice@co.com",
		Directory: []User{{Email: "alice@co.com", Status: StatusActive}},
		ProviderUsers: map[string][]User{
			"figma": {{Email: "alice@gmail.com"}},
		},
	})

	assert.Equal(t, IdentityMatched, id.Status)
	assert.Equal(t, IdentityConfidenceStrong, id.Confidence)
	require.Len(t, id.Links, 2)
	assert.True(t, id.Matches("alice@co.com"))
	assert.False(t, id.Matches("alice@gmail.com"))
	assert.True(t, id.AmbiguousMatch("alice@gmail.com"))
}

func TestResolveCanonicalIdentityUnmatched(t *testing.T) {
	id := ResolveCanonicalIdentity(ResolveIdentityInput{
		Subject:   "missing@co.com",
		Directory: []User{{Email: "alice@co.com", Status: StatusActive}},
	})

	assert.Equal(t, IdentityUnmatched, id.Status)
	assert.Equal(t, IdentityConfidenceUnknown, id.Confidence)
	assert.Empty(t, id.Links)
}
