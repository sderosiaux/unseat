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
