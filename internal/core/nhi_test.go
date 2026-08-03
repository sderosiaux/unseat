package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCredentialToNHI(t *testing.T) {
	nhi := CredentialToNHI(ClassifiedCredential{
		Credential: Credential{
			Provider:         "github",
			Kind:             CredentialAppInstallation,
			ID:               "123",
			Label:            "deployer",
			CreatedBy:        "alice@co.com",
			Scopes:           []string{"contents", "metadata"},
			PrivilegedScopes: []string{"contents"},
			Reach:            ReachAll,
		},
		Class: CredentialOrphaned,
	})

	assert.Equal(t, NHIAppInstallation, nhi.Kind)
	assert.Equal(t, "alice@co.com", nhi.Creator)
	assert.True(t, nhi.OwnerRequired)
	assert.False(t, nhi.DependencyUnknown)
}
