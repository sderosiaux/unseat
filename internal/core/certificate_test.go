package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeCertificateStatus(t *testing.T) {
	tests := []struct {
		name string
		cert OffboardingCertificate
		want CertificateStatus
	}{
		{name: "complete", cert: OffboardingCertificate{}, want: CertificateComplete},
		{name: "provider limits", cert: OffboardingCertificate{Unknowns: []string{"github audit log unavailable"}}, want: CertificateCompleteWithProviderLimits},
		{name: "provider error blocks", cert: OffboardingCertificate{Providers: []ProviderOffboardingReport{{Provider: "github", Errors: []string{"403"}}}}, want: CertificateBlocked},
		{name: "blocked decision blocks", cert: OffboardingCertificate{Decisions: []Decision{{Status: DecisionBlocked}}}, want: CertificateBlocked},
		{name: "proposed decision incomplete", cert: OffboardingCertificate{Decisions: []Decision{{Status: DecisionProposed}}}, want: CertificateIncomplete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ComputeCertificateStatus(tt.cert))
		})
	}
}
