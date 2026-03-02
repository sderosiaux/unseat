package newrelic

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
	assert.Equal(t, "newrelic", p.Name())
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
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "test-key", r.Header.Get("Api-Key"))
		assert.Equal(t, "/v2/users.json", r.URL.Path)

		if callCount == 1 {
			json.NewEncoder(w).Encode(listUsersResponse{
				Users: []newrelicUser{
					{ID: 1001, Email: "alice@co.com", Name: "Alice", Last: "Smith", Role: "admin"},
					{ID: 1002, Email: "bob@co.com", Name: "Bob", Last: "Jones", Role: "user"},
				},
			})
		} else {
			json.NewEncoder(w).Encode(listUsersResponse{Users: []newrelicUser{}})
		}
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
	assert.Equal(t, "1001", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "Bob Jones", users[1].DisplayName)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Equal(t, "1", r.URL.Query().Get("page"))
			json.NewEncoder(w).Encode(listUsersResponse{
				Users: []newrelicUser{
					{ID: 1, Email: "u1@co.com", Name: "User", Last: "1", Role: "user"},
					{ID: 2, Email: "u2@co.com", Name: "User", Last: "2", Role: "user"},
				},
			})
		} else if callCount == 2 {
			assert.Equal(t, "2", r.URL.Query().Get("page"))
			json.NewEncoder(w).Encode(listUsersResponse{
				Users: []newrelicUser{
					{ID: 3, Email: "u3@co.com", Name: "User", Last: "3", Role: "user"},
				},
			})
		} else {
			assert.Equal(t, "3", r.URL.Query().Get("page"))
			json.NewEncoder(w).Encode(listUsersResponse{Users: []newrelicUser{}})
		}
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)
	assert.Equal(t, 3, callCount)
	assert.Equal(t, "u1@co.com", users[0].Email)
	assert.Equal(t, "u3@co.com", users[2].Email)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			if callCount == 1 {
				json.NewEncoder(w).Encode(listUsersResponse{
					Users: []newrelicUser{
						{ID: 1001, Email: "alice@co.com", Name: "Alice", Last: "Smith", Role: "user"},
					},
				})
			} else {
				json.NewEncoder(w).Encode(listUsersResponse{Users: []newrelicUser{}})
			}
			return
		}
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/v2/users/1001.json", r.URL.Path)
		assert.Equal(t, "test-key", r.Header.Get("Api-Key"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := New("test-key").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
}

func TestRemoveUserNotFound(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode(listUsersResponse{Users: []newrelicUser{}})
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
		w.Write([]byte(`{"error":{"title":"Forbidden"}}`))
	}))
	defer server.Close()

	p := New("bad-key").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
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
