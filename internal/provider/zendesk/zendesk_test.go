package zendesk

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
	p := New("test-token", "mycompany")
	assert.Equal(t, "zendesk", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token", "mycompany")
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
		assert.Contains(t, r.URL.Path, "/api/v2/users")

		json.NewEncoder(w).Encode(usersResponse{
			Users: []zendeskUser{
				{ID: 100, Name: "Alice Smith", Email: "alice@co.com", Role: "admin", Active: true, LastLoginAt: "2025-01-25T18:00:00Z"},
				{ID: 200, Name: "Bob Jones", Email: "bob@co.com", Role: "agent", Active: false},
			},
			Meta: struct {
				HasMore     bool   `json:"has_more"`
				AfterCursor string `json:"after_cursor"`
			}{HasMore: false},
		})
	}))
	defer server.Close()

	p := New("test-token", "mycompany").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "admin", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "100", users[0].ProviderID)
	require.NotNil(t, users[0].LastActivityAt)
	assert.Equal(t, time.Date(2025, 1, 25, 18, 0, 0, 0, time.UTC), *users[0].LastActivityAt)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "agent", users[1].Role)
	assert.Equal(t, "suspended", users[1].Status)
	assert.Nil(t, users[1].LastActivityAt)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			json.NewEncoder(w).Encode(usersResponse{
				Users: []zendeskUser{
					{ID: 100, Name: "Alice", Email: "alice@co.com", Role: "admin", Active: true},
				},
				Meta: struct {
					HasMore     bool   `json:"has_more"`
					AfterCursor string `json:"after_cursor"`
				}{HasMore: true, AfterCursor: "abc123"},
				Links: struct {
					Next string `json:"next"`
				}{Next: serverURL + "/api/v2/users?page[after]=abc123&page[size]=100"},
			})
		} else {
			json.NewEncoder(w).Encode(usersResponse{
				Users: []zendeskUser{
					{ID: 200, Name: "Bob", Email: "bob@co.com", Role: "agent", Active: true},
				},
				Meta: struct {
					HasMore     bool   `json:"has_more"`
					AfterCursor string `json:"after_cursor"`
				}{HasMore: false},
			})
		}
	}))
	serverURL = server.URL
	defer server.Close()

	p := New("test-token", "mycompany").WithBaseURL(server.URL)
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
		if callCount == 1 {
			json.NewEncoder(w).Encode(usersResponse{
				Users: []zendeskUser{
					{ID: 42, Email: "alice@co.com", Role: "agent", Active: true},
				},
			})
		} else {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/api/v2/users/42", r.URL.Path)
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"user":{"id":42,"active":false}}`))
		}
	}))
	defer server.Close()

	p := New("test-token", "mycompany").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(usersResponse{Users: []zendeskUser{}})
	}))
	defer server.Close()

	p := New("test-token", "mycompany").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestListUsersAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"Couldn't authenticate you"}`))
	}))
	defer server.Close()

	p := New("bad-token", "mycompany").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-token", "mycompany")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token", "mycompany")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
