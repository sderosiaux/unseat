package okta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderName(t *testing.T) {
	p := New("test-token", "dev-12345")
	assert.Equal(t, "okta", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token", "dev-12345")
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.True(t, caps.CanSuspend)
	assert.False(t, caps.CanAdd)
	assert.False(t, caps.CanSetRole)
	assert.False(t, caps.HasWebhook)
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "SSWS test-token", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/users", r.URL.Path)

		json.NewEncoder(w).Encode([]oktaUser{
			{
				ID:        "00u1",
				Status:    "ACTIVE",
				LastLogin: "2025-01-20T09:15:00Z",
				Profile: oktaUserProfile{
					Login:     "alice@co.com",
					Email:     "alice@co.com",
					FirstName: "Alice",
					LastName:  "Smith",
				},
			},
			{
				ID:     "00u2",
				Status: "DEPROVISIONED",
				Profile: oktaUserProfile{
					Login:     "bob@co.com",
					Email:     "bob@co.com",
					FirstName: "Bob",
					LastName:  "Jones",
				},
			},
		})
	}))
	defer server.Close()

	p := New("test-token", "dev-12345").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "member", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "00u1", users[0].ProviderID)
	require.NotNil(t, users[0].LastActivityAt)
	assert.Equal(t, time.Date(2025, 1, 20, 9, 15, 0, 0, time.UTC), *users[0].LastActivityAt)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "suspended", users[1].Status)
	assert.Nil(t, users[1].LastActivityAt)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Equal(t, "/api/v1/users", r.URL.Path)
			// Set Link header pointing to second page.
			nextURL := fmt.Sprintf("http://%s/api/v1/users?after=00u2&limit=200", r.Host)
			w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, nextURL))
			json.NewEncoder(w).Encode([]oktaUser{
				{ID: "00u1", Status: "ACTIVE", Profile: oktaUserProfile{Email: "u1@co.com", FirstName: "User", LastName: "1"}},
				{ID: "00u2", Status: "ACTIVE", Profile: oktaUserProfile{Email: "u2@co.com", FirstName: "User", LastName: "2"}},
			})
		} else {
			// No Link header = last page.
			json.NewEncoder(w).Encode([]oktaUser{
				{ID: "00u3", Status: "ACTIVE", Profile: oktaUserProfile{Email: "u3@co.com", FirstName: "User", LastName: "3"}},
			})
		}
	}))
	defer server.Close()

	p := New("test-token", "dev-12345").WithBaseURL(server.URL)
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
			json.NewEncoder(w).Encode([]oktaUser{
				{ID: "00u1", Status: "ACTIVE", Profile: oktaUserProfile{Email: "alice@co.com", FirstName: "Alice", LastName: "Smith"}},
			})
		} else {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/api/v1/users/00u1/lifecycle/deactivate", r.URL.Path)
			assert.Equal(t, "SSWS test-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	p := New("test-token", "dev-12345").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]oktaUser{})
	}))
	defer server.Close()

	p := New("test-token", "dev-12345").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"errorCode":"E0000011","errorSummary":"Invalid token provided"}`))
	}))
	defer server.Close()

	p := New("bad-token", "dev-12345").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-token", "dev-12345")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token", "dev-12345")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
