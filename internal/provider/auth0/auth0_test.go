package auth0

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
	p := New("test-token", "mycompany.us.auth0.com")
	assert.Equal(t, "auth0", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token", "mycompany.us.auth0.com")
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
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v2/users", r.URL.Path)
		assert.Equal(t, "true", r.URL.Query().Get("include_totals"))

		json.NewEncoder(w).Encode(listUsersResponse{
			Users: []auth0User{
				{UserID: "auth0|001", Email: "alice@co.com", Name: "Alice Smith", Blocked: false},
				{UserID: "auth0|002", Email: "bob@co.com", Name: "Bob Jones", Blocked: true},
			},
			Start: 0,
			Limit: 100,
			Total: 2,
		})
	}))
	defer server.Close()

	p := New("test-token", "mycompany.us.auth0.com").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "member", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "auth0|001", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "suspended", users[1].Status)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		page := r.URL.Query().Get("page")
		perPage := r.URL.Query().Get("per_page")
		if page == "0" {
			// Return full page to trigger pagination (len == perPage and len < total).
			pp := 100
			if perPage != "" {
				fmt.Sscanf(perPage, "%d", &pp)
			}
			json.NewEncoder(w).Encode(listUsersResponse{
				Users: []auth0User{
					{UserID: "auth0|001", Email: "u1@co.com", Name: "User 1"},
					{UserID: "auth0|002", Email: "u2@co.com", Name: "User 2"},
				},
				Start: 0,
				Limit: pp,
				Total: 3,
			})
		} else {
			json.NewEncoder(w).Encode(listUsersResponse{
				Users: []auth0User{
					{UserID: "auth0|003", Email: "u3@co.com", Name: "User 3"},
				},
				Start: 2,
				Limit: 100,
				Total: 3,
			})
		}
	}))
	defer server.Close()

	p := New("test-token", "mycompany.us.auth0.com").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(listUsersResponse{
				Users: []auth0User{
					{UserID: "auth0|001", Email: "alice@co.com", Name: "Alice Smith"},
				},
				Total: 1,
			})
		} else {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Contains(t, r.URL.Path, "/api/v2/users/auth0")
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	p := New("test-token", "mycompany.us.auth0.com").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(listUsersResponse{
			Users: []auth0User{},
			Total: 0,
		})
	}))
	defer server.Close()

	p := New("test-token", "mycompany.us.auth0.com").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"statusCode":401,"error":"Unauthorized","message":"Invalid token"}`))
	}))
	defer server.Close()

	p := New("bad-token", "mycompany.us.auth0.com").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-token", "mycompany.us.auth0.com")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token", "mycompany.us.auth0.com")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
