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
	assert.True(t, caps.ReportsActivity)
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, http.MethodPost, r.Method)

		var req gqlRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Contains(t, req.Query, "includeDisabled: true")
		assert.Contains(t, req.Query, "pageInfo")
		assert.Equal(t, float64(250), req.Variables["first"])
		assert.Nil(t, req.Variables["after"])

		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"users": map[string]any{
					"nodes": []map[string]any{
						{"id": "u1", "name": "Alice", "email": "alice@co.com", "active": true, "admin": false, "guest": false, "lastSeen": "2025-01-15T10:30:00Z"},
						{"id": "u2", "name": "Bob", "email": "bob@co.com", "active": true, "admin": true, "guest": false, "lastSeen": "2025-02-20T14:00:00Z"},
						{"id": "u3", "name": "Guest User", "email": "guest@co.com", "active": false, "admin": false, "guest": true},
					},
					"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
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

func TestListUsersPaginates(t *testing.T) {
	var cursors []any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		cursors = append(cursors, req.Variables["after"])

		if req.Variables["after"] == nil {
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"users": map[string]any{
						"nodes": []map[string]any{
							{"id": "u1", "name": "Alice", "email": "alice@co.com", "active": true},
							{"id": "u2", "name": "Bob", "email": "bob@co.com", "active": true},
						},
						"pageInfo": map[string]any{"hasNextPage": true, "endCursor": "cursor-1"},
					},
				},
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"users": map[string]any{
					"nodes": []map[string]any{
						{"id": "u3", "name": "Carol", "email": "carol@co.com", "active": true},
					},
					"pageInfo": map[string]any{"hasNextPage": false, "endCursor": "cursor-2"},
				},
			},
		})
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)
	assert.Equal(t, []string{"alice@co.com", "bob@co.com", "carol@co.com"},
		[]string{users[0].Email, users[1].Email, users[2].Email})
	assert.Equal(t, []any{nil, "cursor-1"}, cursors)
}

// A truthful hasNextPage with an empty cursor would otherwise re-request page one forever.
func TestListUsersStopsOnEmptyCursor(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"users": map[string]any{
					"nodes":    []map[string]any{{"id": "u1", "name": "Alice", "email": "alice@co.com", "active": true}},
					"pageInfo": map[string]any{"hasNextPage": true, "endCursor": ""},
				},
			},
		})
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 1)
	assert.Equal(t, 1, calls)
}

func TestListUsersSkipsAppUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Contains(t, req.Query, "app")

		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"users": map[string]any{
					"nodes": []map[string]any{
						{"id": "u1", "name": "Alice", "email": "alice@co.com", "active": true, "app": false},
						{"id": "bot1", "name": "Linear Agent", "email": "agent@linear.app", "active": true, "app": true},
						{"id": "bot2", "name": "GitHub", "email": "github@app.linear", "active": true, "app": true},
					},
					"pageInfo": map[string]any{"hasNextPage": false},
				},
			},
		})
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 1)
	assert.Equal(t, "alice@co.com", users[0].Email)
}

func TestListUsersIncludesDisabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Contains(t, req.Query, "includeDisabled: true")

		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"users": map[string]any{
					"nodes": []map[string]any{
						{"id": "u1", "name": "Dana", "email": "dana@co.com", "active": false, "admin": false, "guest": false},
					},
					"pageInfo": map[string]any{"hasNextPage": false},
				},
			},
		})
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 1)
	assert.Equal(t, "suspended", users[0].Status)
	assert.Equal(t, "member", users[0].Role)
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
