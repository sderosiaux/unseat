package vercel

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
	p := New("test-token", "team-1")
	assert.Equal(t, "vercel", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token", "team-1")
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
		assert.Equal(t, "/v3/teams/team-1/members", r.URL.Path)

		json.NewEncoder(w).Encode(listMembersResponse{
			Members: []vercelMember{
				{UID: "u1", Email: "alice@co.com", Username: "alice", Name: "Alice Smith", Role: "OWNER", Confirmed: true, CreatedAt: 1700000000000},
				{UID: "u2", Email: "bob@co.com", Username: "bob", Name: "Bob Jones", Role: "MEMBER", Confirmed: false, CreatedAt: 1700000001000},
			},
			Pagination: pagination{HasNext: false, Count: 2},
		})
	}))
	defer server.Close()

	p := New("test-token", "team-1").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "OWNER", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "u1", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "invited", users[1].Status)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Empty(t, r.URL.Query().Get("until"))
			next := int64(1700000001000)
			json.NewEncoder(w).Encode(listMembersResponse{
				Members: []vercelMember{
					{UID: "u1", Email: "u1@co.com", Username: "u1", Name: "User 1", Role: "MEMBER", Confirmed: true},
					{UID: "u2", Email: "u2@co.com", Username: "u2", Name: "User 2", Role: "MEMBER", Confirmed: true},
				},
				Pagination: pagination{HasNext: true, Count: 2, Next: &next},
			})
		} else {
			assert.Equal(t, "1700000001000", r.URL.Query().Get("until"))
			json.NewEncoder(w).Encode(listMembersResponse{
				Members: []vercelMember{
					{UID: "u3", Email: "u3@co.com", Username: "u3", Name: "User 3", Role: "MEMBER", Confirmed: true},
				},
				Pagination: pagination{HasNext: false, Count: 1},
			})
		}
	}))
	defer server.Close()

	p := New("test-token", "team-1").WithBaseURL(server.URL)
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
			json.NewEncoder(w).Encode(listMembersResponse{
				Members: []vercelMember{
					{UID: "u1", Email: "alice@co.com", Username: "alice", Name: "Alice", Role: "MEMBER", Confirmed: true},
				},
				Pagination: pagination{HasNext: false, Count: 1},
			})
			return
		}

		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/v1/teams/team-1/members/u1", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"id": "team-1"})
	}))
	defer server.Close()

	p := New("test-token", "team-1").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(listMembersResponse{
			Members:    []vercelMember{},
			Pagination: pagination{HasNext: false, Count: 0},
		})
	}))
	defer server.Close()

	p := New("test-token", "team-1").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"code":"forbidden","message":"Not authorized"}}`))
	}))
	defer server.Close()

	p := New("bad-token", "team-1").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "403")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-token", "team-1")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token", "team-1")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
