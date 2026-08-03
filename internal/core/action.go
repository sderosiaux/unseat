package core

// ObjectKind names the provider object an action or evidence item concerns.
type ObjectKind string

const (
	ObjectUser                 ObjectKind = "user"
	ObjectWorkspaceMember      ObjectKind = "workspace_member"
	ObjectCredential           ObjectKind = "credential"
	ObjectAppInstallation      ObjectKind = "app_installation"
	ObjectBillingSeat          ObjectKind = "billing_seat"
	ObjectOwnerAttestation     ObjectKind = "owner_attestation"
	ObjectManualTask           ObjectKind = "manual_task"
	ObjectUnknownProviderLimit ObjectKind = "unknown_provider_limit"
)

// ActionClass is the durable verb used by decisions, capabilities and enforce
// policy. It is intentionally more precise than CanRemove.
type ActionClass string

const (
	ActionAddUser                 ActionClass = "add_user"
	ActionDeleteUser              ActionClass = "delete_user"
	ActionDeactivateUser          ActionClass = "deactivate_user"
	ActionSuspendLogin            ActionClass = "suspend_login"
	ActionRemoveWorkspaceMember   ActionClass = "remove_workspace_member"
	ActionReleasePaidSeat         ActionClass = "release_paid_seat"
	ActionTransferOwnership       ActionClass = "transfer_ownership"
	ActionRequestOwnerAttestation ActionClass = "request_owner_attestation"
	ActionCreateManualTask        ActionClass = "create_manual_task"
	ActionRevokeCredential        ActionClass = "revoke_credential"
	ActionMarkUnsupported         ActionClass = "mark_unsupported"
	ActionWaitGracePeriod         ActionClass = "wait_grace_period"
	ActionEscalate                ActionClass = "escalate"
)

// VerificationKind describes how unseat can prove an action actually happened.
type VerificationKind string

const (
	VerificationNone            VerificationKind = "none"
	VerificationAbsentOnRescan  VerificationKind = "absent_on_rescan"
	VerificationStatusOnRescan  VerificationKind = "status_on_rescan"
	VerificationBillingDelta    VerificationKind = "billing_delta"
	VerificationProviderReceipt VerificationKind = "provider_receipt"
	VerificationHumanApproval   VerificationKind = "human_approval"
)

// ActionCapability declares one provider/object/action capability in the
// product vocabulary. It is the bridge from connector plumbing to Enforce.
type ActionCapability struct {
	Provider         string           `json:"provider,omitempty"`
	ObjectKind       ObjectKind       `json:"object_kind"`
	ActionClass      ActionClass      `json:"action_class"`
	Effect           string           `json:"effect,omitempty"`
	CanExecute       bool             `json:"can_execute"`
	CanVerify        bool             `json:"can_verify"`
	Verified         bool             `json:"verified"`
	Destructive      bool             `json:"destructive"`
	RequiresApproval bool             `json:"requires_approval"`
	Verification     VerificationKind `json:"verification"`
	KnownLimits      []string         `json:"known_limits,omitempty"`
}

// ActionMatrix returns explicit action capabilities, converting legacy
// booleans when a provider has not declared a matrix yet.
func (c Capabilities) ActionMatrix(provider string) []ActionCapability {
	if len(c.Actions) > 0 {
		out := make([]ActionCapability, len(c.Actions))
		copy(out, c.Actions)
		for i := range out {
			if out[i].Provider == "" {
				out[i].Provider = provider
			}
		}
		return out
	}
	return LegacyActionCapabilities(provider, c)
}

// LegacyActionCapabilities keeps old connectors usable while the provider
// platform moves from booleans to provider/object/action declarations.
func LegacyActionCapabilities(provider string, c Capabilities) []ActionCapability {
	var out []ActionCapability
	if c.CanAdd {
		out = append(out, ActionCapability{
			Provider:     provider,
			ObjectKind:   ObjectWorkspaceMember,
			ActionClass:  ActionAddUser,
			Effect:       "adds or invites a user according to provider semantics",
			CanExecute:   true,
			CanVerify:    true,
			Verification: VerificationStatusOnRescan,
		})
	}
	if c.CanRemove {
		out = append(out, ActionCapability{
			Provider:         provider,
			ObjectKind:       ObjectWorkspaceMember,
			ActionClass:      ActionRemoveWorkspaceMember,
			Effect:           "removes, deactivates, suspends, or otherwise releases a user's workspace access according to provider semantics",
			CanExecute:       true,
			CanVerify:        true,
			Destructive:      true,
			RequiresApproval: true,
			Verification:     VerificationAbsentOnRescan,
			KnownLimits:      []string{"legacy CanRemove does not state whether the provider deletes, deactivates, suspends, or unassigns"},
		})
	}
	if c.CanSuspend {
		out = append(out, ActionCapability{
			Provider:         provider,
			ObjectKind:       ObjectUser,
			ActionClass:      ActionSuspendLogin,
			Effect:           "suspends login while retaining the provider object",
			CanExecute:       true,
			CanVerify:        true,
			Destructive:      true,
			RequiresApproval: true,
			Verification:     VerificationStatusOnRescan,
		})
	}
	if c.CanSetRole {
		out = append(out, ActionCapability{
			Provider:         provider,
			ObjectKind:       ObjectWorkspaceMember,
			ActionClass:      ActionTransferOwnership,
			Effect:           "changes role or ownership-like responsibility according to provider semantics",
			CanExecute:       true,
			CanVerify:        true,
			RequiresApproval: true,
			Verification:     VerificationStatusOnRescan,
			KnownLimits:      []string{"legacy CanSetRole does not prove ownership transfer semantics"},
		})
	}
	return out
}

// SupportsAction reports whether a matrix contains an executable action class.
func SupportsAction(actions []ActionCapability, class ActionClass) bool {
	for _, a := range actions {
		if a.ActionClass == class && a.CanExecute {
			return true
		}
	}
	return false
}
