package snyk

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
	p := New("test-token", "org-123")
	assert.Equal(t, "snyk", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token", "org-123")
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.False(t, caps.CanAdd)
	assert.False(t, caps.CanSuspend)
	assert.False(t, caps.CanSetRole)
	assert.False(t, caps.HasWebhook)
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "token test-token", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/org/org-123/members", r.URL.Path)

		json.NewEncoder(w).Encode([]snykMember{
			{ID: "uid-1", Username: "alice", Email: "alice@co.com", Role: "admin"},
			{ID: "uid-2", Username: "bob", Email: "bob@co.com", Role: "collaborator"},
		})
	}))
	defer server.Close()

	p := New("test-token", "org-123").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "alice", users[0].DisplayName)
	assert.Equal(t, "admin", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "uid-1", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "collaborator", users[1].Role)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode([]snykMember{
				{ID: "uid-1", Username: "alice", Email: "alice@co.com", Role: "admin"},
			})
		} else {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/v1/org/org-123/members/update/uid-1", r.URL.Path)
			assert.Equal(t, "token test-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	p := New("test-token", "org-123").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]snykMember{})
	}))
	defer server.Close()

	p := New("test-token", "org-123").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"code":401,"message":"Invalid auth token provided"}`))
	}))
	defer server.Close()

	p := New("bad-token", "org-123").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-token", "org-123")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token", "org-123")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
