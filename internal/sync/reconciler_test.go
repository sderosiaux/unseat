package sync

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/notify"
	"github.com/sderosiaux/unseat/internal/provider"
	"github.com/sderosiaux/unseat/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Fakes ---

type fakeIdentity struct {
	groups map[string][]core.User
	// users is what ListUsers returns: the corporate directory the reconciler
	// builds removal decisions from. A seat only becomes removable when its
	// email is absent here, or present with Status "suspended".
	users    []core.User
	usersErr error
}

func (f *fakeIdentity) Name() string { return "fake-identity" }
func (f *fakeIdentity) ListUsers(_ context.Context) ([]core.User, error) {
	if f.usersErr != nil {
		return nil, f.usersErr
	}
	return f.users, nil
}
func (f *fakeIdentity) AddUser(_ context.Context, _, _ string) error { return nil }
func (f *fakeIdentity) RemoveUser(_ context.Context, _ string) error { return nil }
func (f *fakeIdentity) SetRole(_ context.Context, _, _ string) error { return nil }
func (f *fakeIdentity) Capabilities() core.Capabilities              { return core.Capabilities{} }
func (f *fakeIdentity) ListGroups(_ context.Context) ([]core.Group, error) {
	return nil, nil
}
func (f *fakeIdentity) ListGroupMembers(_ context.Context, group string) ([]core.User, error) {
	return f.groups[group], nil
}

// directoryOf builds the active half of a directory for the fake identity.
func directoryOf(emails ...string) []core.User {
	users := make([]core.User, 0, len(emails))
	for _, e := range emails {
		users = append(users, core.User{Email: e, Status: "active"})
	}
	return users
}

// suspendedUser is a directory identity that has been deactivated: still
// listed, but no longer an employee, so its SaaS seats are reclaimable.
func suspendedUser(email string) core.User {
	return core.User{Email: email, Status: "suspended"}
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
		// charlie has no directory identity: he has left the company.
		users: directoryOf("alice@co.com", "bob@co.com"),
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
	defer func() { require.NoError(t, db.Close()) }()

	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Domain: "co.com"},
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
	require.Len(t, results[0].ToAdd, 1)
	assert.Equal(t, "alice@co.com", results[0].ToAdd[0].Email)
	require.Len(t, results[0].ToRemove, 1)
	assert.Equal(t, "charlie@co.com", results[0].ToRemove[0].Email)
	assert.Empty(t, results[0].ToReview)
	assert.Equal(t, 1, results[0].Unchanged, "bob is managed")
	assert.Contains(t, target.added, "alice@co.com")
	assert.Equal(t, []string{"charlie@co.com"}, target.removed)

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
		users: directoryOf("alice@co.com"), // old@co.com has left
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
	defer func() { require.NoError(t, db.Close()) }()

	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Domain: "co.com"},
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
	// The plan is still computed in full...
	assert.Len(t, results[0].ToAdd, 1)
	assert.Len(t, results[0].ToRemove, 1)
	// ...but no actual actions executed.
	assert.Empty(t, target.added)
	assert.Empty(t, target.removed)
}

func TestReconcilerGracePeriod(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"design@co.com": {{Email: "alice@co.com"}},
		},
		users: directoryOf("alice@co.com"), // old@co.com has left
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
	defer func() { require.NoError(t, db.Close()) }()

	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Domain: "co.com"},
		Mappings: []config.Mapping{
			{Group: "design@co.com", Providers: []config.ProviderMapping{{Name: "figma", Role: "editor"}}},
		},
		Policies: config.Policies{GracePeriod: 72 * time.Hour},
	}

	r := NewReconciler(db, cfg, reg, identity)
	results, err := r.Run(context.Background())
	require.NoError(t, err)

	require.Len(t, results, 1)
	require.Len(t, results[0].ToRemove, 1)
	assert.Equal(t, "old@co.com", results[0].ToRemove[0].Email)

	// User NOT removed immediately (grace period)
	assert.Empty(t, target.removed)

	// But pending removal was created
	removals, err := db.GetPendingRemovals(context.Background(), "figma")
	require.NoError(t, err)
	require.Len(t, removals, 1)
	assert.Equal(t, "old@co.com", removals[0].Email)
}

func TestReconcilerDoesNotQueuePendingRemovalWhenProviderCannotRemove(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"design@co.com": {{Email: "alice@co.com"}},
		},
		users: directoryOf("alice@co.com"), // old@co.com has left
	}

	target := &fakeTarget{
		name:  "bamboohr",
		users: []core.User{{Email: "alice@co.com"}, {Email: "old@co.com"}},
		caps:  core.Capabilities{CanRemove: false},
	}

	reg := provider.NewRegistry()
	reg.Register(target)

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	var notifyCount atomic.Int32
	srv := newSlackMock(t, &notifyCount)
	defer srv.Close()

	d := notify.NewDispatcher([]string{"slack:#ops"}, notify.NotifyConfig{
		SlackWebhookURL: srv.URL,
	})

	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Domain: "co.com"},
		Mappings: []config.Mapping{
			{Group: "design@co.com", Providers: []config.ProviderMapping{{Name: "bamboohr", Role: "member"}}},
		},
		Policies: config.Policies{
			DryRun:         false,
			GracePeriod:    72 * time.Hour,
			NotifyOnRemove: true,
		},
	}

	r := NewReconciler(db, cfg, reg, identity, WithNotifier(d))
	results, err := r.Run(context.Background())
	require.NoError(t, err)

	require.Len(t, results, 1)
	require.Len(t, results[0].ToRemove, 1)
	assert.Equal(t, "old@co.com", results[0].ToRemove[0].Email)
	assert.Empty(t, target.removed)
	assert.Equal(t, int32(0), notifyCount.Load(), "non-removable providers must not announce pending removals")

	removals, err := db.GetPendingRemovals(context.Background(), "bamboohr")
	require.NoError(t, err)
	assert.Empty(t, removals, "non-removable providers must not queue removals they cannot execute")
}

func TestReconcilerExceptions(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"eng@co.com": {{Email: "alice@co.com"}},
		},
		// service-bot has no directory identity — it would be an orphan if
		// policy did not protect it.
		users: directoryOf("alice@co.com"),
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
	defer func() { require.NoError(t, db.Close()) }()

	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Domain: "co.com"},
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
	// service-bot is an orphan, but the exception protects it: neither removed
	// nor re-reported for review.
	assert.Empty(t, results[0].ToRemove)
	assert.Empty(t, results[0].ToReview, "a protected seat must not be re-reported every run")
	assert.Equal(t, 2, results[0].Unchanged)
	assert.Empty(t, target.removed)
}

// The single most important guarantee: an active employee the mappings do not
// cover is a mapping gap, not a departure. It goes to review, never to removal.
func TestReconcilerNeverRemovesActiveEmployee(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"design@co.com": {{Email: "alice@co.com"}},
		},
		// dana is employed, just not in design@.
		users: directoryOf("alice@co.com", "dana@co.com"),
	}

	target := &fakeTarget{
		name:  "figma",
		users: []core.User{{Email: "alice@co.com"}, {Email: "dana@co.com"}},
		caps:  core.Capabilities{CanAdd: true, CanRemove: true},
	}

	reg := provider.NewRegistry()
	reg.Register(target)

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Domain: "co.com"},
		Mappings: []config.Mapping{
			{Group: "design@co.com", Providers: []config.ProviderMapping{{Name: "figma", Role: "editor"}}},
		},
		Policies: config.Policies{DryRun: false},
	}

	r := NewReconciler(db, cfg, reg, identity)
	results, err := r.Run(context.Background())
	require.NoError(t, err)

	require.Len(t, results, 1)
	assert.Empty(t, results[0].ToRemove, "an active employee is never removable")
	require.Len(t, results[0].ToReview, 1)
	assert.Equal(t, "dana@co.com", results[0].ToReview[0].Email)
	assert.Equal(t, core.SeatUnmapped, results[0].ToReview[0].Class)
	assert.Equal(t, 1, results[0].Unchanged, "alice is managed")

	assert.Empty(t, target.removed)
	removals, err := db.GetPendingRemovals(context.Background(), "figma")
	require.NoError(t, err)
	assert.Empty(t, removals, "an active employee must not even be queued for removal")
}

// A suspended directory identity is a departure: the seat is reclaimed.
func TestReconcilerRemovesSuspendedDirectoryUser(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"design@co.com": {{Email: "alice@co.com"}},
		},
		users: append(directoryOf("alice@co.com"), suspendedUser("gone@co.com")),
	}

	target := &fakeTarget{
		name:  "figma",
		users: []core.User{{Email: "alice@co.com"}, {Email: "gone@co.com"}},
		caps:  core.Capabilities{CanAdd: true, CanRemove: true},
	}

	reg := provider.NewRegistry()
	reg.Register(target)

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Domain: "co.com"},
		Mappings: []config.Mapping{
			{Group: "design@co.com", Providers: []config.ProviderMapping{{Name: "figma", Role: "editor"}}},
		},
		Policies: config.Policies{DryRun: false},
	}

	r := NewReconciler(db, cfg, reg, identity)
	results, err := r.Run(context.Background())
	require.NoError(t, err)

	require.Len(t, results, 1)
	require.Len(t, results[0].ToRemove, 1)
	assert.Equal(t, "gone@co.com", results[0].ToRemove[0].Email)
	assert.Empty(t, results[0].ToReview)
	assert.Equal(t, []string{"gone@co.com"}, target.removed)
}

// A directory read failure must abort the run. Degrading to "no directory"
// would classify every seat as an orphan and wipe the tenant.
func TestReconcilerDirectoryFailureAbortsRun(t *testing.T) {
	boom := errors.New("directory API 503")
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"design@co.com": {{Email: "alice@co.com"}},
		},
		usersErr: boom,
	}

	target := &fakeTarget{
		name:  "figma",
		users: []core.User{{Email: "alice@co.com"}, {Email: "charlie@co.com"}},
		caps:  core.Capabilities{CanAdd: true, CanRemove: true},
	}

	reg := provider.NewRegistry()
	reg.Register(target)

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Domain: "co.com"},
		Mappings: []config.Mapping{
			{Group: "design@co.com", Providers: []config.ProviderMapping{{Name: "figma", Role: "editor"}}},
		},
		Policies: config.Policies{DryRun: false},
	}

	r := NewReconciler(db, cfg, reg, identity)
	results, err := r.Run(context.Background())

	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
	assert.Nil(t, results)
	assert.Empty(t, target.removed, "no seat may be touched when the directory is unreadable")
	assert.Empty(t, target.added)

	removals, err := db.GetPendingRemovals(context.Background(), "figma")
	require.NoError(t, err)
	assert.Empty(t, removals)
}

// An identity outside the corporate domain needs a human decision.
func TestReconcilerExternalSeatGoesToReview(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"design@co.com": {{Email: "alice@co.com"}},
		},
		users: directoryOf("alice@co.com"),
	}

	target := &fakeTarget{
		name:  "figma",
		users: []core.User{{Email: "alice@co.com"}, {Email: "contractor@agency.com"}},
		caps:  core.Capabilities{CanAdd: true, CanRemove: true},
	}

	reg := provider.NewRegistry()
	reg.Register(target)

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Domain: "co.com"},
		Mappings: []config.Mapping{
			{Group: "design@co.com", Providers: []config.ProviderMapping{{Name: "figma", Role: "editor"}}},
		},
		Policies: config.Policies{DryRun: false},
	}

	r := NewReconciler(db, cfg, reg, identity)
	results, err := r.Run(context.Background())
	require.NoError(t, err)

	require.Len(t, results, 1)
	assert.Empty(t, results[0].ToRemove)
	require.Len(t, results[0].ToReview, 1)
	assert.Equal(t, "contractor@agency.com", results[0].ToReview[0].Email)
	assert.Equal(t, core.SeatExternal, results[0].ToReview[0].Class)
	assert.Empty(t, target.removed)
}

// A provider username no alias resolves cannot be judged, so it is surfaced
// rather than removed.
func TestReconcilerUnresolvedUsernameGoesToReview(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"eng@co.com": {{Email: "alice@co.com"}},
		},
		users: directoryOf("alice@co.com"),
	}

	target := &fakeTarget{
		name:  "linear",
		users: []core.User{{Email: "alice@co.com"}, {Email: "ci-bot"}}, // no @, no alias
		caps:  core.Capabilities{CanAdd: true, CanRemove: true},
	}

	reg := provider.NewRegistry()
	reg.Register(target)

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Domain: "co.com"},
		Mappings: []config.Mapping{
			{Group: "eng@co.com", Providers: []config.ProviderMapping{{Name: "linear", Role: "member"}}},
		},
		Policies: config.Policies{DryRun: false},
	}

	r := NewReconciler(db, cfg, reg, identity)
	results, err := r.Run(context.Background())
	require.NoError(t, err)

	require.Len(t, results, 1)
	assert.Empty(t, results[0].ToRemove)
	require.Len(t, results[0].ToReview, 1)
	assert.Equal(t, "ci-bot", results[0].ToReview[0].Email)
	assert.Equal(t, core.SeatUnresolved, results[0].ToReview[0].Class)
	assert.Empty(t, target.removed)
}

// A configured alias makes a provider username resolve to a directory
// identity, so the seat is managed rather than reviewed.
func TestReconcilerAliasResolvesToDirectoryIdentity(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"eng@co.com": {{Email: "tkhan@co.com"}},
		},
		users: directoryOf("tkhan@co.com"),
	}

	target := &fakeTarget{
		name:  "linear",
		users: []core.User{{Email: "tiger-khan"}},
		caps:  core.Capabilities{CanAdd: true, CanRemove: true},
	}

	reg := provider.NewRegistry()
	reg.Register(target)

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Domain: "co.com"},
		Mappings: []config.Mapping{
			{Group: "eng@co.com", Providers: []config.ProviderMapping{{Name: "linear", Role: "member"}}},
		},
		Aliases:  map[string][]string{"tkhan@co.com": {"tiger-khan"}},
		Policies: config.Policies{DryRun: false},
	}

	r := NewReconciler(db, cfg, reg, identity)
	results, err := r.Run(context.Background())
	require.NoError(t, err)

	require.Len(t, results, 1)
	assert.Empty(t, results[0].ToAdd, "tiger-khan already holds tkhan's seat")
	assert.Empty(t, results[0].ToRemove)
	assert.Empty(t, results[0].ToReview)
	assert.Equal(t, 1, results[0].Unchanged)
	assert.Empty(t, target.removed)
}

func TestReconcilerSkipsUnregisteredProvider(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"design@co.com": {{Email: "alice@co.com"}},
		},
		users: directoryOf("alice@co.com"),
	}

	reg := provider.NewRegistry() // empty registry — no providers registered

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Domain: "co.com"},
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
		users: directoryOf("alice@co.com", "bob@co.com"),
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
	defer func() { require.NoError(t, db.Close()) }()

	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Domain: "co.com"},
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
		// bob is an active employee outside eng@: counted, not removed.
		users: directoryOf("alice@co.com", "bob@co.com"),
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
	defer func() { require.NoError(t, db.Close()) }()

	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Domain: "co.com"},
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
	assert.Empty(t, target.removed)
}

// --- Notification integration tests ---

func newSlackMock(t *testing.T, counter *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		counter.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
}

func TestReconcilerNotifiesOnRemoval(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"eng@co.com": {{Email: "alice@co.com"}},
		},
		users: directoryOf("alice@co.com"), // old@co.com has left
	}

	target := &fakeTarget{
		name:  "linear",
		users: []core.User{{Email: "alice@co.com"}, {Email: "old@co.com"}},
		caps:  core.Capabilities{CanAdd: true, CanRemove: true},
	}

	reg := provider.NewRegistry()
	reg.Register(target)

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	var notifyCount atomic.Int32
	srv := newSlackMock(t, &notifyCount)
	defer srv.Close()

	d := notify.NewDispatcher([]string{"slack:#ops"}, notify.NotifyConfig{
		SlackWebhookURL: srv.URL,
	})

	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Domain: "co.com"},
		Mappings: []config.Mapping{
			{Group: "eng@co.com", Providers: []config.ProviderMapping{{Name: "linear", Role: "member"}}},
		},
		Policies: config.Policies{NotifyOnRemove: true},
	}

	r := NewReconciler(db, cfg, reg, identity, WithNotifier(d))
	_, err = r.Run(context.Background())
	require.NoError(t, err)

	assert.Equal(t, []string{"old@co.com"}, target.removed)
	assert.Equal(t, int32(1), notifyCount.Load(), "expected exactly 1 notification for removal")
}

func TestReconcilerNotifiesOnPendingRemoval(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"eng@co.com": {{Email: "alice@co.com"}},
		},
		users: directoryOf("alice@co.com"), // old@co.com has left
	}

	target := &fakeTarget{
		name:  "linear",
		users: []core.User{{Email: "alice@co.com"}, {Email: "old@co.com"}},
		caps:  core.Capabilities{CanAdd: true, CanRemove: true},
	}

	reg := provider.NewRegistry()
	reg.Register(target)

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	var notifyCount atomic.Int32
	srv := newSlackMock(t, &notifyCount)
	defer srv.Close()

	d := notify.NewDispatcher([]string{"slack:#ops"}, notify.NotifyConfig{
		SlackWebhookURL: srv.URL,
	})

	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Domain: "co.com"},
		Mappings: []config.Mapping{
			{Group: "eng@co.com", Providers: []config.ProviderMapping{{Name: "linear", Role: "member"}}},
		},
		Policies: config.Policies{
			NotifyOnRemove: true,
			GracePeriod:    72 * time.Hour,
		},
	}

	r := NewReconciler(db, cfg, reg, identity, WithNotifier(d))
	_, err = r.Run(context.Background())
	require.NoError(t, err)

	assert.Empty(t, target.removed)
	assert.Equal(t, int32(1), notifyCount.Load(), "expected 1 notification for pending_removal")
}

func TestReconcilerNoNotificationWhenDisabled(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"eng@co.com": {{Email: "alice@co.com"}},
		},
		users: directoryOf("alice@co.com"), // old@co.com has left
	}

	target := &fakeTarget{
		name:  "linear",
		users: []core.User{{Email: "alice@co.com"}, {Email: "old@co.com"}},
		caps:  core.Capabilities{CanAdd: true, CanRemove: true},
	}

	reg := provider.NewRegistry()
	reg.Register(target)

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	var notifyCount atomic.Int32
	srv := newSlackMock(t, &notifyCount)
	defer srv.Close()

	d := notify.NewDispatcher([]string{"slack:#ops"}, notify.NotifyConfig{
		SlackWebhookURL: srv.URL,
	})

	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Domain: "co.com"},
		Mappings: []config.Mapping{
			{Group: "eng@co.com", Providers: []config.ProviderMapping{{Name: "linear", Role: "member"}}},
		},
		Policies: config.Policies{NotifyOnRemove: false},
	}

	r := NewReconciler(db, cfg, reg, identity, WithNotifier(d))
	_, err = r.Run(context.Background())
	require.NoError(t, err)

	assert.Contains(t, target.removed, "old@co.com")
	assert.Equal(t, int32(0), notifyCount.Load(), "no notifications when notify_on_remove is false")
}

func TestReconcilerNilNotifierSafe(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"eng@co.com": {{Email: "alice@co.com"}},
		},
		users: directoryOf("alice@co.com"), // old@co.com has left
	}

	target := &fakeTarget{
		name:  "linear",
		users: []core.User{{Email: "alice@co.com"}, {Email: "old@co.com"}},
		caps:  core.Capabilities{CanAdd: true, CanRemove: true},
	}

	reg := provider.NewRegistry()
	reg.Register(target)

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Domain: "co.com"},
		Mappings: []config.Mapping{
			{Group: "eng@co.com", Providers: []config.ProviderMapping{{Name: "linear", Role: "member"}}},
		},
		Policies: config.Policies{NotifyOnRemove: true},
	}

	// No WithNotifier — notifier is nil
	r := NewReconciler(db, cfg, reg, identity)
	_, err = r.Run(context.Background())
	require.NoError(t, err)

	assert.Contains(t, target.removed, "old@co.com")
}
