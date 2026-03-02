package store

import (
	"context"
	"testing"
	"time"

	"github.com/sderosiaux/saas-watcher/internal/core"
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
	expired, err := s.GetExpiredRemovals(ctx)
	require.NoError(t, err)
	assert.Len(t, expired, 0)
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
