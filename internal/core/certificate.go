package core

import "time"

type CertificateStatus string

const (
	CertificateComplete                   CertificateStatus = "complete"
	CertificateCompleteWithProviderLimits CertificateStatus = "complete_with_provider_limits"
	CertificateBlocked                    CertificateStatus = "blocked"
	CertificateIncomplete                 CertificateStatus = "incomplete"
	CertificateStale                      CertificateStatus = "stale"
)

type CertificateMode string

const (
	CertificateModeObserve CertificateMode = "observe"
	CertificateModeApprove CertificateMode = "approve"
	CertificateModeEnforce CertificateMode = "enforce"
)

type ProviderOffboardingReport struct {
	Provider           string                 `json:"provider"`
	UsersRead          int                    `json:"users_read"`
	Seats              []ClassifiedSeat       `json:"seats,omitempty"`
	Credentials        []ClassifiedCredential `json:"credentials,omitempty"`
	NonHumanIdentities []NonHumanIdentity     `json:"non_human_identities,omitempty"`
	BillingClaims      []BillingClaim         `json:"billing_claims,omitempty"`
	Capabilities       []ActionCapability     `json:"capabilities,omitempty"`
	Unknowns           []string               `json:"unknowns,omitempty"`
	Errors             []string               `json:"errors,omitempty"`
}

type OffboardingCertificate struct {
	ID                        string                      `json:"id"`
	TenantID                  string                      `json:"tenant_id,omitempty"`
	Subject                   CanonicalIdentity           `json:"subject"`
	Trigger                   string                      `json:"trigger"`
	Mode                      CertificateMode             `json:"mode"`
	Status                    CertificateStatus           `json:"status"`
	StartedAt                 time.Time                   `json:"started_at"`
	CompletedAt               time.Time                   `json:"completed_at"`
	Providers                 []ProviderOffboardingReport `json:"providers"`
	Decisions                 []Decision                  `json:"decisions,omitempty"`
	Evidence                  []EvidenceItem              `json:"evidence,omitempty"`
	ClosedAccess              []Decision                  `json:"closed_access,omitempty"`
	TransferredResponsibility []Decision                  `json:"transferred_responsibility,omitempty"`
	PendingReviews            []Decision                  `json:"pending_reviews,omitempty"`
	UnsupportedActions        []Decision                  `json:"unsupported_actions,omitempty"`
	Unknowns                  []string                    `json:"unknowns,omitempty"`
}

func RefreshCertificateDecisionViews(c *OffboardingCertificate) {
	c.ClosedAccess = nil
	c.TransferredResponsibility = nil
	c.PendingReviews = nil
	c.UnsupportedActions = nil

	for _, decision := range c.Decisions {
		switch {
		case decision.Status == DecisionVerified:
			if decision.ActionClass == ActionTransferOwnership || decision.ActionClass == ActionRequestOwnerAttestation {
				c.TransferredResponsibility = append(c.TransferredResponsibility, decision)
			} else {
				c.ClosedAccess = append(c.ClosedAccess, decision)
			}
		case decision.Status == DecisionBlocked || decision.ActionClass == ActionMarkUnsupported:
			c.UnsupportedActions = append(c.UnsupportedActions, decision)
		default:
			c.PendingReviews = append(c.PendingReviews, decision)
		}
	}
	c.Status = ComputeCertificateStatus(*c)
}

func ComputeCertificateStatus(c OffboardingCertificate) CertificateStatus {
	if c.Status == CertificateStale {
		return CertificateStale
	}
	if len(c.Unknowns) > 0 {
		if len(c.Decisions) == 0 {
			return CertificateCompleteWithProviderLimits
		}
	}
	for _, p := range c.Providers {
		if len(p.Errors) > 0 {
			return CertificateBlocked
		}
	}
	for _, d := range c.Decisions {
		switch d.Status {
		case DecisionBlocked:
			return CertificateBlocked
		case DecisionProposed, DecisionApproved, DecisionVerificationFailed:
			return CertificateIncomplete
		}
	}
	if len(c.Unknowns) > 0 {
		return CertificateCompleteWithProviderLimits
	}
	return CertificateComplete
}
