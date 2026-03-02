package datadog

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
	p := New("api-key", "app-key")
	assert.Equal(t, "datadog", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("api-key", "app-key")
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
		assert.Equal(t, "api-key", r.Header.Get("DD-API-KEY"))
		assert.Equal(t, "app-key", r.Header.Get("DD-APPLICATION-KEY"))
		assert.Equal(t, "/api/v2/users", r.URL.Path)

		json.NewEncoder(w).Encode(listUsersResponse{
			Data: []datadogUser{
				{ID: "u1", Type: "users", Attributes: datadogUserAttributes{Name: "Alice Smith", Email: "alice@co.com", Status: "Active", Role: "admin"}},
				{ID: "u2", Type: "users", Attributes: datadogUserAttributes{Name: "Bob Jones", Email: "bob@co.com", Status: "Pending", Role: "standard"}},
				{ID: "u3", Type: "users", Attributes: datadogUserAttributes{Name: "Charlie", Email: "charlie@co.com", Disabled: true, Role: "standard"}},
			},
		})
	}))
	defer server.Close()

	p := New("api-key", "app-key").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "admin", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "u1", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "invited", users[1].Status)

	assert.Equal(t, "charlie@co.com", users[2].Email)
	assert.Equal(t, "suspended", users[2].Status)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Equal(t, "0", r.URL.Query().Get("page[number]"))
			users := make([]datadogUser, 100)
			for i := range users {
				users[i] = datadogUser{
					ID:         fmt.Sprintf("u%d", i),
					Type:       "users",
					Attributes: datadogUserAttributes{Name: fmt.Sprintf("User %d", i), Email: fmt.Sprintf("u%d@co.com", i), Status: "Active", Role: "standard"},
				}
			}
			json.NewEncoder(w).Encode(listUsersResponse{Data: users})
		} else {
			assert.Equal(t, "1", r.URL.Query().Get("page[number]"))
			json.NewEncoder(w).Encode(listUsersResponse{
				Data: []datadogUser{
					{ID: "u100", Type: "users", Attributes: datadogUserAttributes{Name: "User 100", Email: "u100@co.com", Status: "Active", Role: "standard"}},
				},
			})
		}
	}))
	defer server.Close()

	p := New("api-key", "app-key").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 101)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(listUsersResponse{
				Data: []datadogUser{
					{ID: "u1", Type: "users", Attributes: datadogUserAttributes{Name: "Alice", Email: "alice@co.com", Status: "Active", Role: "standard"}},
				},
			})
			return
		}
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/v2/users/u1", r.URL.Path)
		assert.Equal(t, "api-key", r.Header.Get("DD-API-KEY"))
		assert.Equal(t, "app-key", r.Header.Get("DD-APPLICATION-KEY"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	p := New("api-key", "app-key").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(listUsersResponse{Data: []datadogUser{}})
	}))
	defer server.Close()

	p := New("api-key", "app-key").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"errors":["Forbidden"]}`))
	}))
	defer server.Close()

	p := New("bad-key", "bad-app").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "403")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("api-key", "app-key")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("api-key", "app-key")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
