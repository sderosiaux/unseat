package claudecode

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
	assert.Equal(t, "claude-code", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-key")
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.False(t, caps.CanAdd)
	assert.False(t, caps.CanSuspend)
	assert.False(t, caps.CanSetRole)
}

func TestListUsersFiltersOnlyClaudeCodeUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		assert.Equal(t, "/v1/organizations/users", r.URL.Path)

		json.NewEncoder(w).Encode(apiListResponse{
			Data: []apiUser{
				{ID: "user_abc123", Email: "alice@co.com", Name: "Alice Smith", Role: "claude_code_user", AddedAt: "2025-01-15T10:00:00Z"},
				{ID: "user_def456", Email: "bob@co.com", Name: "Bob Jones", Role: "developer", AddedAt: "2025-02-10T10:00:00Z"},
				{ID: "user_ghi789", Email: "carol@co.com", Name: "Carol White", Role: "claude_code_user", AddedAt: "2025-03-01T10:00:00Z"},
				{ID: "user_jkl012", Email: "dave@co.com", Name: "Dave Black", Role: "admin", AddedAt: "2025-01-01T10:00:00Z"},
			},
			HasMore: false,
			FirstID: "user_abc123",
			LastID:  "user_jkl012",
		})
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "claude_code_user", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "user_abc123", users[0].ProviderID)

	assert.Equal(t, "carol@co.com", users[1].Email)
	assert.Equal(t, "Carol White", users[1].DisplayName)
	assert.Equal(t, "claude_code_user", users[1].Role)
	assert.Equal(t, "user_ghi789", users[1].ProviderID)
}

func TestListUsersEmptyWhenNoClaudeCodeUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(apiListResponse{
			Data: []apiUser{
				{ID: "user_1", Email: "dev@co.com", Name: "Dev", Role: "developer"},
				{ID: "user_2", Email: "admin@co.com", Name: "Admin", Role: "admin"},
			},
			HasMore: false,
		})
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	assert.Empty(t, users)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Empty(t, r.URL.Query().Get("after_id"))
			json.NewEncoder(w).Encode(apiListResponse{
				Data: []apiUser{
					{ID: "user_1", Email: "dev@co.com", Name: "Dev", Role: "developer"},
					{ID: "user_2", Email: "alice@co.com", Name: "Alice", Role: "claude_code_user"},
				},
				HasMore: true,
				LastID:  "user_2",
			})
		} else {
			assert.Equal(t, "user_2", r.URL.Query().Get("after_id"))
			json.NewEncoder(w).Encode(apiListResponse{
				Data: []apiUser{
					{ID: "user_3", Email: "bob@co.com", Name: "Bob", Role: "claude_code_user"},
					{ID: "user_4", Email: "admin@co.com", Name: "Admin", Role: "admin"},
				},
				HasMore: false,
				LastID:  "user_4",
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
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)

		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(apiListResponse{
				Data: []apiUser{
					{ID: "user_cc1", Email: "alice@co.com", Name: "Alice", Role: "claude_code_user"},
					{ID: "user_dev1", Email: "bob@co.com", Name: "Bob", Role: "developer"},
				},
				HasMore: false,
			})
		} else if r.Method == http.MethodDelete {
			assert.Equal(t, "/v1/organizations/users/user_cc1", r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"GET /v1/organizations/users",
		"DELETE /v1/organizations/users/user_cc1",
	}, calls)
}

func TestRemoveUserNotFoundWrongRole(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// User exists in org but is a developer, not a claude_code_user
		json.NewEncoder(w).Encode(apiListResponse{
			Data: []apiUser{
				{ID: "user_dev1", Email: "bob@co.com", Name: "Bob", Role: "developer"},
			},
			HasMore: false,
		})
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "bob@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRemoveUserNotFoundAtAll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(apiListResponse{
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
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"type":    "authentication_error",
				"message": "invalid api key",
			},
		})
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
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
