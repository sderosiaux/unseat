package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestServer(t *testing.T) (*Server, store.Store) {
	t.Helper()
	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	cfg := &config.Config{
		Mappings: []config.Mapping{
			{Group: "eng@co.com", Providers: []config.ProviderMapping{{Name: "linear", Role: "member"}}},
		},
	}

	srv := NewServer(db, cfg)
	return srv, db
}

func TestHandleListProviders(t *testing.T) {
	srv, db := setupTestServer(t)
	require.NoError(t, db.UpdateSyncState(context.Background(), "linear", 10, false))

	req := httptest.NewRequest("GET", "/api/v1/providers", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var states []store.SyncState
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &states))
	assert.Len(t, states, 1)
	assert.Equal(t, "linear", states[0].Provider)
}

func TestHandleProviderUsers(t *testing.T) {
	srv, db := setupTestServer(t)
	require.NoError(t, db.UpsertProviderUsers(context.Background(), "linear", []core.User{
		{Email: "alice@co.com", DisplayName: "Alice", Role: "member", Status: "active"},
	}))

	req := httptest.NewRequest("GET", "/api/v1/providers/linear/users", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var users []core.User
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &users))
	assert.Len(t, users, 1)
	assert.Equal(t, "alice@co.com", users[0].Email)
}

func TestHandleGetMappings(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/v1/mappings", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var mappings []config.Mapping
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &mappings))
	assert.Len(t, mappings, 1)
	assert.Equal(t, "eng@co.com", mappings[0].Group)
}

func TestHandleListBilling(t *testing.T) {
	srv, db := setupTestServer(t)
	require.NoError(t, db.InsertBillingSnapshot(context.Background(), core.BillingSnapshot{
		Provider:           "github",
		BilledSeats:        core.IntPtr(80),
		MonthlyAmountMinor: core.Int64Ptr(168000),
		Currency:           "USD",
		Source:             core.BillingSourceAPIInvoice,
		Confidence:         core.BillingConfidenceExact,
	}))

	req := httptest.NewRequest("GET", "/api/v1/billing", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var out struct {
		Billing []core.BillingSnapshot `json:"billing"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Len(t, out.Billing, 1)
	assert.Equal(t, "github", out.Billing[0].Provider)
	assert.Equal(t, int64(168000), *out.Billing[0].MonthlyAmountMinor)
}

func TestHandleProviderBillingMissingSnapshot(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/v1/providers/linear/billing", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, "linear", out["provider"])
	assert.Nil(t, out["billing"])
}

func TestHandleListCredentials(t *testing.T) {
	srv, db := setupTestServer(t)
	ctx := context.Background()
	require.NoError(t, db.UpsertProviderCredentials(ctx, "github", []core.ClassifiedCredential{
		{
			Credential: core.Credential{
				Kind:             core.CredentialAppInstallation,
				ID:               "app-1",
				Label:            "Deploy Bot",
				CreatedBy:        "gone@co.com",
				Reach:            core.ReachAll,
				PrivilegedScopes: []string{"contents:write"},
			},
			Class:        core.CredentialOrphaned,
			Reason:       "gone@co.com is not in the directory",
			Overreaching: true,
		},
	}))
	require.NoError(t, db.UpdateCredentialSyncState(ctx, store.CredentialSyncState{
		Provider:        "github",
		CredentialCount: 1,
		Status:          "ok",
		UsageKnown:      true,
	}))

	req := httptest.NewRequest("GET", "/api/v1/credentials", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var out credentialsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Len(t, out.Credentials, 1)
	assert.Equal(t, "github", out.Credentials[0].Credential.Provider)
	assert.Equal(t, core.CredentialOrphaned, out.Credentials[0].Class)
	require.Len(t, out.SyncStates, 1)
	assert.Equal(t, "ok", out.SyncStates[0].Status)
}

func TestHandleProviderCredentialsEmptyArray(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/v1/providers/linear/credentials", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var out providerCredentialsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, "linear", out.Provider)
	require.NotNil(t, out.Credentials)
	assert.Empty(t, out.Credentials)
}

func TestHandleCredentialsSummaryIncludesUnsupportedState(t *testing.T) {
	srv, db := setupTestServer(t)
	ctx := context.Background()
	require.NoError(t, db.UpdateCredentialSyncState(ctx, store.CredentialSyncState{
		Provider:        "figma",
		CredentialCount: 0,
		Status:          "not_supported",
		Message:         "provider API exposes no credential listing",
	}))

	req := httptest.NewRequest("GET", "/api/v1/credentials/summary", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var out credentialsSummaryResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.NotNil(t, out.Summary)
	require.Len(t, out.SyncStates, 1)
	assert.Equal(t, "figma", out.SyncStates[0].Provider)
	assert.Equal(t, "not_supported", out.SyncStates[0].Status)
	assert.Empty(t, out.Summary)
}

func TestHandleListDecisions(t *testing.T) {
	srv, db := setupTestServer(t)
	decision := core.NewDecision("alice@co.com", "github", core.ObjectWorkspaceMember, "alice@co.com", core.ActionRemoveWorkspaceMember, core.DecisionRiskHigh, "directory identity is suspended")
	require.NoError(t, db.UpsertDecisions(context.Background(), []core.Decision{decision}))

	req := httptest.NewRequest("GET", "/api/v1/decisions?status=proposed&provider=github", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var out decisionsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Len(t, out.Decisions, 1)
	assert.Equal(t, decision.ID, out.Decisions[0].ID)
}

func TestHandleApproveAndRejectDecision(t *testing.T) {
	srv, db := setupTestServer(t)
	ctx := context.Background()
	decision := core.NewDecision("alice@co.com", "github", core.ObjectWorkspaceMember, "alice@co.com", core.ActionRemoveWorkspaceMember, core.DecisionRiskHigh, "directory identity is suspended")
	require.NoError(t, db.UpsertDecisions(ctx, []core.Decision{decision}))

	req := httptest.NewRequest("POST", "/api/v1/decisions/"+decision.ID+"/approve", bytes.NewBufferString(`{"by":"sre@co.com"}`))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var approved core.Decision
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &approved))
	assert.Equal(t, core.DecisionApproved, approved.Status)
	assert.Equal(t, "sre@co.com", approved.ApprovedBy)

	req = httptest.NewRequest("POST", "/api/v1/decisions/"+decision.ID+"/reject", bytes.NewBufferString(`{"by":"owner@co.com","reason":"keep during migration"}`))
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var rejected core.Decision
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rejected))
	assert.Equal(t, core.DecisionRejected, rejected.Status)
	assert.Equal(t, "owner@co.com", rejected.RejectedBy)
	assert.Equal(t, "keep during migration", rejected.RejectedReason)
}
