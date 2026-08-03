package offboarding

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunObserveBuildsCertificateWithoutMutatingProviders(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 2, 10, 30, 0, 0, time.UTC)
	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Provider: "google-directory", Domain: "co.com"},
		Domain:         "co.com",
		Providers: map[string]config.ProviderConfig{
			"github": {APIKey: "token"},
		},
		Mappings: []config.Mapping{{
			Group: "eng@co.com",
			Providers: []config.ProviderMapping{
				{Name: "github", Role: "member"},
			},
		}},
		Aliases: map[string][]string{
			"alice@co.com": {"alice-dev"},
		},
	}
	identity := &fakeIdentity{
		users: []core.User{
			{Email: "alice@co.com", Status: core.StatusSuspended},
			{Email: "bob@co.com", Status: core.StatusActive},
		},
		members: map[string][]core.User{
			"eng@co.com": {{Email: "bob@co.com", Status: core.StatusActive}},
		},
	}
	github := &fakeFullProvider{
		name: "github",
		users: []core.User{
			{Email: "alice-dev", Status: core.StatusActive},
		},
		caps: core.Capabilities{CanRemove: true},
		credentials: []core.Credential{{
			Provider:         "github",
			Kind:             core.CredentialAppInstallation,
			ID:               "app-1",
			Label:            "Deploy Bot",
			CreatedBy:        "alice-dev",
			Reach:            core.ReachAll,
			PrivilegedScopes: []string{"contents:write"},
		}},
		billing: &core.Billing{
			Provider:         "github",
			BilledSeats:      core.IntPtr(3),
			CostPerSeatMinor: core.Int64Ptr(1200),
			Currency:         "USD",
			Source:           core.BillingSourceAPIInvoice,
		},
	}
	reg := provider.NewRegistry()
	reg.Register(github)

	cert, err := RunObserve(ctx, Input{
		Config:   cfg,
		Registry: reg,
		Identity: identity,
		Subject:  "alice@co.com",
		Now:      now,
		Actor:    "tester",
	})

	require.NoError(t, err)
	assert.Equal(t, core.CertificateIncomplete, cert.Status)
	assert.Equal(t, "alice@co.com", cert.Subject.PrimaryEmail)
	require.Len(t, cert.Providers, 1)
	assert.Equal(t, 1, cert.Providers[0].UsersRead)
	require.Len(t, cert.Providers[0].Seats, 1)
	assert.Equal(t, core.SeatOrphan, cert.Providers[0].Seats[0].Class)
	require.Len(t, cert.Providers[0].Credentials, 1)
	assert.Equal(t, core.CredentialOrphaned, cert.Providers[0].Credentials[0].Class)
	require.Len(t, cert.Providers[0].NonHumanIdentities, 1)
	assert.True(t, cert.Providers[0].NonHumanIdentities[0].OwnerRequired)
	require.Len(t, cert.Providers[0].BillingClaims, 1)
	assert.Equal(t, core.BillingClaimSavingVerified, cert.Providers[0].BillingClaims[0].Type)
	require.NotNil(t, cert.Providers[0].BillingClaims[0].AmountMinor)
	assert.Equal(t, int64(1200), *cert.Providers[0].BillingClaims[0].AmountMinor)
	assert.NotEmpty(t, cert.Evidence)
	assert.GreaterOrEqual(t, len(cert.PendingReviews), 3)
	assert.Zero(t, github.writeCalls)
}

func TestRunObserveTurnsWeakPersonalSeatIntoManualReview(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Provider: "google-directory", Domain: "co.com"},
		Domain:         "co.com",
		Providers: map[string]config.ProviderConfig{
			"figma": {APIKey: "token"},
		},
	}
	identity := &fakeIdentity{
		users: []core.User{{Email: "alice@co.com", Status: core.StatusActive}},
	}
	figma := &fakeProvider{
		name:  "figma",
		users: []core.User{{Email: "alice@gmail.com", Status: core.StatusActive}},
		caps:  core.Capabilities{CanRemove: true},
	}
	reg := provider.NewRegistry()
	reg.Register(figma)

	cert, err := RunObserve(ctx, Input{
		Config:   cfg,
		Registry: reg,
		Identity: identity,
		Subject:  "alice@co.com",
		Now:      time.Date(2026, 8, 2, 10, 30, 0, 0, time.UTC),
	})

	require.NoError(t, err)
	require.Len(t, cert.Decisions, 1)
	assert.Equal(t, core.ActionCreateManualTask, cert.Decisions[0].ActionClass)
	assert.Contains(t, cert.Decisions[0].BlockedBy, "human_review_required")
	assert.NotContains(t, decisionActions(cert.Decisions), core.ActionRemoveWorkspaceMember)
	assert.Zero(t, figma.writeCalls)
}

func TestRunObserveKeepsCredentialReadFailuresAsProviderLimits(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Provider: "google-directory", Domain: "co.com"},
		Domain:         "co.com",
		Providers: map[string]config.ProviderConfig{
			"github": {APIKey: "token"},
		},
	}
	identity := &fakeIdentity{users: []core.User{{Email: "alice@co.com", Status: core.StatusSuspended}}}
	github := &fakeFullProvider{
		name:           "github",
		users:          []core.User{{Email: "alice@co.com", Status: core.StatusActive}},
		caps:           core.Capabilities{CanRemove: true},
		credentialsErr: errors.New("missing apps scope"),
	}
	reg := provider.NewRegistry()
	reg.Register(github)

	cert, err := RunObserve(ctx, Input{
		Config:   cfg,
		Registry: reg,
		Identity: identity,
		Subject:  "alice@co.com",
		Now:      time.Date(2026, 8, 2, 10, 30, 0, 0, time.UTC),
	})

	require.NoError(t, err)
	require.Len(t, cert.Providers, 1)
	assert.Empty(t, cert.Providers[0].Errors)
	require.NotEmpty(t, cert.Providers[0].Unknowns)
	assert.Contains(t, cert.Unknowns, "github: non-human identity inventory unavailable: missing apps scope")
	assert.Zero(t, github.writeCalls)
}

func TestRunObserveTurnsUnownedCredentialIntoOwnerAttestationDecision(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{
		IdentitySource: config.IdentitySource{Provider: "google-directory", Domain: "co.com"},
		Domain:         "co.com",
		Providers: map[string]config.ProviderConfig{
			"github": {APIKey: "token"},
		},
	}
	identity := &fakeIdentity{users: []core.User{{Email: "alice@co.com", Status: core.StatusSuspended}}}
	github := &fakeFullProvider{
		name:  "github",
		users: []core.User{{Email: "alice@co.com", Status: core.StatusActive}},
		caps:  core.Capabilities{CanRemove: true},
		credentials: []core.Credential{{
			Provider: "github",
			Kind:     core.CredentialDeployKey,
			ID:       "key-1",
			Label:    "deploy key",
		}},
	}
	reg := provider.NewRegistry()
	reg.Register(github)

	cert, err := RunObserve(ctx, Input{
		Config:   cfg,
		Registry: reg,
		Identity: identity,
		Subject:  "alice@co.com",
		Now:      time.Date(2026, 8, 2, 10, 30, 0, 0, time.UTC),
	})

	require.NoError(t, err)
	require.Len(t, cert.Providers, 1)
	require.Len(t, cert.Providers[0].Credentials, 1)
	require.Len(t, cert.Providers[0].NonHumanIdentities, 1)
	assert.True(t, cert.Providers[0].NonHumanIdentities[0].OwnerRequired)
	require.Contains(t, decisionActions(cert.Decisions), core.ActionRequestOwnerAttestation)
	var attestation core.Decision
	for _, decision := range cert.Decisions {
		if decision.ActionClass == core.ActionRequestOwnerAttestation {
			attestation = decision
			break
		}
	}
	assert.Equal(t, core.ObjectCredential, attestation.ObjectKind)
	assert.Equal(t, "key-1", attestation.ObjectID)
	assert.Equal(t, "provider_unowned", attestation.Metadata["attestation_scope"])
	assert.Contains(t, attestation.RequiredEvidence, "owner_attestation")
	assert.Zero(t, github.writeCalls)
}

func decisionActions(decisions []core.Decision) []core.ActionClass {
	out := make([]core.ActionClass, 0, len(decisions))
	for _, d := range decisions {
		out = append(out, d.ActionClass)
	}
	return out
}

type fakeIdentity struct {
	users   []core.User
	members map[string][]core.User
}

func (f *fakeIdentity) Name() string { return "google-directory" }
func (f *fakeIdentity) ListUsers(context.Context) ([]core.User, error) {
	return f.users, nil
}
func (f *fakeIdentity) ListGroups(context.Context) ([]core.Group, error) {
	return nil, nil
}
func (f *fakeIdentity) ListGroupMembers(_ context.Context, groupEmail string) ([]core.User, error) {
	return f.members[groupEmail], nil
}
func (f *fakeIdentity) AddUser(context.Context, string, string) error {
	panic("offboarding observe must not add users")
}
func (f *fakeIdentity) RemoveUser(context.Context, string) error {
	panic("offboarding observe must not remove users")
}
func (f *fakeIdentity) SetRole(context.Context, string, string) error {
	panic("offboarding observe must not set roles")
}
func (f *fakeIdentity) Capabilities() core.Capabilities { return core.Capabilities{} }

type fakeProvider struct {
	name       string
	users      []core.User
	caps       core.Capabilities
	writeCalls int
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) ListUsers(context.Context) ([]core.User, error) {
	return f.users, nil
}
func (f *fakeProvider) AddUser(context.Context, string, string) error {
	f.writeCalls++
	return errors.New("unexpected AddUser")
}
func (f *fakeProvider) RemoveUser(context.Context, string) error {
	f.writeCalls++
	return errors.New("unexpected RemoveUser")
}
func (f *fakeProvider) SetRole(context.Context, string, string) error {
	f.writeCalls++
	return errors.New("unexpected SetRole")
}
func (f *fakeProvider) Capabilities() core.Capabilities { return f.caps }

type fakeFullProvider struct {
	fakeProvider
	name           string
	users          []core.User
	caps           core.Capabilities
	credentials    []core.Credential
	credentialsErr error
	billing        *core.Billing
	billingErr     error
	writeCalls     int
}

func (f *fakeFullProvider) Name() string { return f.name }
func (f *fakeFullProvider) ListUsers(context.Context) ([]core.User, error) {
	return f.users, nil
}
func (f *fakeFullProvider) AddUser(context.Context, string, string) error {
	f.writeCalls++
	return errors.New("unexpected AddUser")
}
func (f *fakeFullProvider) RemoveUser(context.Context, string) error {
	f.writeCalls++
	return errors.New("unexpected RemoveUser")
}
func (f *fakeFullProvider) SetRole(context.Context, string, string) error {
	f.writeCalls++
	return errors.New("unexpected SetRole")
}
func (f *fakeFullProvider) Capabilities() core.Capabilities { return f.caps }
func (f *fakeFullProvider) ListCredentials(context.Context) ([]core.Credential, error) {
	return f.credentials, f.credentialsErr
}
func (f *fakeFullProvider) Billing(context.Context) (*core.Billing, error) {
	return f.billing, f.billingErr
}
