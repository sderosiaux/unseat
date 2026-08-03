package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/store"
	"github.com/stretchr/testify/require"
)

func TestNewMCPServer(t *testing.T) {
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	cfg := &config.Config{}
	srv := New(db, cfg)
	require.NotNil(t, srv)
	require.NotNil(t, srv.server)
}

func TestHandleListProviders(t *testing.T) {
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	ctx := context.Background()
	require.NoError(t, db.UpdateSyncState(ctx, "slack", 5, false))

	srv := New(db, &config.Config{})
	_, out, err := srv.handleListProviders(ctx, nil, emptyInput{})
	require.NoError(t, err)
	require.Len(t, out.Providers, 1)
	require.Equal(t, "slack", out.Providers[0].Provider)
	require.Equal(t, 5, out.Providers[0].UserCount)
}

func TestHandleProviderUsers(t *testing.T) {
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	ctx := context.Background()
	require.NoError(t, db.UpsertProviderUsers(ctx, "github", []core.User{
		{Email: "alice@test.com", DisplayName: "Alice", Role: "admin", Status: "active"},
		{Email: "bob@test.com", DisplayName: "Bob", Role: "member", Status: "active"},
	}))

	srv := New(db, &config.Config{})
	_, out, err := srv.handleProviderUsers(ctx, nil, providerInput{Provider: "github"})
	require.NoError(t, err)
	require.Len(t, out.Users, 2)
	require.Equal(t, "alice@test.com", out.Users[0].Email)
}

func TestHandleListBilling(t *testing.T) {
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	ctx := context.Background()
	require.NoError(t, db.InsertBillingSnapshot(ctx, core.BillingSnapshot{
		Provider:           "github",
		BilledSeats:        core.IntPtr(80),
		MonthlyAmountMinor: core.Int64Ptr(168000),
		Currency:           "USD",
		Source:             core.BillingSourceAPIInvoice,
		Confidence:         core.BillingConfidenceExact,
	}))

	srv := New(db, &config.Config{})
	_, out, err := srv.handleListBilling(ctx, nil, emptyInput{})
	require.NoError(t, err)
	require.Len(t, out.Billing, 1)
	require.Equal(t, "github", out.Billing[0].Provider)
	require.Equal(t, int64(168000), *out.Billing[0].MonthlyAmountMinor)
}

func TestHandleProviderBillingMissingSnapshot(t *testing.T) {
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	srv := New(db, &config.Config{})
	_, out, err := srv.handleProviderBilling(context.Background(), nil, providerInput{Provider: "linear"})
	require.NoError(t, err)
	require.Equal(t, "linear", out.Provider)
	require.Nil(t, out.Billing)
	require.NotEmpty(t, out.Reason)
}

func TestHandleListOrphans(t *testing.T) {
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	ctx := context.Background()
	require.NoError(t, db.UpdateSyncState(ctx, "slack", 3, false))
	require.NoError(t, db.InsertPendingRemoval(ctx, "slack", "gone@test.com", time.Now().Add(72*time.Hour)))

	srv := New(db, &config.Config{})
	_, out, err := srv.handleListOrphans(ctx, nil, emptyInput{})
	require.NoError(t, err)
	require.Len(t, out.Orphans, 1)
	require.Equal(t, "gone@test.com", out.Orphans[0].Email)
	require.Equal(t, "slack", out.Orphans[0].Provider)
}

func TestHandleListEvents(t *testing.T) {
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	ctx := context.Background()
	require.NoError(t, db.InsertEvent(ctx, core.Event{
		Type: core.EventUserAdded, Provider: "github", Email: "new@test.com", Trigger: "cron", OccurredAt: time.Now(),
	}))

	srv := New(db, &config.Config{})

	// Default limit
	_, out, err := srv.handleListEvents(ctx, nil, eventsInput{})
	require.NoError(t, err)
	require.Len(t, out.Events, 1)

	// Explicit limit
	_, out, err = srv.handleListEvents(ctx, nil, eventsInput{Limit: 10})
	require.NoError(t, err)
	require.Len(t, out.Events, 1)
}

func TestHandleGetMappings(t *testing.T) {
	cfg := &config.Config{
		Mappings: []config.Mapping{
			{Group: "engineering", Providers: []config.ProviderMapping{
				{Name: "github", Role: "member"},
				{Name: "slack", Role: "member"},
			}},
		},
	}

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	srv := New(db, cfg)
	_, out, err := srv.handleGetMappings(context.Background(), nil, emptyInput{})
	require.NoError(t, err)
	require.Len(t, out.Mappings, 1)
	require.Equal(t, "engineering", out.Mappings[0].Group)
	require.Len(t, out.Mappings[0].Providers, 2)
}

func TestHandleGetMappingsEmpty(t *testing.T) {
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	srv := New(db, &config.Config{})
	_, out, err := srv.handleGetMappings(context.Background(), nil, emptyInput{})
	require.NoError(t, err)
	require.NotNil(t, out.Mappings)
	require.Empty(t, out.Mappings)
}

func TestHandleListCredentials(t *testing.T) {
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	ctx := context.Background()
	require.NoError(t, db.UpsertProviderCredentials(ctx, "github", []core.ClassifiedCredential{
		{
			Credential: core.Credential{
				Kind:      core.CredentialDeployKey,
				ID:        "key-1",
				Label:     "Production Deploy",
				CreatedBy: "gone@test.com",
			},
			Class:  core.CredentialOrphaned,
			Reason: "gone@test.com is not in the directory",
		},
	}))
	require.NoError(t, db.UpdateCredentialSyncState(ctx, store.CredentialSyncState{
		Provider:        "github",
		CredentialCount: 1,
		Status:          "ok",
	}))

	srv := New(db, &config.Config{})
	_, out, err := srv.handleListCredentials(ctx, nil, emptyInput{})
	require.NoError(t, err)
	require.Len(t, out.Credentials, 1)
	require.Equal(t, "github", out.Credentials[0].Credential.Provider)
	require.Len(t, out.SyncStates, 1)
	require.Equal(t, "ok", out.SyncStates[0].Status)
}

func TestHandleProviderCredentialsEmptyArray(t *testing.T) {
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	srv := New(db, &config.Config{})
	_, out, err := srv.handleProviderCredentials(context.Background(), nil, providerInput{Provider: "linear"})
	require.NoError(t, err)
	require.Equal(t, "linear", out.Provider)
	require.NotNil(t, out.Credentials)
	require.Empty(t, out.Credentials)
	require.Nil(t, out.SyncState)
}

func TestHandleCredentialSummaryIncludesUnsupportedState(t *testing.T) {
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	ctx := context.Background()
	require.NoError(t, db.UpdateCredentialSyncState(ctx, store.CredentialSyncState{
		Provider:        "figma",
		CredentialCount: 0,
		Status:          "not_supported",
		Message:         "provider API exposes no credential listing",
	}))

	srv := New(db, &config.Config{})
	_, out, err := srv.handleCredentialSummary(ctx, nil, emptyInput{})
	require.NoError(t, err)
	require.NotNil(t, out.Summary)
	require.Empty(t, out.Summary)
	require.Len(t, out.SyncStates, 1)
	require.Equal(t, "figma", out.SyncStates[0].Provider)
	require.Equal(t, "not_supported", out.SyncStates[0].Status)
}

func TestHandleDecisionLedgerTools(t *testing.T) {
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	ctx := context.Background()
	decision := core.NewDecision("alice@test.com", "github", core.ObjectWorkspaceMember, "alice@test.com", core.ActionRemoveWorkspaceMember, core.DecisionRiskHigh, "directory identity is suspended")
	require.NoError(t, db.UpsertDecisions(ctx, []core.Decision{decision}))

	srv := New(db, &config.Config{})
	_, listed, err := srv.handleListDecisions(ctx, nil, decisionsInput{Provider: "github", Status: string(core.DecisionProposed)})
	require.NoError(t, err)
	require.Len(t, listed.Decisions, 1)
	require.Equal(t, decision.ID, listed.Decisions[0].ID)

	_, approved, err := srv.handleApproveDecision(ctx, nil, decisionMutationInput{ID: decision.ID, By: "sre@test.com"})
	require.NoError(t, err)
	require.NotNil(t, approved.Decision)
	require.Equal(t, core.DecisionApproved, approved.Decision.Status)
	require.Equal(t, "sre@test.com", approved.Decision.ApprovedBy)

	_, events, err := srv.handleDecisionEvents(ctx, nil, decisionInput{ID: decision.ID})
	require.NoError(t, err)
	require.NotEmpty(t, events.Events)
	require.Equal(t, "approved", events.Events[0].EventType)
}

func TestHandleRejectDecisionRequiresReason(t *testing.T) {
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	ctx := context.Background()
	decision := core.NewDecision("alice@test.com", "github", core.ObjectWorkspaceMember, "alice@test.com", core.ActionRemoveWorkspaceMember, core.DecisionRiskHigh, "directory identity is suspended")
	require.NoError(t, db.UpsertDecisions(ctx, []core.Decision{decision}))

	srv := New(db, &config.Config{})
	_, _, err = srv.handleRejectDecision(ctx, nil, decisionMutationInput{ID: decision.ID, By: "owner@test.com"})
	require.Error(t, err)
}

func TestHandleCertificateTools(t *testing.T) {
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	ctx := context.Background()
	started := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	cert := core.OffboardingCertificate{
		ID:          "cert_123",
		Subject:     core.CanonicalIdentity{PrimaryEmail: "alice@test.com"},
		Status:      core.CertificateIncomplete,
		Mode:        core.CertificateModeObserve,
		Trigger:     "manual",
		StartedAt:   started,
		CompletedAt: started.Add(time.Minute),
		Providers:   []core.ProviderOffboardingReport{{Provider: "github"}},
		Unknowns:    []string{"github: billing unknown"},
	}
	require.NoError(t, db.UpsertOffboardingCertificate(ctx, cert))

	srv := New(db, &config.Config{})
	_, listed, err := srv.handleListCertificates(ctx, nil, certificatesInput{
		Subject: "alice@test.com",
		Status:  string(core.CertificateIncomplete),
	})
	require.NoError(t, err)
	require.Len(t, listed.Certificates, 1)
	require.Equal(t, cert.ID, listed.Certificates[0].ID)

	_, got, err := srv.handleGetCertificate(ctx, nil, certificateInput{ID: cert.ID})
	require.NoError(t, err)
	require.NotNil(t, got.Certificate)
	require.Equal(t, "alice@test.com", got.Certificate.Subject.PrimaryEmail)
}
