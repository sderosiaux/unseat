package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderName(t *testing.T) {
	p := New("test-token", "my-org")
	assert.Equal(t, "github", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token", "my-org")
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.False(t, caps.CanAdd)
	assert.False(t, caps.CanSuspend)
	assert.False(t, caps.CanSetRole)
	assert.False(t, caps.HasWebhook)
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/orgs/my-org/members", r.URL.Path)

		json.NewEncoder(w).Encode([]orgMember{
			{Login: "alice", ID: 101},
			{Login: "bob", ID: 102},
		})
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice", users[0].Email)
	assert.Equal(t, "alice", users[0].DisplayName)
	assert.Equal(t, "member", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "101", users[0].ProviderID)

	assert.Equal(t, "bob", users[1].Email)
	assert.Equal(t, "102", users[1].ProviderID)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Equal(t, "1", r.URL.Query().Get("page"))
			// Set Link header indicating there is a next page
			w.Header().Set("Link", fmt.Sprintf(`<%s/orgs/my-org/members?page=2>; rel="next"`, "http://"+r.Host))
			json.NewEncoder(w).Encode([]orgMember{
				{Login: "alice", ID: 101},
				{Login: "bob", ID: 102},
			})
		} else {
			assert.Equal(t, "2", r.URL.Query().Get("page"))
			json.NewEncoder(w).Encode([]orgMember{
				{Login: "charlie", ID: 103},
			})
		}
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)
	assert.Equal(t, 2, callCount)

	assert.Equal(t, "alice", users[0].Email)
	assert.Equal(t, "charlie", users[2].Email)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode([]orgMember{
				{Login: "alice", ID: 101},
			})
		} else {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/orgs/my-org/members/alice", r.URL.Path)
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]orgMember{})
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer server.Close()

	p := New("bad-token", "my-org").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-token", "my-org")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token", "my-org")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
