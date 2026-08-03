package core

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

type DecisionStatus string

const (
	DecisionProposed           DecisionStatus = "proposed"
	DecisionApproved           DecisionStatus = "approved"
	DecisionRejected           DecisionStatus = "rejected"
	DecisionExecuted           DecisionStatus = "executed"
	DecisionVerified           DecisionStatus = "verified"
	DecisionBlocked            DecisionStatus = "blocked"
	DecisionStale              DecisionStatus = "stale"
	DecisionVerificationFailed DecisionStatus = "verification_failed"
)

type DecisionRisk string

const (
	DecisionRiskLow      DecisionRisk = "low"
	DecisionRiskMedium   DecisionRisk = "medium"
	DecisionRiskHigh     DecisionRisk = "high"
	DecisionRiskCritical DecisionRisk = "critical"
)

type Decision struct {
	ID                string            `json:"id"`
	TenantID          string            `json:"tenant_id,omitempty"`
	Subject           string            `json:"subject"`
	ObjectKind        ObjectKind        `json:"object_kind"`
	ObjectID          string            `json:"object_id"`
	Provider          string            `json:"provider"`
	ActionClass       ActionClass       `json:"action_class"`
	RecommendedAction string            `json:"recommended_action"`
	Status            DecisionStatus    `json:"status"`
	Risk              DecisionRisk      `json:"risk"`
	Reason            string            `json:"reason"`
	PolicyVersion     string            `json:"policy_version"`
	IdempotencyKey    string            `json:"idempotency_key"`
	RequiredEvidence  []string          `json:"required_evidence,omitempty"`
	BlockedBy         []string          `json:"blocked_by,omitempty"`
	ApprovedBy        string            `json:"approved_by,omitempty"`
	RejectedBy        string            `json:"rejected_by,omitempty"`
	RejectedReason    string            `json:"rejected_reason,omitempty"`
	ExpiresAt         *time.Time        `json:"expires_at,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

func NewDecision(subject, provider string, objectKind ObjectKind, objectID string, action ActionClass, risk DecisionRisk, reason string) Decision {
	idKey := strings.Join([]string{subject, provider, string(objectKind), objectID, string(action)}, ":")
	sum := sha256.Sum256([]byte(idKey))
	id := "dec_" + hex.EncodeToString(sum[:])[:16]
	return Decision{
		ID:                id,
		Subject:           subject,
		ObjectKind:        objectKind,
		ObjectID:          objectID,
		Provider:          provider,
		ActionClass:       action,
		RecommendedAction: string(action),
		Status:            DecisionProposed,
		Risk:              risk,
		Reason:            reason,
		PolicyVersion:     DefaultPolicyVersion,
		IdempotencyKey:    id,
	}
}

type DecisionInput struct {
	Subject       CanonicalIdentity
	Seats         []ClassifiedSeat
	Credentials   []ClassifiedCredential
	BillingClaims []BillingClaim
	Capabilities  map[string][]ActionCapability
}

func BuildDecisions(in DecisionInput) []Decision {
	var decisions []Decision
	subject := in.Subject.PrimaryEmail

	for _, seat := range in.Seats {
		matched := in.Subject.Matches(seat.Email) || in.Subject.Matches(seat.RawEmail)
		ambiguous := in.Subject.AmbiguousMatch(seat.Email) || in.Subject.AmbiguousMatch(seat.RawEmail)
		if !matched && !ambiguous {
			continue
		}
		objectID := seat.RawEmail
		if objectID == "" {
			objectID = seat.Email
		}
		if ambiguous && !matched {
			d := NewDecision(subject, seat.Provider, ObjectWorkspaceMember, objectID, ActionCreateManualTask, DecisionRiskMedium, "ambiguous identity match: "+seat.Reason)
			d.BlockedBy = []string{"human_review_required"}
			decisions = append(decisions, d)
			continue
		}
		switch seat.Class {
		case SeatOrphan:
			d := NewDecision(subject, seat.Provider, ObjectWorkspaceMember, objectID, ActionRemoveWorkspaceMember, DecisionRiskHigh, seat.Reason)
			if !SupportsAction(in.Capabilities[seat.Provider], ActionRemoveWorkspaceMember) {
				d.Status = DecisionBlocked
				d.BlockedBy = []string{"provider_does_not_support_action_class"}
				d.ActionClass = ActionMarkUnsupported
				d.RecommendedAction = string(ActionMarkUnsupported)
			}
			decisions = append(decisions, d)
		case SeatExternal, SeatUnresolved, SeatUnmapped:
			d := NewDecision(subject, seat.Provider, ObjectWorkspaceMember, objectID, ActionCreateManualTask, DecisionRiskMedium, seat.Reason)
			d.BlockedBy = []string{"human_review_required"}
			decisions = append(decisions, d)
		}
	}

	for _, cred := range in.Credentials {
		rawOwner := cred.Credential.CreatedBy
		if rawOwner != "" && !in.Subject.Matches(rawOwner) {
			if in.Subject.AmbiguousMatch(rawOwner) {
				d := NewDecision(subject, cred.Credential.Provider, ObjectCredential, cred.Credential.ID, ActionCreateManualTask, DecisionRiskMedium, "ambiguous credential owner: "+cred.Reason)
				d.BlockedBy = []string{"human_review_required"}
				decisions = append(decisions, d)
			}
			continue
		}
		if rawOwner == "" && cred.Class != CredentialUnowned {
			continue
		}
		action := ActionRequestOwnerAttestation
		risk := DecisionRiskMedium
		switch cred.Class {
		case CredentialOrphaned:
			risk = DecisionRiskHigh
		case CredentialDormant:
			action = ActionRevokeCredential
			risk = DecisionRiskLow
		case CredentialUnowned:
			action = ActionRequestOwnerAttestation
			risk = DecisionRiskMedium
		default:
			continue
		}
		d := NewDecision(subject, cred.Credential.Provider, ObjectCredential, cred.Credential.ID, action, risk, cred.Reason)
		if action == ActionRevokeCredential {
			d.BlockedBy = []string{"credential_revocation_not_automated"}
			d.Status = DecisionBlocked
		}
		if cred.Overreaching {
			d.Risk = DecisionRiskCritical
			d.Metadata = map[string]string{"reach_reason": cred.ReachReason}
		}
		decisions = append(decisions, d)
	}

	for _, claim := range in.BillingClaims {
		if claim.Type == BillingClaimMoneyUnknown || claim.Type == BillingClaimProcurementRequired {
			continue
		}
		d := NewDecision(subject, claim.Provider, ObjectBillingSeat, claim.ID, ActionReleasePaidSeat, DecisionRiskMedium, claim.Reason)
		if !claim.Verified {
			d.BlockedBy = []string{"billing_claim_not_verified"}
		}
		decisions = append(decisions, d)
	}

	return decisions
}
