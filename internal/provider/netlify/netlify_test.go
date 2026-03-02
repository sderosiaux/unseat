package netlify

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
	p := New("test-token", "my-team")
	assert.Equal(t, "netlify", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token", "my-team")
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
		assert.Equal(t, "/api/v1/my-team/members", r.URL.Path)

		json.NewEncoder(w).Encode([]netlifyMember{
			{ID: "m1", FullName: "Alice Smith", Email: "alice@co.com", Role: "Owner"},
			{ID: "m2", FullName: "Bob Jones", Email: "bob@co.com", Role: "Collaborator"},
		})
	}))
	defer server.Close()

	p := New("test-token", "my-team").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "Owner", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "m1", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "Collaborator", users[1].Role)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Equal(t, "1", r.URL.Query().Get("page"))
			members := make([]netlifyMember, 100)
			for i := range members {
				members[i] = netlifyMember{
					ID:       fmt.Sprintf("m%d", i),
					FullName: fmt.Sprintf("User %d", i),
					Email:    fmt.Sprintf("u%d@co.com", i),
					Role:     "Collaborator",
				}
			}
			json.NewEncoder(w).Encode(members)
		} else {
			assert.Equal(t, "2", r.URL.Query().Get("page"))
			json.NewEncoder(w).Encode([]netlifyMember{
				{ID: "m100", FullName: "User 100", Email: "u100@co.com", Role: "Collaborator"},
			})
		}
	}))
	defer server.Close()

	p := New("test-token", "my-team").WithBaseURL(server.URL)
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
			json.NewEncoder(w).Encode([]netlifyMember{
				{ID: "m1", FullName: "Alice", Email: "alice@co.com", Role: "Collaborator"},
			})
			return
		}
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/v1/my-team/members/m1", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	p := New("test-token", "my-team").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]netlifyMember{})
	}))
	defer server.Close()

	p := New("test-token", "my-team").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"code":403,"message":"Access denied"}`))
	}))
	defer server.Close()

	p := New("bad-token", "my-team").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "403")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-token", "my-team")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token", "my-team")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
