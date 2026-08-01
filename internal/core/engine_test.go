package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// directoryOf builds an active-directory map for tests.
func directoryOf(emails ...string) map[string]DirectoryUser {
	d := make(map[string]DirectoryUser, len(emails))
	for _, e := range emails {
		d[e] = DirectoryUser{Email: e}
	}
	return d
}

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
		// charlie has no directory identity: he has left.
		Directory: directoryOf("alice@co.com", "bob@co.com"),
		Domain:    "co.com",
	})
	require.NoError(t, err)
	assert.Len(t, plan.ToAdd, 1)
	assert.Equal(t, "alice@co.com", plan.ToAdd[0].Email)
	assert.Len(t, plan.ToRemove, 1)
	assert.Equal(t, "charlie@co.com", plan.ToRemove[0].Email)
}

// An active employee outside every mapped group must never be removed — that
// is a mapping gap, not a departure. This is the failure mode that made
// dry_run permanent in practice.
func TestReconcileNeverRemovesActiveEmployee(t *testing.T) {
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
			{Email: "dana@co.com"}, // employed, just not in design@
		},
		Directory: directoryOf("alice@co.com", "dana@co.com"),
		Domain:    "co.com",
	})
	require.NoError(t, err)
	assert.Empty(t, plan.ToRemove)
	require.Len(t, plan.ToReview, 1)
	assert.Equal(t, "dana@co.com", plan.ToReview[0].Email)
	assert.Equal(t, SeatUnmapped, plan.ToReview[0].Class)
}

// A suspended directory identity is a departure, so its seat is reclaimable.
func TestReconcileRemovesSuspendedDirectoryUser(t *testing.T) {
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
			{Email: "gone@co.com"},
		},
		Directory: map[string]DirectoryUser{
			"alice@co.com": {Email: "alice@co.com"},
			"gone@co.com":  {Email: "gone@co.com", Suspended: true},
		},
		Domain: "co.com",
	})
	require.NoError(t, err)
	require.Len(t, plan.ToRemove, 1)
	assert.Equal(t, "gone@co.com", plan.ToRemove[0].Email)
}

// Most providers implement removal as deactivation, so a removed seat keeps
// coming back as an orphan. Re-issuing the removal would re-notify the same
// person on every sync, forever.
func TestReconcileDoesNotReRemoveDeactivatedSeat(t *testing.T) {
	input := ReconcileInput{
		ProviderName: "figma",
		GroupMappings: []GroupMappingInput{
			{GroupEmail: "design@co.com", Role: "editor"},
		},
		DesiredResolver: func(_ context.Context, _ string) ([]User, error) {
			return []User{{Email: "alice@co.com"}}, nil
		},
		ActualUsers: []User{
			{Email: "alice@co.com", Status: StatusActive},
			{Email: "gone@co.com", Status: StatusActive},
		},
		Directory: directoryOf("alice@co.com"),
		Domain:    "co.com",
	}

	// First pass: the departed seat is still active in the provider.
	plan, err := Reconcile(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, plan.ToRemove, 1)
	assert.Equal(t, "gone@co.com", plan.ToRemove[0].Email)
	assert.Empty(t, plan.AlreadyDeactivated)

	// Second pass: the provider deactivated the seat but still reports it.
	input.ActualUsers = []User{
		{Email: "alice@co.com", Status: StatusActive},
		{Email: "gone@co.com", Status: StatusSuspended},
	}
	plan, err = Reconcile(context.Background(), input)
	require.NoError(t, err)
	assert.Empty(t, plan.ToRemove, "the seat is already deactivated — acting again re-notifies forever")
	require.Len(t, plan.AlreadyDeactivated, 1)
	assert.Equal(t, "gone@co.com", plan.AlreadyDeactivated[0].Email)
	// Still visible: a deactivated seat is usually billed until fully deleted.
	assert.Empty(t, plan.ToReview)
}

// Without a directory nothing can be proven to have left, so nothing is removed.
func TestReconcileWithoutDirectoryRemovesNothing(t *testing.T) {
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
			{Email: "charlie@co.com"},
		},
		Domain: "co.com",
	})
	require.NoError(t, err)
	assert.Empty(t, plan.ToRemove)
	assert.Len(t, plan.ToReview, 2)
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
		// cto has left the directory but policy protects the seat.
		Directory:  directoryOf("alice@co.com"),
		Domain:     "co.com",
		Exceptions: map[string]bool{"cto@co.com": true},
	})
	require.NoError(t, err)
	assert.Empty(t, plan.ToRemove, "cto is protected by an exception")
	assert.Empty(t, plan.ToReview, "a protected seat must not be re-reported every run")
	assert.Equal(t, 2, plan.Unchanged)
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
		Directory: directoryOf("alice@co.com", "bob@co.com"),
		Domain:    "co.com",
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
			{Email: "jmartinez"}, // username, not email
			{Email: "alice@co.com"},
		},
		AliasIndex: aliasIndex,
		Directory:  directoryOf("jmartinez@co.com", "alice@co.com"),
		Domain:     "co.com",
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
			{Email: "tiger-khan"}, // explicit alias
			{Email: "alice@co.com"},
		},
		AliasIndex: aliasIndex,
		Directory:  directoryOf("tkhan@co.com", "alice@co.com"),
		Domain:     "co.com",
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
			{Email: "bot-ci"}, // service account: no email, no directory identity
		},
		AliasIndex: aliasIndex,
		Directory:  directoryOf("alice@co.com"),
		Domain:     "co.com",
		Exceptions: map[string]bool{"bot-ci": true},
	})
	require.NoError(t, err)
	assert.Empty(t, plan.ToRemove, "bot-ci should be excepted")
	assert.Empty(t, plan.ToReview, "an excepted service account must not be re-reported")
	assert.Equal(t, 2, plan.Unchanged)
}
