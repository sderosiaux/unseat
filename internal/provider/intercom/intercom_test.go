package intercom

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderName(t *testing.T) {
	p := New("test-token")
	assert.Equal(t, "intercom", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token")
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.False(t, caps.CanAdd)
	assert.False(t, caps.CanSuspend)
	assert.False(t, caps.CanSetRole)
	assert.False(t, caps.HasWebhook)
	assert.False(t, caps.ReportsActivity)
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/admins", r.URL.Path)

		// Raw JSON in the shape Intercom's own OpenAPI spec declares for the
		// admin schema (intercom/Intercom-OpenAPI, GET /admins example and
		// components.schemas.admin): type, id, name, email, job_title,
		// away_mode_enabled, away_mode_reassign, has_inbox_seat, team_ids,
		// avatar. There is no role and no last_request_at on this object —
		// encoding our own intercomAdmin struct would have round-tripped both
		// and hidden that the live API never sends them, exactly how
		// last_request_at stayed invisible on three other connectors.
		fmt.Fprint(w, `{
		  "type": "admin.list",
		  "admins": [
		    {"type":"admin","id":"1","name":"Alice","email":"alice@co.com","job_title":"Support Lead","away_mode_enabled":false,"away_mode_reassign":false,"has_inbox_seat":true,"team_ids":[]},
		    {"type":"admin","id":"2","name":"Bob","email":"bob@co.com","job_title":"","away_mode_enabled":true,"away_mode_reassign":false,"has_inbox_seat":true,"team_ids":[]}
		  ]
		}`)
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice", users[0].DisplayName)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "1", users[0].ProviderID)
	// intercomAdmin.Role parses a "role" key that this schema does not
	// declare and the example response never sends, so it is always empty —
	// dead code, not a mapping this test can claim to cover.
	assert.Empty(t, users[0].Role, "the Admin object has no role field; none may be invented")

	assert.Equal(t, "bob@co.com", users[1].Email)
	// away_mode_enabled is a presence toggle, not account state: a teammate who
	// stepped away still holds a paid seat. Reporting "away" as a status also
	// broke the documented contract, which is active or suspended.
	assert.Equal(t, core.StatusActive, users[1].Status)
	assert.Equal(t, "true", users[1].Metadata["away"])

	// The Admin object carries no activity field at all — last_request_at
	// exists only on Contact and Company, per the same spec — so none may be
	// invented regardless of what a future payload contains.
	for _, u := range users {
		assert.Nil(t, u.LastActivityAt)
	}
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			json.NewEncoder(w).Encode(adminsResponse{
				Type: "admin.list",
				Admins: []intercomAdmin{
					{ID: "42", Name: "Alice", Email: "alice@co.com"},
				},
			})
		} else {
			assert.Equal(t, http.MethodPut, r.Method)
			assert.Equal(t, "/admins/42/away", r.URL.Path)
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(adminsResponse{Type: "admin.list", Admins: []intercomAdmin{}})
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestListUsersAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"type":"error.list","errors":[{"code":"token_unauthorized"}]}`))
	}))
	defer server.Close()

	p := New("bad-token").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-token")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
