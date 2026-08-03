package core

type NonHumanIdentityKind string

const (
	NHIAppInstallation NonHumanIdentityKind = "app_installation"
	NHIIntegration     NonHumanIdentityKind = "integration"
	NHIWebhook         NonHumanIdentityKind = "webhook"
	NHIAPIKey          NonHumanIdentityKind = "api_key"
	NHIDeployKey       NonHumanIdentityKind = "deploy_key"
	NHIOAuthGrant      NonHumanIdentityKind = "oauth_grant"
	NHIServiceAccount  NonHumanIdentityKind = "service_account"
	NHIBot             NonHumanIdentityKind = "bot"
	NHIAgent           NonHumanIdentityKind = "agent"
)

type NonHumanIdentity struct {
	Provider          string               `json:"provider"`
	Kind              NonHumanIdentityKind `json:"kind"`
	ID                string               `json:"id"`
	Label             string               `json:"label"`
	Creator           string               `json:"creator,omitempty"`
	Owner             string               `json:"owner,omitempty"`
	Consumer          string               `json:"consumer,omitempty"`
	Scopes            []string             `json:"scopes,omitempty"`
	PrivilegedScopes  []string             `json:"privileged_scopes,omitempty"`
	Reach             string               `json:"reach,omitempty"`
	LastUsedKnown     bool                 `json:"last_used_known"`
	Disabled          bool                 `json:"disabled"`
	OwnerRequired     bool                 `json:"owner_required"`
	DependencyUnknown bool                 `json:"dependency_unknown"`
}

func CredentialToNHI(c ClassifiedCredential) NonHumanIdentity {
	nhi := NonHumanIdentity{
		Provider:          c.Credential.Provider,
		Kind:              NonHumanIdentityKind(c.Credential.Kind),
		ID:                c.Credential.ID,
		Label:             c.Credential.Label,
		Creator:           c.Credential.CreatedBy,
		Scopes:            c.Credential.Scopes,
		PrivilegedScopes:  c.Credential.PrivilegedScopes,
		Reach:             c.Credential.Reach,
		LastUsedKnown:     c.Credential.LastUsedAt != nil,
		Disabled:          c.Credential.Disabled,
		OwnerRequired:     c.Class == CredentialOrphaned || c.Class == CredentialUnowned,
		DependencyUnknown: c.Credential.Reach == ReachUnknown,
	}
	return nhi
}
