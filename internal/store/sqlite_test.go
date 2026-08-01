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
	t.Cleanup(func() { s.Close() })
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

	require.NoError(t, s.UpdateSyncState(ctx, "linear", 42))

	state, err := s.GetSyncState(ctx, "linear")
	require.NoError(t, err)
	assert.Equal(t, 42, state.UserCount)
	assert.Equal(t, "ok", state.Status)

	states, err := s.ListSyncStates(ctx)
	require.NoError(t, err)
	assert.Len(t, states, 1)
}
