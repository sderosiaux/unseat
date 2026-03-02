package msteams

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
	p := New("tok")
	assert.Equal(t, "microsoft-teams", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("tok")
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.False(t, caps.CanAdd)
	assert.False(t, caps.CanSuspend)
	assert.False(t, caps.CanSetRole)
	assert.False(t, caps.HasWebhook)
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		assert.Equal(t, "eventual", r.Header.Get("ConsistencyLevel"))
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1.0/users", r.URL.Path)
		assert.Contains(t, r.URL.Query().Get("$filter"), teamsServicePlanID)

		json.NewEncoder(w).Encode(graphListResponse{
			Value: []graphUser{
				{
					ID:                "t-001",
					DisplayName:       "Alice Smith",
					Mail:              "alice@co.com",
					UserPrincipalName: "alice@co.onmicrosoft.com",
					AccountEnabled:    true,
				},
				{
					ID:                "t-002",
					DisplayName:       "Bob Jones",
					Mail:              "",
					UserPrincipalName: "bob@co.onmicrosoft.com",
					AccountEnabled:    false,
				},
			},
		})
	}))
	defer server.Close()

	p := New("tok").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "t-001", users[0].ProviderID)

	// Falls back to UPN when Mail is empty.
	assert.Equal(t, "bob@co.onmicrosoft.com", users[1].Email)
	assert.Equal(t, "suspended", users[1].Status)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			json.NewEncoder(w).Encode(graphListResponse{
				Value: []graphUser{
					{ID: "1", DisplayName: "User 1", Mail: "u1@co.com", AccountEnabled: true},
					{ID: "2", DisplayName: "User 2", Mail: "u2@co.com", AccountEnabled: true},
				},
				NextLink: serverURL + "/v1.0/users?$skiptoken=page2",
			})
		} else {
			json.NewEncoder(w).Encode(graphListResponse{
				Value: []graphUser{
					{ID: "3", DisplayName: "User 3", Mail: "u3@co.com", AccountEnabled: true},
				},
			})
		}
	}))
	defer server.Close()
	serverURL = server.URL

	p := New("tok").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(graphListResponse{
				Value: []graphUser{
					{ID: "t-001", DisplayName: "Alice", Mail: "alice@co.com", AccountEnabled: true},
				},
			})
		} else {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/v1.0/users/t-001", r.URL.Path)
			assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	p := New("tok").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(graphListResponse{Value: []graphUser{}})
	}))
	defer server.Close()

	p := New("tok").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"code":"InvalidAuthenticationToken","message":"token expired"}}`))
	}))
	defer server.Close()

	p := New("bad").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("tok")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("tok")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
