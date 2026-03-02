package linear

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderName(t *testing.T) {
	p := New("test-key")
	assert.Equal(t, "linear", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-key")
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.True(t, caps.CanSuspend)
	assert.False(t, caps.CanAdd)
	assert.False(t, caps.CanSetRole)
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, http.MethodPost, r.Method)

		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"users": map[string]any{
					"nodes": []map[string]any{
						{"id": "u1", "name": "Alice", "email": "alice@co.com", "active": true, "admin": false, "guest": false, "lastSeen": "2025-01-15T10:30:00Z"},
						{"id": "u2", "name": "Bob", "email": "bob@co.com", "active": true, "admin": true, "guest": false, "lastSeen": "2025-02-20T14:00:00Z"},
						{"id": "u3", "name": "Guest User", "email": "guest@co.com", "active": false, "admin": false, "guest": true},
					},
				},
			},
		})
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice", users[0].DisplayName)
	assert.Equal(t, "member", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "u1", users[0].ProviderID)
	require.NotNil(t, users[0].LastActivityAt)
	assert.Equal(t, time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC), *users[0].LastActivityAt)

	assert.Equal(t, "admin", users[1].Role)
	assert.Equal(t, "active", users[1].Status)
	require.NotNil(t, users[1].LastActivityAt)
	assert.Equal(t, time.Date(2025, 2, 20, 14, 0, 0, 0, time.UTC), *users[1].LastActivityAt)

	assert.Equal(t, "guest", users[2].Role)
	assert.Equal(t, "suspended", users[2].Status)
	assert.Nil(t, users[2].LastActivityAt)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var req gqlRequest
		json.NewDecoder(r.Body).Decode(&req)

		if callCount == 1 {
			// ListUsers call
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"users": map[string]any{
						"nodes": []map[string]any{
							{"id": "u1", "name": "Alice", "email": "alice@co.com", "active": true, "admin": false, "guest": false},
						},
					},
				},
			})
		} else {
			// userSuspend mutation
			assert.Equal(t, "u1", req.Variables["id"])
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"userSuspend": map[string]any{"success": true},
				},
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
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"users": map[string]any{
					"nodes": []map[string]any{},
				},
			},
		})
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGraphQLError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"errors": []map[string]any{
				{"message": "authentication failed"},
			},
		})
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed")
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
