package asana

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
	p := New("test-token", "ws-123")
	assert.Equal(t, "asana", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token", "ws-123")
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
		assert.Equal(t, "/api/1.0/workspaces/ws-123/users", r.URL.Path)
		assert.Equal(t, "email,name,resource_type", r.URL.Query().Get("opt_fields"))

		json.NewEncoder(w).Encode(listUsersResponse{
			Data: []asanaUser{
				{GID: "100", Name: "Alice Smith", Email: "alice@co.com", ResourceType: "user"},
				{GID: "200", Name: "Bob Jones", Email: "bob@co.com", ResourceType: "user"},
			},
			NextPage: nil,
		})
	}))
	defer server.Close()

	p := New("test-token", "ws-123").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "member", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "100", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "200", users[1].ProviderID)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Empty(t, r.URL.Query().Get("offset"))
			json.NewEncoder(w).Encode(listUsersResponse{
				Data: []asanaUser{
					{GID: "1", Name: "User 1", Email: "u1@co.com"},
					{GID: "2", Name: "User 2", Email: "u2@co.com"},
				},
				NextPage: &nextPage{Offset: "eyJ0eXAi"},
			})
		} else {
			assert.Equal(t, "eyJ0eXAi", r.URL.Query().Get("offset"))
			json.NewEncoder(w).Encode(listUsersResponse{
				Data: []asanaUser{
					{GID: "3", Name: "User 3", Email: "u3@co.com"},
				},
				NextPage: nil,
			})
		}
	}))
	defer server.Close()

	p := New("test-token", "ws-123").WithBaseURL(server.URL)
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
				Data: []asanaUser{
					{GID: "100", Name: "Alice", Email: "alice@co.com"},
				},
			})
			return
		}

		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/1.0/workspaces/ws-123/removeUser", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		var body map[string]map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "100", body["data"]["user"])

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	p := New("test-token", "ws-123").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(listUsersResponse{Data: []asanaUser{}})
	}))
	defer server.Close()

	p := New("test-token", "ws-123").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"errors":[{"message":"Not authorized"}]}`))
	}))
	defer server.Close()

	p := New("bad-token", "ws-123").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "403")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-token", "ws-123")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token", "ws-123")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
