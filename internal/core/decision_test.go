package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildDecisionsBlocksRemovalWhenProviderCannotAct(t *testing.T) {
	identity := CanonicalIdentity{
		PrimaryEmail: "alice@co.com",
		Status:       IdentityMatched,
		Confidence:   IdentityConfidenceStrong,
	}

	decisions := BuildDecisions(DecisionInput{
		Subject: identity,
		Seats: []ClassifiedSeat{
			{Provider: "bamboohr", Email: "alice@co.com", RawEmail: "alice@co.com", Class: SeatOrphan, Reason: "directory identity is suspended"},
		},
		Capabilities: map[string][]ActionCapability{
			"bamboohr": {},
		},
	})

	require.Len(t, decisions, 1)
	assert.Equal(t, DecisionBlocked, decisions[0].Status)
	assert.Equal(t, ActionMarkUnsupported, decisions[0].ActionClass)
	assert.Contains(t, decisions[0].BlockedBy, "provider_does_not_support_action_class")
}

func TestBuildDecisionsDoesNotRemoveWeakExternalMatch(t *testing.T) {
	identity := CanonicalIdentity{
		PrimaryEmail: "alice@co.com",
		Status:       IdentityAmbiguous,
		Confidence:   IdentityConfidenceWeak,
		Links: []IdentityLink{{
			RawIdentifier:  "alice@gmail.com",
			CanonicalEmail: "alice@co.com",
			Confidence:     IdentityConfidenceWeak,
			Status:         IdentityAmbiguous,
		}},
	}

	decisions := BuildDecisions(DecisionInput{
		Subject: identity,
		Seats: []ClassifiedSeat{
			{Provider: "figma", Email: "alice@gmail.com", RawEmail: "alice@gmail.com", Class: SeatExternal, Reason: "outside domain"},
		},
		Capabilities: map[string][]ActionCapability{
			"figma": LegacyActionCapabilities("figma", Capabilities{CanRemove: true}),
		},
	})

	require.Len(t, decisions, 1)
	assert.Equal(t, ActionCreateManualTask, decisions[0].ActionClass)
	assert.Equal(t, DecisionProposed, decisions[0].Status)
	assert.Contains(t, decisions[0].BlockedBy, "human_review_required")
}

func TestBuildDecisionsRequestsOwnerForUnownedCredential(t *testing.T) {
	decisions := BuildDecisions(DecisionInput{
		Subject: CanonicalIdentity{PrimaryEmail: "alice@co.com", Status: IdentityMatched},
		Credentials: []ClassifiedCredential{
			{
				Credential: Credential{Provider: "github", ID: "123", Label: "deployer"},
				Class:      CredentialUnowned,
				Reason:     "the provider does not report who authorised this",
			},
		},
	})

	require.Len(t, decisions, 1)
	assert.Equal(t, ActionRequestOwnerAttestation, decisions[0].ActionClass)
	assert.Equal(t, DecisionRiskMedium, decisions[0].Risk)
	assert.Contains(t, decisions[0].RequiredEvidence, "owner_attestation")
	assert.Equal(t, "provider_unowned", decisions[0].Metadata["attestation_scope"])
}
