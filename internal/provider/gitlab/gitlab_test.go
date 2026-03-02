package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-token", r.Header.Get("PRIVATE-TOKEN"))
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v4/users", r.URL.Path)
		assert.Equal(t, "true", r.URL.Query().Get("active"))

		w.Header().Set("X-Total-Pages", "1")
		json.NewEncoder(w).Encode([]apiUser{
			{ID: 1, Username: "alice", Name: "Alice Smith", Email: "alice@co.com", State: "active", IsAdmin: false},
			{ID: 2, Username: "bob", Name: "Bob Jones", Email: "bob@co.com", State: "active", IsAdmin: true},
		})
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "member", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "1", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "admin", users[1].Role)
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
