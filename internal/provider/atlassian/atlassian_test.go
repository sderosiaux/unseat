package atlassian

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
	p := New("test-token", "dir-123")
	assert.Equal(t, "atlassian", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token", "dir-123")
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
		assert.Equal(t, "/scim/directory/dir-123/Users", r.URL.Path)

		json.NewEncoder(w).Encode(scimListResponse{
			Resources: []scimUser{
				{
					ID:          "u-abc",
					UserName:    "alice@co.com",
					DisplayName: "Alice Smith",
					Emails:      []scimEmail{{Value: "alice@co.com", Primary: true}},
					Active:      true,
				},
				{
					ID:          "u-def",
					UserName:    "bob@co.com",
					DisplayName: "Bob Jones",
					Emails:      []scimEmail{{Value: "bob@co.com", Primary: true}},
					Active:      false,
				},
			},
			TotalResults: 2,
			ItemsPerPage: 100,
			StartIndex:   1,
		})
	}))
	defer server.Close()

	p := New("test-token", "dir-123").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "member", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "u-abc", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "suspended", users[1].Status)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Equal(t, "1", r.URL.Query().Get("startIndex"))
			json.NewEncoder(w).Encode(scimListResponse{
				Resources: []scimUser{
					{ID: "u1", UserName: "u1@co.com", DisplayName: "User 1", Emails: []scimEmail{{Value: "u1@co.com", Primary: true}}, Active: true},
					{ID: "u2", UserName: "u2@co.com", DisplayName: "User 2", Emails: []scimEmail{{Value: "u2@co.com", Primary: true}}, Active: true},
				},
				TotalResults: 3,
				ItemsPerPage: 2,
				StartIndex:   1,
			})
		} else {
			assert.Equal(t, "3", r.URL.Query().Get("startIndex"))
			json.NewEncoder(w).Encode(scimListResponse{
				Resources: []scimUser{
					{ID: "u3", UserName: "u3@co.com", DisplayName: "User 3", Emails: []scimEmail{{Value: "u3@co.com", Primary: true}}, Active: true},
				},
				TotalResults: 3,
				ItemsPerPage: 2,
				StartIndex:   3,
			})
		}
	}))
	defer server.Close()

	p := New("test-token", "dir-123").WithBaseURL(server.URL)
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
			json.NewEncoder(w).Encode(scimListResponse{
				Resources: []scimUser{
					{ID: "u-abc", UserName: "alice@co.com", DisplayName: "Alice", Emails: []scimEmail{{Value: "alice@co.com", Primary: true}}, Active: true},
				},
				TotalResults: 1,
				ItemsPerPage: 100,
				StartIndex:   1,
			})
		} else {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/scim/directory/dir-123/Users/u-abc", r.URL.Path)
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	p := New("test-token", "dir-123").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(scimListResponse{
			Resources:    []scimUser{},
			TotalResults: 0,
			ItemsPerPage: 100,
			StartIndex:   1,
		})
	}))
	defer server.Close()

	p := New("test-token", "dir-123").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"detail":"Authentication credentials were not provided."}`))
	}))
	defer server.Close()

	p := New("bad-token", "dir-123").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-token", "dir-123")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token", "dir-123")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
