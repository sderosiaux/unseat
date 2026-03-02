package api

import (
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
	t.Cleanup(func() { db.Close() })

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
	db.UpdateSyncState(context.Background(), "linear", 10)

	req := httptest.NewRequest("GET", "/api/v1/providers", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var states []store.SyncState
	json.Unmarshal(w.Body.Bytes(), &states)
	assert.Len(t, states, 1)
	assert.Equal(t, "linear", states[0].Provider)
}

func TestHandleProviderUsers(t *testing.T) {
	srv, db := setupTestServer(t)
	db.UpsertProviderUsers(context.Background(), "linear", []core.User{
		{Email: "alice@co.com", DisplayName: "Alice", Role: "member", Status: "active"},
	})

	req := httptest.NewRequest("GET", "/api/v1/providers/linear/users", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var users []core.User
	json.Unmarshal(w.Body.Bytes(), &users)
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
	json.Unmarshal(w.Body.Bytes(), &mappings)
	assert.Len(t, mappings, 1)
	assert.Equal(t, "eng@co.com", mappings[0].Group)
}
