package loom

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
	p := New("test-token", "space-abc")
	assert.Equal(t, "loom", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token", "space-abc")
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
		assert.Equal(t, "/v1/spaces/space-abc/members", r.URL.Path)

		json.NewEncoder(w).Encode(loomListResponse{
			Members: []loomMember{
				{ID: "m1", Email: "alice@co.com", DisplayName: "Alice Smith", Role: "admin", Status: "active"},
				{ID: "m2", Email: "bob@co.com", DisplayName: "Bob Jones", Role: "member", Status: "active"},
			},
		})
	}))
	defer server.Close()

	p := New("test-token", "space-abc").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "admin", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "m1", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "Bob Jones", users[1].DisplayName)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Empty(t, r.URL.Query().Get("page_token"))
			json.NewEncoder(w).Encode(loomListResponse{
				Members:       []loomMember{{ID: "m1", Email: "u1@co.com", DisplayName: "User 1"}},
				NextPageToken: "cursor-abc",
			})
		} else {
			assert.Equal(t, "cursor-abc", r.URL.Query().Get("page_token"))
			json.NewEncoder(w).Encode(loomListResponse{
				Members: []loomMember{{ID: "m2", Email: "u2@co.com", DisplayName: "User 2"}},
			})
		}
	}))
	defer server.Close()

	p := New("test-token", "space-abc").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)
	assert.Equal(t, 2, callCount)
	assert.Equal(t, "u1@co.com", users[0].Email)
	assert.Equal(t, "u2@co.com", users[1].Email)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(loomListResponse{
				Members: []loomMember{
					{ID: "m1", Email: "alice@co.com", DisplayName: "Alice"},
				},
			})
		} else {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/v1/spaces/space-abc/members/m1", r.URL.Path)
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	p := New("test-token", "space-abc").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(loomListResponse{Members: []loomMember{}})
	}))
	defer server.Close()

	p := New("test-token", "space-abc").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	p := New("bad-token", "space-abc").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-token", "space-abc")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token", "space-abc")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
