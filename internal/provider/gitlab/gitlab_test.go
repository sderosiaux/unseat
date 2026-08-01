package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderName(t *testing.T) {
	p := New("test-token")
	assert.Equal(t, "gitlab", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token")
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.True(t, caps.CanSuspend)
	assert.False(t, caps.CanAdd)
	assert.False(t, caps.CanSetRole)
	assert.False(t, caps.HasWebhook)
	// current_sign_in_at is documented as admin-only (docs.gitlab.com/api/users);
	// we cannot confirm at runtime that the configured PAT is an admin's, so this
	// must stay false rather than let a nil LastActivityAt read as "never active".
	assert.False(t, caps.ReportsActivity)
}

// TestListUsers uses the regular-user response shape from
// docs.gitlab.com/api/users ("As a regular user" example): no email, no
// current_sign_in_at. That is the shape a non-admin PAT -- the common case,
// since token privilege can't be verified at runtime -- actually receives.
func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-token", r.Header.Get("PRIVATE-TOKEN"))
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v4/users", r.URL.Path)
		assert.Equal(t, "true", r.URL.Query().Get("active"))

		w.Header().Set("X-Total-Pages", "1")
		w.Write([]byte(`[
			{"id": 1, "username": "alice", "name": "Alice Smith", "state": "active", "locked": false, "avatar_url": "https://gitlab.com/avatar/1", "web_url": "https://gitlab.com/alice"},
			{"id": 2, "username": "bob", "name": "Bob Jones", "state": "active", "locked": false, "avatar_url": "https://gitlab.com/avatar/2", "web_url": "https://gitlab.com/bob"}
		]`))
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "member", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "1", users[0].ProviderID)
	assert.Nil(t, users[0].LastActivityAt)

	assert.Equal(t, "2", users[1].ProviderID)
	assert.Nil(t, users[1].LastActivityAt)
}

// TestListUsersAdminResponse uses the administrator response shape (same doc
// page, "As an administrator" example), which does carry current_sign_in_at.
// Parsing still happens opportunistically for deployments that do configure an
// admin PAT -- but Capabilities().ReportsActivity stays false regardless,
// because ListUsers has no way to know which shape a given token will get
// before making the call.
func TestListUsersAdminResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Total-Pages", "1")
		w.Write([]byte(`[
			{"id": 1, "username": "alice", "name": "Alice Smith", "email": "alice@co.com", "state": "active", "is_admin": false, "last_sign_in_at": "2025-01-10T08:00:00Z", "current_sign_in_at": "2025-01-10T08:00:00Z", "last_activity_on": "2025-01-10"},
			{"id": 2, "username": "bob", "name": "Bob Jones", "email": "bob@co.com", "state": "active", "is_admin": true, "last_sign_in_at": "2025-03-01T12:30:00Z", "current_sign_in_at": "2025-03-01T12:30:00Z", "last_activity_on": "2025-03-01"}
		]`))
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	require.NotNil(t, users[0].LastActivityAt)
	assert.Equal(t, time.Date(2025, 1, 10, 8, 0, 0, 0, time.UTC), *users[0].LastActivityAt)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "admin", users[1].Role)
	require.NotNil(t, users[1].LastActivityAt)
	assert.Equal(t, time.Date(2025, 3, 1, 12, 30, 0, 0, time.UTC), *users[1].LastActivityAt)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("X-Total-Pages", "2")
		if callCount == 1 {
			assert.Equal(t, "1", r.URL.Query().Get("page"))
			json.NewEncoder(w).Encode([]apiUser{
				{ID: 1, Username: "alice", Name: "Alice", Email: "alice@co.com", State: "active"},
				{ID: 2, Username: "bob", Name: "Bob", Email: "bob@co.com", State: "active"},
			})
		} else {
			assert.Equal(t, "2", r.URL.Query().Get("page"))
			json.NewEncoder(w).Encode([]apiUser{
				{ID: 3, Username: "charlie", Name: "Charlie", Email: "charlie@co.com", State: "active"},
			})
		}
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)
	assert.Equal(t, 2, callCount)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "charlie@co.com", users[2].Email)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			w.Header().Set("X-Total-Pages", "1")
			json.NewEncoder(w).Encode([]apiUser{
				{ID: 42, Username: "alice", Name: "Alice", Email: "alice@co.com", State: "active"},
			})
		} else {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/api/v4/users/42/block", r.URL.Path)
			assert.Equal(t, "test-token", r.Header.Get("PRIVATE-TOKEN"))
			w.WriteHeader(http.StatusCreated)
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
		w.Header().Set("X-Total-Pages", "1")
		json.NewEncoder(w).Encode([]apiUser{})
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"401 Unauthorized"}`))
	}))
	defer server.Close()

	p := New("bad-token").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
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
