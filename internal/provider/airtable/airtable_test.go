package airtable

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
	p := New("test-token", "ent123")
	assert.Equal(t, "airtable", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token", "ent123")
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
		assert.Equal(t, "/v0/meta/enterpriseAccount/ent123/users", r.URL.Path)

		json.NewEncoder(w).Encode(airtableListResponse{
			Users: []airtableUser{
				{ID: "usr1", Email: "alice@co.com", Name: "Alice Smith", State: "active"},
				{ID: "usr2", Email: "bob@co.com", Name: "Bob Jones", State: "deactivated"},
			},
		})
	}))
	defer server.Close()

	p := New("test-token", "ent123").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "member", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "usr1", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "suspended", users[1].Status)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Empty(t, r.URL.Query().Get("offset"))
			json.NewEncoder(w).Encode(airtableListResponse{
				Users: []airtableUser{
					{ID: "usr1", Email: "u1@co.com", Name: "User 1", State: "active"},
					{ID: "usr2", Email: "u2@co.com", Name: "User 2", State: "active"},
				},
				Offset: "page2cursor",
			})
		} else {
			assert.Equal(t, "page2cursor", r.URL.Query().Get("offset"))
			json.NewEncoder(w).Encode(airtableListResponse{
				Users: []airtableUser{
					{ID: "usr3", Email: "u3@co.com", Name: "User 3", State: "active"},
				},
			})
		}
	}))
	defer server.Close()

	p := New("test-token", "ent123").WithBaseURL(server.URL)
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
			json.NewEncoder(w).Encode(airtableListResponse{
				Users: []airtableUser{
					{ID: "usr1", Email: "alice@co.com", Name: "Alice", State: "active"},
				},
			})
		} else {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/v0/meta/enterpriseAccount/ent123/users/usr1", r.URL.Path)
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	p := New("test-token", "ent123").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(airtableListResponse{
			Users: []airtableUser{},
		})
	}))
	defer server.Close()

	p := New("test-token", "ent123").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"AUTHENTICATION_REQUIRED"}`))
	}))
	defer server.Close()

	p := New("bad-token", "ent123").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "403")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-token", "ent123")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token", "ent123")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
