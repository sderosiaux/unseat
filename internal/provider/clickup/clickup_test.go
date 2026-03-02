package clickup

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
	assert.Equal(t, "clickup", p.Name())
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
		assert.Equal(t, "test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/api/v2/team/team-1", r.URL.Path)

		json.NewEncoder(w).Encode(getTeamResponse{
			Team: teamDetail{
				ID:   "team-1",
				Name: "My Workspace",
				Members: []teamMember{
					{User: memberUser{ID: 100, Username: "alice", Email: "alice@co.com", Role: 1}},
					{User: memberUser{ID: 200, Username: "bob", Email: "bob@co.com", Role: 3}},
					{User: memberUser{ID: 300, Username: "charlie", Email: "charlie@co.com", Role: 4}},
				},
			},
		})
	}))
	defer server.Close()

	p := New("test-token", "team-1").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "alice", users[0].DisplayName)
	assert.Equal(t, "owner", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "100", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "member", users[1].Role)

	assert.Equal(t, "charlie@co.com", users[2].Email)
	assert.Equal(t, "guest", users[2].Role)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(getTeamResponse{
				Team: teamDetail{
					ID: "team-1",
					Members: []teamMember{
						{User: memberUser{ID: 100, Username: "alice", Email: "alice@co.com", Role: 3}},
					},
				},
			})
			return
		}

		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/v2/team/team-1/user/100", r.URL.Path)
		assert.Equal(t, "test-token", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	p := New("test-token", "team-1").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(getTeamResponse{
			Team: teamDetail{ID: "team-1", Members: []teamMember{}},
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
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"err":"Token invalid","ECODE":"OAUTH_025"}`))
	}))
	defer server.Close()

	p := New("bad-token", "team-1").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "401")
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
