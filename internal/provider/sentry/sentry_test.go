package sentry

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
	p := New("test-token", "my-org")
	assert.Equal(t, "sentry", p.Name())
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
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/api/0/organizations/my-org/members/", r.URL.Path)

		json.NewEncoder(w).Encode([]sentryUser{
			{ID: "1", Email: "alice@co.com", Name: "Alice", Role: "owner", User: &struct {
				Email string `json:"email"`
				Name  string `json:"name"`
			}{Email: "alice@co.com", Name: "Alice A"}},
			{ID: "2", Email: "bob@co.com", Name: "Bob", Role: "member", Pending: true},
		})
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice A", users[0].DisplayName)
	assert.Equal(t, "owner", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "1", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "member", users[1].Role)
	assert.Equal(t, "invited", users[1].Status)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Empty(t, r.URL.Query().Get("cursor"))
			w.Header().Set("Link", `<http://example.com>; rel="previous"; results="false"; cursor="prev123", <http://example.com>; rel="next"; results="true"; cursor="next456"`)
			json.NewEncoder(w).Encode([]sentryUser{
				{ID: "1", Email: "alice@co.com", Role: "member"},
			})
		} else {
			assert.Equal(t, "next456", r.URL.Query().Get("cursor"))
			w.Header().Set("Link", `<http://example.com>; rel="previous"; results="true"; cursor="prev789", <http://example.com>; rel="next"; results="false"; cursor="next000"`)
			json.NewEncoder(w).Encode([]sentryUser{
				{ID: "2", Email: "bob@co.com", Role: "admin"},
			})
		}
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)
	assert.Equal(t, 2, callCount)
	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "bob@co.com", users[1].Email)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			json.NewEncoder(w).Encode([]sentryUser{
				{ID: "42", Email: "alice@co.com", Role: "member"},
			})
		} else {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/api/0/organizations/my-org/members/42/", r.URL.Path)
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]sentryUser{})
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestListUsersAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"detail":"Authentication credentials were not provided."}`))
	}))
	defer server.Close()

	p := New("bad-token", "my-org").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
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
