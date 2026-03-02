package anthropic

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
	assert.Equal(t, "anthropic", p.Name())
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
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/organizations/users", r.URL.Path)

		json.NewEncoder(w).Encode(listUsersResponse{
			Data: []apiUser{
				{ID: "user_1", Email: "alice@co.com", Name: "Alice Smith", Role: "developer", AddedAt: "2025-01-15T10:00:00Z", Type: "user"},
				{ID: "user_2", Email: "bob@co.com", Name: "Bob Jones", Role: "admin", AddedAt: "2025-02-01T10:00:00Z", Type: "user"},
			},
			HasMore: false,
			FirstID: "user_1",
			LastID:  "user_2",
		})
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "developer", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "user_1", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "admin", users[1].Role)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))

		if callCount == 1 {
			assert.Empty(t, r.URL.Query().Get("after_id"))
			json.NewEncoder(w).Encode(listUsersResponse{
				Data: []apiUser{
					{ID: "user_1", Email: "alice@co.com", Name: "Alice", Role: "developer", Type: "user"},
				},
				HasMore: true,
				FirstID: "user_1",
				LastID:  "user_1",
			})
		} else {
			assert.Equal(t, "user_1", r.URL.Query().Get("after_id"))
			json.NewEncoder(w).Encode(listUsersResponse{
				Data: []apiUser{
					{ID: "user_2", Email: "bob@co.com", Name: "Bob", Role: "admin", Type: "user"},
				},
				HasMore: false,
				FirstID: "user_2",
				LastID:  "user_2",
			})
		}
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
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
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))

		if callCount == 1 {
			// ListUsers call
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/v1/organizations/users", r.URL.Path)
			json.NewEncoder(w).Encode(listUsersResponse{
				Data: []apiUser{
					{ID: "user_abc123", Email: "alice@co.com", Name: "Alice", Role: "developer", Type: "user"},
				},
				HasMore: false,
			})
		} else {
			// DELETE call
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/v1/organizations/users/user_abc123", r.URL.Path)
			json.NewEncoder(w).Encode(map[string]string{
				"id":   "user_abc123",
				"type": "user_deleted",
			})
		}
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(listUsersResponse{
			Data:    []apiUser{},
			HasMore: false,
		})
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
		json.NewEncoder(w).Encode(apiError{
			Type:    "authentication_error",
			Message: "invalid x-api-key",
		})
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid x-api-key")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-key")
	err := p.AddUser(context.Background(), "test@co.com", "developer")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-key")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "my-admin-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		json.NewEncoder(w).Encode(listUsersResponse{Data: []apiUser{}, HasMore: false})
	}))
	defer server.Close()

	p := New("my-admin-key").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.NoError(t, err)
}
