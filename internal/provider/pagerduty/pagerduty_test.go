package pagerduty

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
	p := New("test-key")
	assert.Equal(t, "pagerduty", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-key")
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
		assert.Equal(t, "Token token=test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "/users", r.URL.Path)

		json.NewEncoder(w).Encode(listUsersResponse{
			Users: []pagerdutyUser{
				{ID: "P1", Name: "Alice Smith", Email: "alice@co.com", Role: "admin"},
				{ID: "P2", Name: "Bob Jones", Email: "bob@co.com", Role: "user"},
			},
			More:   false,
			Offset: 0,
			Limit:  100,
			Total:  2,
		})
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "admin", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "P1", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "user", users[1].Role)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Equal(t, "0", r.URL.Query().Get("offset"))
			json.NewEncoder(w).Encode(listUsersResponse{
				Users: []pagerdutyUser{
					{ID: "P1", Name: "User 1", Email: "u1@co.com", Role: "user"},
					{ID: "P2", Name: "User 2", Email: "u2@co.com", Role: "user"},
				},
				More:   true,
				Offset: 0,
				Limit:  100,
				Total:  3,
			})
		} else {
			assert.Equal(t, "100", r.URL.Query().Get("offset"))
			json.NewEncoder(w).Encode(listUsersResponse{
				Users: []pagerdutyUser{
					{ID: "P3", Name: "User 3", Email: "u3@co.com", Role: "user"},
				},
				More:   false,
				Offset: 100,
				Limit:  100,
				Total:  3,
			})
		}
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)
	assert.Equal(t, 2, callCount)
	assert.Equal(t, "u1@co.com", users[0].Email)
	assert.Equal(t, "u3@co.com", users[2].Email)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(listUsersResponse{
				Users: []pagerdutyUser{
					{ID: "P1", Name: "Alice", Email: "alice@co.com", Role: "user"},
				},
				More: false,
			})
			return
		}
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/users/P1", r.URL.Path)
		assert.Equal(t, "Token token=test-key", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(listUsersResponse{Users: []pagerdutyUser{}, More: false})
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"Invalid API key"}}`))
	}))
	defer server.Close()

	p := New("bad-key").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-key")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-key")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
