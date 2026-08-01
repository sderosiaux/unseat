package sync

import (
	"context"
	"testing"
	"time"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider"
	"github.com/sderosiaux/unseat/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSafetyStore(t *testing.T) *store.SQLite {
	t.Helper()
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

// strictTarget fails the test the moment anything mutating is called. Unlike
// fakeTarget, which records calls for later inspection, this one refuses to let
// an unexpected write pass silently.
type strictTarget struct {
	t     *testing.T
	name  string
	users []core.User
}

func (s *strictTarget) Name() string                                     { return s.name }
func (s *strictTarget) ListUsers(_ context.Context) ([]core.User, error) { return s.users, nil }
func (s *strictTarget) Capabilities() core.Capabilities {
	return core.Capabilities{CanAdd: true, CanRemove: true, CanSuspend: true, CanSetRole: true}
}

func (s *strictTarget) AddUser(_ context.Context, email, _ string) error {
	s.t.Fatalf("AddUser(%s) called on %s — this run must not mutate any provider", email, s.name)
	return nil
}

func (s *strictTarget) RemoveUser(_ context.Context, email string) error {
	s.t.Fatalf("RemoveUser(%s) called on %s — this run must not mutate any provider", email, s.name)
	return nil
}

func (s *strictTarget) SetRole(_ context.Context, email, role string) error {
	s.t.Fatalf("SetRole(%s, %s) called on %s — this run must not mutate any provider", email, role, s.name)
	return nil
}

// A dry run must reach zero mutating provider calls, even when the plan is full
// of work: users to add, departed identities to remove, and a grace period that
// would otherwise queue removals. The capabilities are deliberately all true so
// nothing is skipped for the wrong reason.
func TestDryRunPerformsNoProviderMutation(t *testing.T) {
	db := newSafetyStore(t)

	target := &strictTarget{
		t:    t,
		name: "figma",
		users: []core.User{
			{Email: "alice@co.com", Status: core.StatusActive},
			{Email: "departed@co.com", Status: core.StatusActive},
			{Email: "contractor@agency.com", Status: core.StatusActive},
		},
	}

	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"design@co.com": {
				{Email: "alice@co.com"},
				{Email: "newcomer@co.com"}, // would be an add
			},
		},
		users: directoryOf("alice@co.com", "newcomer@co.com"),
	}

	reg := provider.NewRegistry()
	reg.Register(target)

	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Provider: "fake-identity", Domain: "co.com"},
		Mappings: []config.Mapping{{
			Group:     "design@co.com",
			Providers: []config.ProviderMapping{{Name: "figma", Role: "editor"}},
		}},
		Policies: config.Policies{
			DryRun:      true,
			GracePeriod: 72 * time.Hour,
		},
	}

	plans, err := NewReconciler(db, cfg, reg, identity).Run(context.Background())
	require.NoError(t, err)
	require.Len(t, plans, 1)

	// The plan is genuinely non-empty: this proves the absence of mutation is
	// the dry-run guard, not an empty diff.
	plan := plans[0]
	assert.NotEmpty(t, plan.ToAdd, "newcomer should be planned for addition")
	assert.NotEmpty(t, plan.ToRemove, "departed identity should be planned for removal")
	assert.True(t, plan.DryRun)

	// Nor may a dry run queue a future removal — a pending row fires later.
	pending, err := db.GetPendingRemovals(context.Background(), "figma")
	require.NoError(t, err)
	assert.Empty(t, pending, "a dry run must not schedule removals either")
}

// The grace period must not become a back door: with a countdown already
// expired, a dry run still may not touch the provider.
func TestDryRunDoesNotExecuteExpiredRemovals(t *testing.T) {
	db := newSafetyStore(t)
	ctx := context.Background()

	require.NoError(t, db.InsertPendingRemoval(ctx, "figma", "departed@co.com", time.Now().Add(-24*time.Hour)))

	target := &strictTarget{
		t:     t,
		name:  "figma",
		users: []core.User{{Email: "departed@co.com", Status: core.StatusActive}},
	}

	identity := &fakeIdentity{
		groups: map[string][]core.User{"design@co.com": {}},
		users:  directoryOf("alice@co.com"),
	}

	reg := provider.NewRegistry()
	reg.Register(target)

	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Provider: "fake-identity", Domain: "co.com"},
		Mappings: []config.Mapping{{
			Group:     "design@co.com",
			Providers: []config.ProviderMapping{{Name: "figma", Role: "editor"}},
		}},
		Policies: config.Policies{DryRun: true, GracePeriod: 72 * time.Hour},
	}

	_, err := NewReconciler(db, cfg, reg, identity).Run(ctx)
	require.NoError(t, err)

	// Still pending, still not executed.
	expired, err := db.GetExpiredRemovals(ctx, "figma")
	require.NoError(t, err)
	assert.Len(t, expired, 1, "the countdown survives a dry run untouched")
}

// Reading the seat inventory must never write to a provider, whatever the
// policy says — audit and scan depend on this.
func TestListUsersPathIsReadOnlyRegardlessOfPolicy(t *testing.T) {
	db := newSafetyStore(t)

	target := &strictTarget{
		t:     t,
		name:  "figma",
		users: []core.User{{Email: "alice@co.com", Status: core.StatusActive}},
	}

	identity := &fakeIdentity{
		groups: map[string][]core.User{"design@co.com": {{Email: "alice@co.com"}}},
		users:  directoryOf("alice@co.com"),
	}

	reg := provider.NewRegistry()
	reg.Register(target)

	// dry_run false, but the plan is empty: nothing to do means nothing done.
	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Provider: "fake-identity", Domain: "co.com"},
		Mappings: []config.Mapping{{
			Group:     "design@co.com",
			Providers: []config.ProviderMapping{{Name: "figma", Role: "editor"}},
		}},
		Policies: config.Policies{DryRun: false},
	}

	plans, err := NewReconciler(db, cfg, reg, identity).Run(context.Background())
	require.NoError(t, err)
	require.Len(t, plans, 1)
	assert.Equal(t, 1, plans[0].Unchanged)
}
