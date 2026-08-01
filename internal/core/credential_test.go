package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func at(s string) *time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return &t
}

func dir() map[string]DirectoryUser {
	return map[string]DirectoryUser{
		"ada@acme.com":  {Email: "ada@acme.com"},
		"gone@acme.com": {Email: "gone@acme.com", Suspended: true},
	}
}

func cred(label, createdBy string) Credential {
	return Credential{
		Provider:  "linear",
		Kind:      CredentialIntegration,
		ID:        label,
		Label:     label,
		CreatedBy: createdBy,
		CreatedAt: at("2024-01-01"),
	}
}

func classify(c ...Credential) []ClassifiedCredential {
	return ClassifyCredentials(ClassifyCredentialsInput{
		Credentials: c, Directory: dir(), Domain: "acme.com",
	})
}

// The whole point of the model: access authorised by someone who has left.
// Google offboarding cannot see it, so nothing else will report it.
func TestClassifyCredentialsOrphanedWhenAuthorHasLeft(t *testing.T) {
	got := classify(cred("slack", "departed@acme.com"), cred("zendesk", "gone@acme.com"))
	require.Len(t, got, 2)

	for _, c := range got {
		assert.Equal(t, CredentialOrphaned, c.Class, c.Credential.Label)
	}
	assert.Contains(t, got[0].Reason, "not in the directory")
	assert.Contains(t, got[1].Reason, "suspended in the directory")
}

func TestClassifyCredentialsLiveWhenAuthorIsCurrent(t *testing.T) {
	got := classify(cred("figma", "ada@acme.com"))
	require.Len(t, got, 1)
	assert.Equal(t, CredentialLive, got[0].Class)
	assert.Contains(t, got[0].Reason, "current employee")
}

// A switched-off credential is settled before its author is considered. It
// cannot act, so the recommendation is to uninstall it — not to find it a new
// owner — and that holds whoever installed it.
func TestClassifyCredentialsDormantOutranksOrphaned(t *testing.T) {
	c := cred("codecov", "departed@acme.com")
	c.Disabled = true
	c.DisabledAt = at("2022-11-25")

	got := classify(c)
	require.Len(t, got, 1)
	assert.Equal(t, CredentialDormant, got[0].Class)
	assert.Contains(t, got[0].Reason, "since 2022-11-25")
	assert.Contains(t, got[0].Reason, "switch it back on")
}

// A disabled date the provider did not give must not surface as 1970.
func TestClassifyCredentialsDormantWithoutADate(t *testing.T) {
	c := cred("mystery", "ada@acme.com")
	c.Disabled = true

	got := classify(c)
	require.Len(t, got, 1)
	assert.Equal(t, CredentialDormant, got[0].Class)
	assert.NotContains(t, got[0].Reason, "1970")
	assert.NotContains(t, got[0].Reason, "since")
}

// No author reported is not the same as no author. Saying "orphaned" here
// would accuse an org of leaving access behind on the strength of a field the
// vendor simply does not expose.
func TestClassifyCredentialsUnownedWhenProviderNamesNobody(t *testing.T) {
	got := classify(cred("anonymous-app", ""))
	require.Len(t, got, 1)
	assert.Equal(t, CredentialUnowned, got[0].Class)
	assert.Contains(t, got[0].Reason, "does not report who")
}

// Degrade to reporting: with no directory, nothing can be called orphaned.
func TestClassifyCredentialsWithoutDirectoryJudgesNobody(t *testing.T) {
	got := ClassifyCredentials(ClassifyCredentialsInput{
		Credentials: []Credential{cred("slack", "departed@acme.com")},
		Directory:   nil,
		Domain:      "acme.com",
	})
	require.Len(t, got, 1)
	assert.Equal(t, CredentialUnowned, got[0].Class)
	assert.Contains(t, got[0].Reason, "directory unavailable")
}

func TestClassifyCredentialsExternalAuthor(t *testing.T) {
	got := classify(cred("contractor-tool", "someone@other.com"))
	require.Len(t, got, 1)
	assert.Equal(t, CredentialExternal, got[0].Class)
}

// Reach is a fact about the grant, not about its author, so it is reported
// alongside the class rather than instead of it.
func TestClassifyCredentialsOverreachIsOrthogonalToClass(t *testing.T) {
	c := cred("vercel", "ada@acme.com")
	c.Reach = ReachAll
	c.PrivilegedScopes = []string{"administration", "contents"}

	got := classify(c)
	require.Len(t, got, 1)
	assert.Equal(t, CredentialLive, got[0].Class, "a current employee installed it, and that is still true")
	assert.True(t, got[0].Overreaching)
	assert.Contains(t, got[0].ReachReason, "administration")
}

// A suspended app with admin over everything is one click from being live
// again, so its reach is still worth stating.
func TestClassifyCredentialsOverreachAssessedWhenDormant(t *testing.T) {
	c := cred("runner", "ada@acme.com")
	c.Disabled = true
	c.Reach = ReachAll
	c.PrivilegedScopes = []string{"administration"}

	got := classify(c)
	require.Len(t, got, 1)
	assert.Equal(t, CredentialDormant, got[0].Class)
	assert.True(t, got[0].Overreaching)
}

// Broad reach without write power is not overreach: a read-only app installed
// everywhere is how most CI works.
func TestClassifyCredentialsBroadButReadOnlyIsNotOverreach(t *testing.T) {
	c := cred("readonly-scanner", "ada@acme.com")
	c.Reach = ReachAll
	c.Scopes = []string{"metadata", "contents:read"}

	got := classify(c)
	require.Len(t, got, 1)
	assert.False(t, got[0].Overreaching)
	assert.Empty(t, got[0].ReachReason)
}

// Selected reach is narrow by construction, whatever the permissions.
func TestClassifyCredentialsSelectedReachIsNotOverreach(t *testing.T) {
	c := cred("scoped", "ada@acme.com")
	c.Reach = ReachSelected
	c.PrivilegedScopes = []string{"contents"}

	got := classify(c)
	require.Len(t, got, 1)
	assert.False(t, got[0].Overreaching)
}

// The provider may report a handle rather than an address; the same alias
// index seat classification uses resolves it.
func TestClassifyCredentialsResolvesAuthorThroughAliases(t *testing.T) {
	got := ClassifyCredentials(ClassifyCredentialsInput{
		Credentials: []Credential{cred("bot", "ada-handle")},
		Directory:   dir(),
		Domain:      "acme.com",
		AliasIndex:  map[string]string{"ada-handle": "ada@acme.com"},
	})
	require.Len(t, got, 1)
	assert.Equal(t, CredentialLive, got[0].Class)
}

// Findings that need a decision come first, and oldest first within a class:
// age is the argument an operator carries into the conversation.
func TestClassifyCredentialsOrdersByUrgencyThenAge(t *testing.T) {
	recentOrphan := cred("recent-orphan", "departed@acme.com")
	recentOrphan.CreatedAt = at("2026-01-01")
	oldOrphan := cred("old-orphan", "departed@acme.com")
	oldOrphan.CreatedAt = at("2021-01-01")
	dormant := cred("dormant", "ada@acme.com")
	dormant.Disabled = true
	live := cred("live", "ada@acme.com")

	got := classify(live, dormant, recentOrphan, oldOrphan)

	labels := make([]string, len(got))
	for i, c := range got {
		labels[i] = c.Credential.Label
	}
	assert.Equal(t, []string{"old-orphan", "recent-orphan", "dormant", "live"}, labels)
}

// A credential with no creation date sorts after dated ones rather than
// jumping to the front as a zero time would.
func TestClassifyCredentialsUndatedSortsLast(t *testing.T) {
	undated := cred("undated", "departed@acme.com")
	undated.CreatedAt = nil
	dated := cred("dated", "departed@acme.com")

	got := classify(undated, dated)
	require.Len(t, got, 2)
	assert.Equal(t, "dated", got[0].Credential.Label)
}

func TestSummarizeCredentials(t *testing.T) {
	over := cred("vercel", "ada@acme.com")
	over.Reach = ReachAll
	over.PrivilegedScopes = []string{"administration"}
	dormant := cred("codecov", "ada@acme.com")
	dormant.Disabled = true

	s := SummarizeCredentials("github", classify(
		cred("orphan", "departed@acme.com"),
		dormant,
		cred("anon", ""),
		cred("ext", "x@other.com"),
		over,
	), false)

	assert.Equal(t, CredentialSummary{
		Provider: "github", Orphaned: 1, Dormant: 1, Unowned: 1, External: 1,
		Live: 1, Overreaching: 1, Total: 5, UsageKnown: false,
	}, s)
}

// Without the capability, no credential may be described as unused however
// old it is — the same rule seats already follow for activity.
func TestSummarizeCredentialsCarriesUsageKnown(t *testing.T) {
	assert.False(t, SummarizeCredentials("github", nil, false).UsageKnown)
	assert.True(t, SummarizeCredentials("anthropic", nil, true).UsageKnown)
}
