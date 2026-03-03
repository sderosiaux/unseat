package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconcile(t *testing.T) {
	plan, err := Reconcile(context.Background(), ReconcileInput{
		ProviderName: "figma",
		GroupMappings: []GroupMappingInput{
			{GroupEmail: "design@co.com", Role: "editor"},
		},
		DesiredResolver: func(_ context.Context, group string) ([]User, error) {
			return []User{
				{Email: "alice@co.com"},
				{Email: "bob@co.com"},
			}, nil
		},
		ActualUsers: []User{
			{Email: "bob@co.com"},
			{Email: "charlie@co.com"},
		},
	})
	require.NoError(t, err)
	assert.Len(t, plan.ToAdd, 1)
	assert.Equal(t, "alice@co.com", plan.ToAdd[0].Email)
	assert.Len(t, plan.ToRemove, 1)
	assert.Equal(t, "charlie@co.com", plan.ToRemove[0].Email)
}

func TestReconcileWithExceptions(t *testing.T) {
	plan, err := Reconcile(context.Background(), ReconcileInput{
		ProviderName: "figma",
		GroupMappings: []GroupMappingInput{
			{GroupEmail: "design@co.com", Role: "editor"},
		},
		DesiredResolver: func(_ context.Context, _ string) ([]User, error) {
			return []User{{Email: "alice@co.com"}}, nil
		},
		ActualUsers: []User{
			{Email: "alice@co.com"},
			{Email: "cto@co.com"},
		},
		Exceptions: map[string]bool{"cto@co.com": true},
	})
	require.NoError(t, err)
	assert.Len(t, plan.ToRemove, 0) // cto excluded
}

func TestReconcileDryRun(t *testing.T) {
	plan, err := Reconcile(context.Background(), ReconcileInput{
		ProviderName: "figma",
		GroupMappings: []GroupMappingInput{
			{GroupEmail: "design@co.com", Role: "editor"},
		},
		DesiredResolver: func(_ context.Context, _ string) ([]User, error) {
			return []User{{Email: "new@co.com"}}, nil
		},
		ActualUsers: nil,
		DryRun:      true,
	})
	require.NoError(t, err)
	assert.Len(t, plan.ToAdd, 1)
	assert.True(t, plan.DryRun)
}

func TestReconcileMultipleGroups(t *testing.T) {
	plan, err := Reconcile(context.Background(), ReconcileInput{
		ProviderName: "figma",
		GroupMappings: []GroupMappingInput{
			{GroupEmail: "design@co.com", Role: "editor"},
			{GroupEmail: "eng@co.com", Role: "viewer"},
		},
		DesiredResolver: func(_ context.Context, group string) ([]User, error) {
			if group == "design@co.com" {
				return []User{{Email: "alice@co.com"}}, nil
			}
			return []User{{Email: "bob@co.com"}, {Email: "alice@co.com"}}, nil // alice in both groups
		},
		ActualUsers: []User{
			{Email: "alice@co.com"},
			{Email: "bob@co.com"},
			{Email: "old@co.com"},
		},
	})
	require.NoError(t, err)
	assert.Len(t, plan.ToAdd, 0)    // alice and bob are desired
	assert.Len(t, plan.ToRemove, 1) // old@co.com
	assert.Equal(t, "old@co.com", plan.ToRemove[0].Email)
}

func TestBuildAliasIndex(t *testing.T) {
	index := BuildAliasIndex(
		map[string][]string{
			"dana@co.com":  {"dana99"},
			"river@co.com": {"river@personal.net", "river-gh"},
		},
		[]string{"alice@co.com", "bob@co.com", "dana@co.com", "river@co.com"},
	)

	// Implicit aliases from local parts.
	assert.Equal(t, "alice@co.com", index["alice"])
	assert.Equal(t, "bob@co.com", index["bob"])

	// Explicit aliases.
	assert.Equal(t, "dana@co.com", index["dana99"])
	assert.Equal(t, "river@co.com", index["river@personal.net"])
	assert.Equal(t, "river@co.com", index["river-gh"])

	// Implicit still works for those with explicit too.
	assert.Equal(t, "dana@co.com", index["dana"])
	assert.Equal(t, "river@co.com", index["river"])
}

func TestReconcileWithImplicitAlias(t *testing.T) {
	desiredEmails := []string{"jmartinez@co.com", "alice@co.com"}
	aliasIndex := BuildAliasIndex(nil, desiredEmails)

	plan, err := Reconcile(context.Background(), ReconcileInput{
		ProviderName: "github",
		GroupMappings: []GroupMappingInput{
			{GroupEmail: "eng@co.com", Role: "member"},
		},
		DesiredResolver: func(_ context.Context, _ string) ([]User, error) {
			return []User{
				{Email: "jmartinez@co.com"},
				{Email: "alice@co.com"},
			}, nil
		},
		ActualUsers: []User{
			{Email: "jmartinez"},   // username, not email
			{Email: "alice@co.com"},
		},
		AliasIndex: aliasIndex,
	})
	require.NoError(t, err)
	assert.Len(t, plan.ToAdd, 0, "jmartinez should match via implicit alias")
	assert.Len(t, plan.ToRemove, 0)
	assert.Equal(t, 2, plan.Unchanged)
}

func TestReconcileWithExplicitAlias(t *testing.T) {
	desiredEmails := []string{"tkhan@co.com", "alice@co.com"}
	aliasIndex := BuildAliasIndex(
		map[string][]string{"tkhan@co.com": {"tiger-khan"}},
		desiredEmails,
	)

	plan, err := Reconcile(context.Background(), ReconcileInput{
		ProviderName: "discord",
		GroupMappings: []GroupMappingInput{
			{GroupEmail: "eng@co.com", Role: "member"},
		},
		DesiredResolver: func(_ context.Context, _ string) ([]User, error) {
			return []User{
				{Email: "tkhan@co.com"},
				{Email: "alice@co.com"},
			}, nil
		},
		ActualUsers: []User{
			{Email: "tiger-khan"},  // explicit alias
			{Email: "alice@co.com"},
		},
		AliasIndex: aliasIndex,
	})
	require.NoError(t, err)
	assert.Len(t, plan.ToAdd, 0, "tiger-khan should match tkhan@co.com via explicit alias")
	assert.Len(t, plan.ToRemove, 0)
}

func TestReconcileAliasWithExceptions(t *testing.T) {
	desiredEmails := []string{"alice@co.com"}
	aliasIndex := BuildAliasIndex(nil, desiredEmails)

	plan, err := Reconcile(context.Background(), ReconcileInput{
		ProviderName: "github",
		GroupMappings: []GroupMappingInput{
			{GroupEmail: "eng@co.com", Role: "member"},
		},
		DesiredResolver: func(_ context.Context, _ string) ([]User, error) {
			return []User{{Email: "alice@co.com"}}, nil
		},
		ActualUsers: []User{
			{Email: "alice@co.com"},
			{Email: "bot-ci"},  // not desired, but excepted
		},
		AliasIndex: aliasIndex,
		Exceptions: map[string]bool{"bot-ci": true},
	})
	require.NoError(t, err)
	assert.Len(t, plan.ToRemove, 0, "bot-ci should be excepted")
	assert.Equal(t, 2, plan.Unchanged)
}
