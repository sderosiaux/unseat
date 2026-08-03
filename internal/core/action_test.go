package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestActionMatrixConvertsLegacyCanRemove(t *testing.T) {
	caps := Capabilities{CanRemove: true}

	actions := caps.ActionMatrix("github")

	requireAction(t, actions, ActionRemoveWorkspaceMember)
	a := actions[0]
	assert.Equal(t, "github", a.Provider)
	assert.Equal(t, ObjectWorkspaceMember, a.ObjectKind)
	assert.True(t, a.CanExecute)
	assert.True(t, a.CanVerify)
	assert.True(t, a.Destructive)
	assert.True(t, a.RequiresApproval)
	assert.Equal(t, VerificationAbsentOnRescan, a.Verification)
	assert.NotEmpty(t, a.KnownLimits)
}

func TestActionMatrixUsesExplicitActions(t *testing.T) {
	caps := Capabilities{
		CanRemove: true,
		Actions: []ActionCapability{
			{ObjectKind: ObjectCredential, ActionClass: ActionRequestOwnerAttestation, CanExecute: true},
		},
	}

	actions := caps.ActionMatrix("linear")

	assert.Len(t, actions, 1)
	assert.Equal(t, "linear", actions[0].Provider)
	assert.Equal(t, ActionRequestOwnerAttestation, actions[0].ActionClass)
	assert.False(t, SupportsAction(actions, ActionRemoveWorkspaceMember), "explicit matrix replaces legacy booleans")
}

func requireAction(t *testing.T, actions []ActionCapability, class ActionClass) {
	t.Helper()
	for _, action := range actions {
		if action.ActionClass == class {
			return
		}
	}
	t.Fatalf("action %s not found in %#v", class, actions)
}
