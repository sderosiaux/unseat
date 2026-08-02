package store

import (
	"context"
	"testing"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	s, err := NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	return s
}

func TestUpsertAndGetProviderUsers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	users := []core.User{
		{Email: "alice@co.com", DisplayName: "Alice", Role: "editor", Status: "active"},
		{Email: "bob@co.com", DisplayName: "Bob", Role: "viewer", Status: "active"},
	}
	require.NoError(t, s.UpsertProviderUsers(ctx, "figma", users))

	got, err := s.GetProviderUsers(ctx, "figma")
	require.NoError(t, err)
	assert.Len(t, got, 2)

	// Upsert again with changes
	users[0].Role = "admin"
	require.NoError(t, s.UpsertProviderUsers(ctx, "figma", users))

	got, err = s.GetProviderUsers(ctx, "figma")
	require.NoError(t, err)
	assert.Equal(t, "admin", got[0].Role)
}

func TestInsertAndListEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	event := core.Event{
		Type:       core.EventUserAdded,
		Provider:   "linear",
		Email:      "alice@co.com",
		Trigger:    "cron",
		OccurredAt: time.Now(),
	}
	require.NoError(t, s.InsertEvent(ctx, event))

	events, err := s.ListEvents(ctx, EventFilter{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, core.EventUserAdded, events[0].Type)
}

func TestPendingRemovals(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	expires := time.Now().Add(72 * time.Hour)
	require.NoError(t, s.InsertPendingRemoval(ctx, "figma", "old@co.com", expires))

	removals, err := s.GetPendingRemovals(ctx, "figma")
	require.NoError(t, err)
	assert.Len(t, removals, 1)
	assert.Equal(t, "old@co.com", removals[0].Email)

	// Cancel it
	require.NoError(t, s.CancelPendingRemoval(ctx, "figma", "old@co.com"))
	removals, err = s.GetPendingRemovals(ctx, "figma")
	require.NoError(t, err)
	assert.Len(t, removals, 0) // cancelled ones excluded

	// No expired removals (expires in 72h)
	expired, err := s.GetExpiredRemovals(ctx, "figma")
	require.NoError(t, err)
	assert.Len(t, expired, 0)
}

// A repeated sync must not push the deadline forward, otherwise a grace period
// shorter than the sync interval can never elapse.
func TestPendingRemovalKeepsOriginalDeadline(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	past := time.Now().Add(-1 * time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, s.InsertPendingRemoval(ctx, "figma", "old@co.com", past))

	// A later sync re-detects the same orphan and proposes a fresh deadline.
	require.NoError(t, s.InsertPendingRemoval(ctx, "figma", "old@co.com", time.Now().Add(72*time.Hour)))

	expired, err := s.GetExpiredRemovals(ctx, "figma")
	require.NoError(t, err)
	require.Len(t, expired, 1, "the original deadline has passed, so the seat is due")
	assert.Equal(t, past.Unix(), expired[0].ExpiresAt.Unix())
}

// Cancelling means the user came back; leaving again must restart the clock.
func TestPendingRemovalRestartsAfterCancel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	past := time.Now().Add(-1 * time.Hour)
	require.NoError(t, s.InsertPendingRemoval(ctx, "figma", "old@co.com", past))
	require.NoError(t, s.CancelPendingRemoval(ctx, "figma", "old@co.com"))

	future := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, s.InsertPendingRemoval(ctx, "figma", "old@co.com", future))

	expired, err := s.GetExpiredRemovals(ctx, "figma")
	require.NoError(t, err)
	assert.Empty(t, expired, "a re-opened countdown uses the new deadline")

	pending, err := s.GetPendingRemovals(ctx, "figma")
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, future.Unix(), pending[0].ExpiresAt.Unix())
}

func TestGetExpiredRemovalsScopedToProvider(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	past := time.Now().Add(-1 * time.Hour)
	require.NoError(t, s.InsertPendingRemoval(ctx, "figma", "old@co.com", past))
	require.NoError(t, s.InsertPendingRemoval(ctx, "linear", "other@co.com", past))

	expired, err := s.GetExpiredRemovals(ctx, "figma")
	require.NoError(t, err)
	require.Len(t, expired, 1)
	assert.Equal(t, "old@co.com", expired[0].Email)
}

func TestUpsertAndGetProviderUsers_WithLastActivity(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	users := []core.User{
		{Email: "active@co.com", DisplayName: "Active", Role: "member", Status: "active", LastActivityAt: &now},
		{Email: "inactive@co.com", DisplayName: "Inactive", Role: "member", Status: "active"},
	}
	require.NoError(t, s.UpsertProviderUsers(ctx, "linear", users))

	got, err := s.GetProviderUsers(ctx, "linear")
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.NotNil(t, got[0].LastActivityAt)
	assert.Equal(t, now.Unix(), got[0].LastActivityAt.Unix())
	assert.Nil(t, got[1].LastActivityAt)
}

func TestGetInactiveUsers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	recent := time.Now().UTC().Truncate(time.Second)
	old := time.Now().AddDate(0, 0, -60).UTC().Truncate(time.Second)

	users := []core.User{
		{Email: "recent@co.com", DisplayName: "Recent", Role: "member", Status: "active", LastActivityAt: &recent},
		{Email: "old@co.com", DisplayName: "Old", Role: "member", Status: "active", LastActivityAt: &old},
		{Email: "never@co.com", DisplayName: "Never", Role: "member", Status: "active"},
	}
	require.NoError(t, s.UpsertProviderUsers(ctx, "linear", users))

	since := time.Now().AddDate(0, 0, -30)
	inactive, err := s.GetInactiveUsers(ctx, since, []string{"linear"})
	require.NoError(t, err)
	assert.Len(t, inactive, 2) // old + never

	// "never" should be first (NULL sorts first), then "old"
	assert.Equal(t, "never@co.com", inactive[0].Email)
	assert.Nil(t, inactive[0].LastActivityAt)
	assert.Equal(t, "old@co.com", inactive[1].Email)
	assert.NotNil(t, inactive[1].LastActivityAt)
}

func TestGetInactiveUsersScopedToProviders(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	old := time.Now().AddDate(0, 0, -60).UTC().Truncate(time.Second)

	require.NoError(t, s.UpsertProviderUsers(ctx, "linear", []core.User{
		{Email: "old@co.com", Status: "active", LastActivityAt: &old},
	}))
	// figma exposes no activity data: a null last_activity_at means "unknown",
	// and must never be reported as inactivity.
	require.NoError(t, s.UpsertProviderUsers(ctx, "figma", []core.User{
		{Email: "unknown@co.com", Status: "active"},
	}))

	since := time.Now().AddDate(0, 0, -30)

	inactive, err := s.GetInactiveUsers(ctx, since, []string{"linear"})
	require.NoError(t, err)
	require.Len(t, inactive, 1)
	assert.Equal(t, "old@co.com", inactive[0].Email)

	// No instrumented provider at all: the honest answer is nothing, not
	// "every user of every provider is inactive".
	none, err := s.GetInactiveUsers(ctx, since, nil)
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestSyncState(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpdateSyncState(ctx, "linear", 42, true))

	state, err := s.GetSyncState(ctx, "linear")
	require.NoError(t, err)
	assert.Equal(t, 42, state.UserCount)
	assert.Equal(t, "ok", state.Status)

	states, err := s.ListSyncStates(ctx)
	require.NoError(t, err)
	assert.Len(t, states, 1)
}

func TestBillingSnapshotsRoundTripLatestPerProvider(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	first := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	second := first.Add(time.Hour)
	periodStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	require.NoError(t, s.InsertBillingSnapshot(ctx, core.BillingSnapshot{
		Provider:          "github",
		FetchedAt:         first,
		Plan:              "enterprise",
		BilledSeats:       core.IntPtr(80),
		FilledSeats:       core.IntPtr(41),
		Source:            core.BillingSourceAPISeatCount,
		Confidence:        core.BillingConfidencePartial,
		UnavailableReason: "GitHub reports seats but not contracted price",
	}))
	require.NoError(t, s.InsertBillingSnapshot(ctx, core.BillingSnapshot{
		Provider:           "github",
		AccountID:          "org/acme",
		FetchedAt:          second,
		Plan:               "enterprise",
		BilledSeats:        core.IntPtr(80),
		FilledSeats:        core.IntPtr(40),
		CostPerSeatMinor:   core.Int64Ptr(2100),
		MonthlyAmountMinor: core.Int64Ptr(168000),
		Currency:           "USD",
		Source:             core.BillingSourceAPIInvoice,
		Confidence:         core.BillingConfidenceExact,
		PeriodStart:        &periodStart,
		PeriodEnd:          &periodEnd,
		LineItems: []core.BillingLine{{
			ID:          "line_1",
			Description: "GitHub Enterprise seats",
			Quantity:    core.IntPtr(80),
			AmountMinor: core.Int64Ptr(168000),
			Currency:    "USD",
			PeriodStart: &periodStart,
			PeriodEnd:   &periodEnd,
		}},
	}))
	require.NoError(t, s.InsertBillingSnapshot(ctx, core.BillingSnapshot{
		Provider:          "figma",
		FetchedAt:         first,
		Source:            core.BillingSourceUnavailable,
		Confidence:        core.BillingConfidenceUnavailable,
		UnavailableReason: "provider connector does not expose billing API data yet",
	}))

	got, err := s.LatestBillingSnapshot(ctx, "github")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "org/acme", got.AccountID)
	assert.Equal(t, int64(168000), *got.MonthlyAmountMinor)
	assert.Equal(t, int64(2100), *got.CostPerSeatMinor)
	assert.Equal(t, 80, got.BilledSeatCount())
	require.Len(t, got.LineItems, 1)
	assert.Equal(t, "line_1", got.LineItems[0].ID)
	assert.Equal(t, int64(168000), *got.LineItems[0].AmountMinor)

	all, err := s.ListLatestBillingSnapshots(ctx)
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, "figma", all[0].Provider)
	assert.Equal(t, core.BillingConfidenceUnavailable, all[0].Confidence)
	assert.Equal(t, "github", all[1].Provider)
	assert.Equal(t, int64(168000), *all[1].MonthlyAmountMinor)
}

func TestProviderCredentialsRoundTripAndReplace(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	created := time.Now().AddDate(-1, 0, 0).UTC().Truncate(time.Second)
	used := time.Now().AddDate(0, 0, -3).UTC().Truncate(time.Second)

	require.NoError(t, s.UpsertProviderCredentials(ctx, "github", []core.ClassifiedCredential{
		{
			Credential: core.Credential{
				Provider:         "ignored",
				Kind:             core.CredentialAppInstallation,
				ID:               "app-1",
				Label:            "Deploy Bot",
				CreatedAt:        &created,
				CreatedBy:        "gone@co.com",
				LastUsedAt:       &used,
				Scopes:           []string{"contents:write", "metadata:read"},
				PrivilegedScopes: []string{"contents:write"},
				Reach:            core.ReachAll,
				Metadata:         map[string]string{"url": "https://github.com/apps/deploy-bot"},
			},
			Class:        core.CredentialOrphaned,
			Reason:       "gone@co.com is not in the directory",
			Overreaching: true,
			ReachReason:  "write or admin access to every resource: contents:write",
		},
	}))

	got, err := s.GetProviderCredentials(ctx, "github")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "github", got[0].Credential.Provider)
	assert.Equal(t, core.CredentialAppInstallation, got[0].Credential.Kind)
	assert.Equal(t, "app-1", got[0].Credential.ID)
	assert.Equal(t, []string{"contents:write", "metadata:read"}, got[0].Credential.Scopes)
	assert.Equal(t, []string{"contents:write"}, got[0].Credential.PrivilegedScopes)
	assert.Equal(t, "https://github.com/apps/deploy-bot", got[0].Credential.Metadata["url"])
	require.NotNil(t, got[0].Credential.CreatedAt)
	require.NotNil(t, got[0].Credential.LastUsedAt)
	assert.Equal(t, created.Unix(), got[0].Credential.CreatedAt.Unix())
	assert.Equal(t, used.Unix(), got[0].Credential.LastUsedAt.Unix())
	assert.True(t, got[0].Overreaching)
	assert.Equal(t, core.CredentialOrphaned, got[0].Class)

	require.NoError(t, s.UpsertProviderCredentials(ctx, "github", []core.ClassifiedCredential{
		{
			Credential: core.Credential{
				Kind:  core.CredentialWebhook,
				ID:    "hook-1",
				Label: "Audit Hook",
			},
			Class:  core.CredentialUnowned,
			Reason: "the provider does not report who authorised this",
		},
	}))

	got, err = s.GetProviderCredentials(ctx, "github")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, core.CredentialWebhook, got[0].Credential.Kind)
	assert.Equal(t, "hook-1", got[0].Credential.ID)
	assert.NotNil(t, got[0].Credential.Scopes)
	assert.NotNil(t, got[0].Credential.PrivilegedScopes)
	assert.NotNil(t, got[0].Credential.Metadata)
}

func TestCredentialSyncStateCanRecordUnsupportedProvider(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpdateCredentialSyncState(ctx, CredentialSyncState{
		Provider:        "figma",
		CredentialCount: 0,
		Status:          "not_supported",
		Message:         "provider API exposes no credential listing",
	}))

	states, err := s.ListCredentialSyncStates(ctx)
	require.NoError(t, err)
	require.Len(t, states, 1)
	assert.Equal(t, "figma", states[0].Provider)
	assert.Equal(t, "not_supported", states[0].Status)
	assert.Equal(t, "provider API exposes no credential listing", states[0].Message)

	credentials, err := s.GetProviderCredentials(ctx, "figma")
	require.NoError(t, err)
	assert.Empty(t, credentials)
}

// A deactivated seat is trivially unused. Reporting it as inactivity
// double-counts the same seat and buries the live, billed, idle ones that are
// the only actionable result.
func TestGetInactiveUsersExcludesSuspended(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	old := time.Now().AddDate(0, 0, -200).UTC().Truncate(time.Second)
	require.NoError(t, s.UpsertProviderUsers(ctx, "linear", []core.User{
		{Email: "idle@co.com", Status: core.StatusActive, LastActivityAt: &old},
		{Email: "gone@co.com", Status: core.StatusSuspended, LastActivityAt: &old},
		{Email: "never@co.com", Status: core.StatusActive},
	}))

	inactive, err := s.GetInactiveUsers(ctx, time.Now().AddDate(0, 0, -60), []string{"linear"})
	require.NoError(t, err)

	emails := make([]string, len(inactive))
	for i, u := range inactive {
		emails[i] = u.Email
	}
	assert.ElementsMatch(t, []string{"never@co.com", "idle@co.com"}, emails)
	assert.NotContains(t, emails, "gone@co.com")
}
