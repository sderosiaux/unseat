package trello

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
	p := New("api-key", "api-token", "my-org")
	assert.Equal(t, "trello", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("api-key", "api-token", "my-org")
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.False(t, caps.CanAdd)
	assert.False(t, caps.CanSuspend)
	assert.False(t, caps.CanSetRole)
	assert.False(t, caps.HasWebhook)
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/1/organizations/my-org/members", r.URL.Path)
		assert.Equal(t, "api-key", r.URL.Query().Get("key"))
		assert.Equal(t, "api-token", r.URL.Query().Get("token"))
		assert.Equal(t, "fullName,username,email", r.URL.Query().Get("fields"))

		json.NewEncoder(w).Encode([]trelloMember{
			{ID: "m1", FullName: "Alice Smith", Username: "alicesmith", Email: "alice@co.com"},
			{ID: "m2", FullName: "Bob Jones", Username: "bobjones", Email: "bob@co.com"},
		})
	}))
	defer server.Close()

	p := New("api-key", "api-token", "my-org").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "member", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "m1", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "m2", users[1].ProviderID)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode([]trelloMember{
				{ID: "m1", FullName: "Alice", Username: "alice", Email: "alice@co.com"},
			})
			return
		}

		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/1/organizations/my-org/members/m1", r.URL.Path)
		assert.Equal(t, "api-key", r.URL.Query().Get("key"))
		assert.Equal(t, "api-token", r.URL.Query().Get("token"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := New("api-key", "api-token", "my-org").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]trelloMember{})
	}))
	defer server.Close()

	p := New("api-key", "api-token", "my-org").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`invalid key`))
	}))
	defer server.Close()

	p := New("bad-key", "bad-token", "my-org").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("api-key", "api-token", "my-org")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("api-key", "api-token", "my-org")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
