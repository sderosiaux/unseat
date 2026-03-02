package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/store"
	"github.com/stretchr/testify/require"
)

func TestNewMCPServer(t *testing.T) {
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer db.Close()

	cfg := &config.Config{}
	srv := New(db, cfg)
	require.NotNil(t, srv)
	require.NotNil(t, srv.server)
}

func TestHandleListProviders(t *testing.T) {
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	require.NoError(t, db.UpdateSyncState(ctx, "slack", 5))

	srv := New(db, &config.Config{})
	_, out, err := srv.handleListProviders(ctx, nil, emptyInput{})
	require.NoError(t, err)
	require.Len(t, out.Providers, 1)
	require.Equal(t, "slack", out.Providers[0].Provider)
	require.Equal(t, 5, out.Providers[0].UserCount)
}

func TestHandleProviderUsers(t *testing.T) {
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	require.NoError(t, db.UpsertProviderUsers(ctx, "github", []core.User{
		{Email: "alice@test.com", DisplayName: "Alice", Role: "admin", Status: "active"},
		{Email: "bob@test.com", DisplayName: "Bob", Role: "member", Status: "active"},
	}))

	srv := New(db, &config.Config{})
	_, out, err := srv.handleProviderUsers(ctx, nil, providerInput{Provider: "github"})
	require.NoError(t, err)
	require.Len(t, out.Users, 2)
	require.Equal(t, "alice@test.com", out.Users[0].Email)
}

func TestHandleListOrphans(t *testing.T) {
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	require.NoError(t, db.UpdateSyncState(ctx, "slack", 3))
	require.NoError(t, db.InsertPendingRemoval(ctx, "slack", "gone@test.com", time.Now().Add(72*time.Hour)))

	srv := New(db, &config.Config{})
	_, out, err := srv.handleListOrphans(ctx, nil, emptyInput{})
	require.NoError(t, err)
	require.Len(t, out.Orphans, 1)
	require.Equal(t, "gone@test.com", out.Orphans[0].Email)
	require.Equal(t, "slack", out.Orphans[0].Provider)
}

func TestHandleListEvents(t *testing.T) {
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	require.NoError(t, db.InsertEvent(ctx, core.Event{
		Type: core.EventUserAdded, Provider: "github", Email: "new@test.com", Trigger: "cron", OccurredAt: time.Now(),
	}))

	srv := New(db, &config.Config{})

	// Default limit
	_, out, err := srv.handleListEvents(ctx, nil, eventsInput{})
	require.NoError(t, err)
	require.Len(t, out.Events, 1)

	// Explicit limit
	_, out, err = srv.handleListEvents(ctx, nil, eventsInput{Limit: 10})
	require.NoError(t, err)
	require.Len(t, out.Events, 1)
}

func TestHandleGetMappings(t *testing.T) {
	cfg := &config.Config{
		Mappings: []config.Mapping{
			{Group: "engineering", Providers: []config.ProviderMapping{
				{Name: "github", Role: "member"},
				{Name: "slack", Role: "member"},
			}},
		},
	}

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer db.Close()

	srv := New(db, cfg)
	_, out, err := srv.handleGetMappings(context.Background(), nil, emptyInput{})
	require.NoError(t, err)
	require.Len(t, out.Mappings, 1)
	require.Equal(t, "engineering", out.Mappings[0].Group)
	require.Len(t, out.Mappings[0].Providers, 2)
}

func TestHandleGetMappingsEmpty(t *testing.T) {
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer db.Close()

	srv := New(db, &config.Config{})
	_, out, err := srv.handleGetMappings(context.Background(), nil, emptyInput{})
	require.NoError(t, err)
	require.NotNil(t, out.Mappings)
	require.Empty(t, out.Mappings)
}
