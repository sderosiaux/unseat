package discord

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
	p := New("test-token", "guild-123")
	assert.Equal(t, "discord", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token", "guild-123")
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.False(t, caps.CanAdd)
	assert.False(t, caps.CanSuspend)
	assert.False(t, caps.CanSetRole)
	assert.False(t, caps.HasWebhook)
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bot test-token", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v10/guilds/guild-123/members", r.URL.Path)
		assert.Equal(t, "1000", r.URL.Query().Get("limit"))

		json.NewEncoder(w).Encode([]discordMember{
			{
				User: discordUser{ID: "111", Username: "alice", GlobalName: "Alice Smith", Email: "alice@co.com"},
				Nick: "Ali",
			},
			{
				User: discordUser{ID: "222", Username: "bob", GlobalName: "Bob Jones", Email: "bob@co.com"},
			},
		})
	}))
	defer server.Close()

	p := New("test-token", "guild-123").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Ali", users[0].DisplayName)
	assert.Equal(t, "member", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "111", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "Bob Jones", users[1].DisplayName)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Equal(t, "0", r.URL.Query().Get("after"))
			// Return exactly 1000 to trigger pagination
			members := make([]discordMember, 1000)
			for i := range members {
				id := fmt.Sprintf("%d", i+1)
				members[i] = discordMember{
					User: discordUser{ID: id, Username: fmt.Sprintf("user%d", i+1), Email: fmt.Sprintf("u%d@co.com", i+1)},
				}
			}
			json.NewEncoder(w).Encode(members)
		} else {
			assert.Equal(t, "1000", r.URL.Query().Get("after"))
			json.NewEncoder(w).Encode([]discordMember{
				{User: discordUser{ID: "1001", Username: "lastuser", Email: "last@co.com"}},
			})
		}
	}))
	defer server.Close()

	p := New("test-token", "guild-123").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 1001)
	assert.Equal(t, 2, callCount)
}

func TestListUsersNoEmail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]discordMember{
			{User: discordUser{ID: "111", Username: "alice", GlobalName: "Alice Smith"}},
		})
	}))
	defer server.Close()

	p := New("test-token", "guild-123").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 1)
	assert.Equal(t, "alice", users[0].Email) // falls back to username
	assert.Equal(t, "alice", users[0].Metadata["username"])
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode([]discordMember{
				{User: discordUser{ID: "111", Username: "alice", Email: "alice@co.com"}},
			})
		} else {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/api/v10/guilds/guild-123/members/111", r.URL.Path)
			assert.Equal(t, "Bot test-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	p := New("test-token", "guild-123").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]discordMember{})
	}))
	defer server.Close()

	p := New("test-token", "guild-123").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"code":0,"message":"401: Unauthorized"}`))
	}))
	defer server.Close()

	p := New("bad-token", "guild-123").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-token", "guild-123")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token", "guild-123")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
