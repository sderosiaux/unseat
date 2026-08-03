package enforce

import (
	"context"
	"testing"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider"
	"github.com/sderosiaux/unseat/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyExecutesApprovedWorkspaceRemoval(t *testing.T) {
	ctx := context.Background()
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	decision := core.NewDecision("alice@co.com", "figma", core.ObjectWorkspaceMember, "alice@co.com", core.ActionRemoveWorkspaceMember, core.DecisionRiskHigh, "directory identity is suspended")
	require.NoError(t, db.UpsertDecisions(ctx, []core.Decision{decision}))
	_, err = db.ApproveDecision(ctx, decision.ID, "sre@co.com")
	require.NoError(t, err)

	p := &fakeProvider{name: "figma", caps: core.Capabilities{CanRemove: true}}
	reg := provider.NewRegistry()
	reg.Register(p)

	executed, err := New(db, reg).Apply(ctx, decision.ID, "enforce")
	require.NoError(t, err)
	assert.Equal(t, core.DecisionExecuted, executed.Status)
	assert.Equal(t, []string{"alice@co.com"}, p.removed)

	events, err := db.ListDecisionEvents(ctx, decision.ID)
	require.NoError(t, err)
	assert.Equal(t, "executed", events[0].EventType)
}

func TestPlanBlocksCredentialActions(t *testing.T) {
	ctx := context.Background()
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	decision := core.NewDecision("alice@co.com", "github", core.ObjectCredential, "app-1", core.ActionRequestOwnerAttestation, core.DecisionRiskMedium, "owner required")
	require.NoError(t, db.UpsertDecisions(ctx, []core.Decision{decision}))
	_, err = db.ApproveDecision(ctx, decision.ID, "sre@co.com")
	require.NoError(t, err)

	reg := provider.NewRegistry()
	reg.Register(&fakeProvider{name: "github", caps: core.Capabilities{CanRemove: true}})

	candidates, err := New(db, reg).Plan(ctx, store.DecisionFilter{})
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.False(t, candidates[0].Executable)
	assert.Contains(t, candidates[0].BlockedBy, "action_class_not_implemented_by_enforce")
}

type fakeProvider struct {
	name    string
	caps    core.Capabilities
	removed []string
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) ListUsers(context.Context) ([]core.User, error) {
	return nil, nil
}
func (f *fakeProvider) AddUser(context.Context, string, string) error {
	return nil
}
func (f *fakeProvider) RemoveUser(_ context.Context, email string) error {
	f.removed = append(f.removed, email)
	return nil
}
func (f *fakeProvider) SetRole(context.Context, string, string) error {
	return nil
}
func (f *fakeProvider) Capabilities() core.Capabilities { return f.caps }
