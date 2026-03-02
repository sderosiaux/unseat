package sync

import (
	"context"
	"testing"
	"time"

	"github.com/sderosiaux/saas-watcher/config"
	"github.com/sderosiaux/saas-watcher/internal/core"
	"github.com/sderosiaux/saas-watcher/internal/provider"
	"github.com/sderosiaux/saas-watcher/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Fakes ---

type fakeIdentity struct {
	groups map[string][]core.User
}

func (f *fakeIdentity) Name() string                                     { return "fake-identity" }
func (f *fakeIdentity) ListUsers(_ context.Context) ([]core.User, error) { return nil, nil }
func (f *fakeIdentity) AddUser(_ context.Context, _, _ string) error     { return nil }
func (f *fakeIdentity) RemoveUser(_ context.Context, _ string) error     { return nil }
func (f *fakeIdentity) SetRole(_ context.Context, _, _ string) error     { return nil }
func (f *fakeIdentity) Capabilities() core.Capabilities                  { return core.Capabilities{} }
func (f *fakeIdentity) ListGroups(_ context.Context) ([]core.Group, error) {
	return nil, nil
}
func (f *fakeIdentity) ListGroupMembers(_ context.Context, group string) ([]core.User, error) {
	return f.groups[group], nil
}

type fakeTarget struct {
	name    string
	users   []core.User
	added   []string
	removed []string
	caps    core.Capabilities
}

func (f *fakeTarget) Name() string                                     { return f.name }
func (f *fakeTarget) ListUsers(_ context.Context) ([]core.User, error) { return f.users, nil }
func (f *fakeTarget) AddUser(_ context.Context, email, _ string) error {
	f.added = append(f.added, email)
	return nil
}
func (f *fakeTarget) RemoveUser(_ context.Context, email string) error {
	f.removed = append(f.removed, email)
	return nil
}
func (f *fakeTarget) SetRole(_ context.Context, _, _ string) error { return nil }
func (f *fakeTarget) Capabilities() core.Capabilities              { return f.caps }

// --- Tests ---

func TestReconcilerFullSync(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"design@co.com": {{Email: "alice@co.com"}, {Email: "bob@co.com"}},
		},
	}

	target := &fakeTarget{
		name:  "figma",
		users: []core.User{{Email: "bob@co.com"}, {Email: "charlie@co.com"}},
		caps:  core.Capabilities{CanAdd: true, CanRemove: true},
	}

	reg := provider.NewRegistry()
	reg.Register(target)

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer db.Close()

	cfg := &config.Config{
		Mappings: []config.Mapping{
			{Group: "design@co.com", Providers: []config.ProviderMapping{{Name: "figma", Role: "editor"}}},
		},
		Policies: config.Policies{DryRun: false},
	}

	r := NewReconciler(db, cfg, reg, identity)
	results, err := r.Run(context.Background())
	require.NoError(t, err)

	require.Len(t, results, 1)
	assert.Equal(t, "figma", results[0].ProviderName)
	assert.Len(t, results[0].ToAdd, 1)
	assert.Len(t, results[0].ToRemove, 1)
	assert.Contains(t, target.added, "alice@co.com")
	assert.Contains(t, target.removed, "charlie@co.com")

	// Verify events were logged
	events, err := db.ListEvents(context.Background(), store.EventFilter{Limit: 10})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(events), 3) // add + remove + sync_completed
}

func TestReconcilerDryRun(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"design@co.com": {{Email: "alice@co.com"}},
		},
	}

	target := &fakeTarget{
		name:  "figma",
		users: []core.User{{Email: "old@co.com"}},
		caps:  core.Capabilities{CanAdd: true, CanRemove: true},
	}

	reg := provider.NewRegistry()
	reg.Register(target)

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer db.Close()

	cfg := &config.Config{
		Mappings: []config.Mapping{
			{Group: "design@co.com", Providers: []config.ProviderMapping{{Name: "figma", Role: "editor"}}},
		},
		Policies: config.Policies{DryRun: true},
	}

	r := NewReconciler(db, cfg, reg, identity)
	results, err := r.Run(context.Background())
	require.NoError(t, err)

	require.Len(t, results, 1)
	assert.True(t, results[0].DryRun)
	// No actual actions executed
	assert.Empty(t, target.added)
	assert.Empty(t, target.removed)
}

func TestReconcilerGracePeriod(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"design@co.com": {{Email: "alice@co.com"}},
		},
	}

	target := &fakeTarget{
		name:  "figma",
		users: []core.User{{Email: "alice@co.com"}, {Email: "old@co.com"}},
		caps:  core.Capabilities{CanAdd: true, CanRemove: true},
	}

	reg := provider.NewRegistry()
	reg.Register(target)

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer db.Close()

	cfg := &config.Config{
		Mappings: []config.Mapping{
			{Group: "design@co.com", Providers: []config.ProviderMapping{{Name: "figma", Role: "editor"}}},
		},
		Policies: config.Policies{GracePeriod: 72 * time.Hour},
	}

	r := NewReconciler(db, cfg, reg, identity)
	_, err = r.Run(context.Background())
	require.NoError(t, err)

	// User NOT removed immediately (grace period)
	assert.Empty(t, target.removed)

	// But pending removal was created
	removals, err := db.GetPendingRemovals(context.Background(), "figma")
	require.NoError(t, err)
	require.Len(t, removals, 1)
	assert.Equal(t, "old@co.com", removals[0].Email)
}

func TestReconcilerExceptions(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"eng@co.com": {{Email: "alice@co.com"}},
		},
	}

	target := &fakeTarget{
		name:  "linear",
		users: []core.User{{Email: "alice@co.com"}, {Email: "service-bot@co.com"}},
		caps:  core.Capabilities{CanAdd: true, CanRemove: true},
	}

	reg := provider.NewRegistry()
	reg.Register(target)

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer db.Close()

	cfg := &config.Config{
		Mappings: []config.Mapping{
			{Group: "eng@co.com", Providers: []config.ProviderMapping{{Name: "linear", Role: "member"}}},
		},
		Policies: config.Policies{
			Exceptions: []config.Exception{
				{Email: "service-bot@co.com", Providers: []string{"*"}},
			},
		},
	}

	r := NewReconciler(db, cfg, reg, identity)
	results, err := r.Run(context.Background())
	require.NoError(t, err)

	require.Len(t, results, 1)
	// service-bot should NOT be in ToRemove because it's an exception
	assert.Empty(t, results[0].ToRemove)
	assert.Empty(t, target.removed)
}

func TestReconcilerSkipsUnregisteredProvider(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"design@co.com": {{Email: "alice@co.com"}},
		},
	}

	reg := provider.NewRegistry() // empty registry — no providers registered

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer db.Close()

	cfg := &config.Config{
		Mappings: []config.Mapping{
			{Group: "design@co.com", Providers: []config.ProviderMapping{{Name: "figma", Role: "editor"}}},
		},
	}

	r := NewReconciler(db, cfg, reg, identity)
	results, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Empty(t, results) // skipped because figma not registered
}

func TestReconcilerMultipleProviders(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"design@co.com": {{Email: "alice@co.com"}},
			"eng@co.com":    {{Email: "bob@co.com"}},
		},
	}

	figma := &fakeTarget{
		name:  "figma",
		users: []core.User{},
		caps:  core.Capabilities{CanAdd: true, CanRemove: true},
	}
	linear := &fakeTarget{
		name:  "linear",
		users: []core.User{},
		caps:  core.Capabilities{CanAdd: true, CanRemove: true},
	}

	reg := provider.NewRegistry()
	reg.Register(figma)
	reg.Register(linear)

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer db.Close()

	cfg := &config.Config{
		Mappings: []config.Mapping{
			{Group: "design@co.com", Providers: []config.ProviderMapping{{Name: "figma", Role: "editor"}}},
			{Group: "eng@co.com", Providers: []config.ProviderMapping{{Name: "linear", Role: "member"}}},
		},
	}

	r := NewReconciler(db, cfg, reg, identity)
	results, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Len(t, results, 2)

	assert.Contains(t, figma.added, "alice@co.com")
	assert.Contains(t, linear.added, "bob@co.com")
}

func TestReconcilerSyncStateUpdated(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"eng@co.com": {{Email: "alice@co.com"}},
		},
	}

	target := &fakeTarget{
		name:  "linear",
		users: []core.User{{Email: "alice@co.com"}, {Email: "bob@co.com"}},
		caps:  core.Capabilities{CanAdd: true, CanRemove: true},
	}

	reg := provider.NewRegistry()
	reg.Register(target)

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer db.Close()

	cfg := &config.Config{
		Mappings: []config.Mapping{
			{Group: "eng@co.com", Providers: []config.ProviderMapping{{Name: "linear", Role: "member"}}},
		},
	}

	r := NewReconciler(db, cfg, reg, identity)
	_, err = r.Run(context.Background())
	require.NoError(t, err)

	state, err := db.GetSyncState(context.Background(), "linear")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, 2, state.UserCount)
	assert.Equal(t, "ok", state.Status)
}
