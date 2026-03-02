package shortcut

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
	assert.Equal(t, "shortcut", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token")
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.False(t, caps.CanAdd)
	assert.False(t, caps.CanSuspend)
	assert.False(t, caps.CanSetRole)
	assert.False(t, caps.HasWebhook)
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-token", r.Header.Get("Shortcut-Token"))
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v3/members", r.URL.Path)

		json.NewEncoder(w).Encode([]apiMember{
			{
				ID:       "mem-1",
				Profile:  apiProfile{Name: "Alice Smith", EmailAddress: "alice@co.com"},
				Role:     "admin",
				Disabled: false,
			},
			{
				ID:       "mem-2",
				Profile:  apiProfile{Name: "Bob Jones", EmailAddress: "bob@co.com", Deactivated: true},
				Role:     "member",
				Disabled: false,
			},
		})
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "admin", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "mem-1", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "suspended", users[1].Status)
}

func TestListUsersPagination(t *testing.T) {
	// Shortcut API returns all members in a single response (no pagination).
	// This test verifies the provider handles a full response correctly.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]apiMember{
			{ID: "mem-1", Profile: apiProfile{Name: "User 1", EmailAddress: "u1@co.com"}, Role: "member"},
			{ID: "mem-2", Profile: apiProfile{Name: "User 2", EmailAddress: "u2@co.com"}, Role: "member"},
			{ID: "mem-3", Profile: apiProfile{Name: "User 3", EmailAddress: "u3@co.com"}, Role: "admin"},
		})
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)

	assert.Equal(t, "u1@co.com", users[0].Email)
	assert.Equal(t, "u3@co.com", users[2].Email)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode([]apiMember{
				{ID: "mem-1", Profile: apiProfile{Name: "Alice", EmailAddress: "alice@co.com"}, Role: "member"},
			})
		} else {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/api/v3/members/mem-1", r.URL.Path)
			assert.Equal(t, "test-token", r.Header.Get("Shortcut-Token"))
			w.WriteHeader(http.StatusNoContent)
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
		json.NewEncoder(w).Encode([]apiMember{})
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
		w.Write([]byte(`{"message":"Sorry, the Shortcut-Token header was not found."}`))
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
