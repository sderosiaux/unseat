package miro

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
	p := New("test-token", "org-123")
	assert.Equal(t, "miro", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token", "org-123")
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
		assert.Equal(t, "/v2/orgs/org-123/members", r.URL.Path)

		json.NewEncoder(w).Encode(membersResponse{
			Data: []member{
				{ID: "m1", Email: "alice@co.com", Role: "admin", Active: true, License: "full", LastActivityAt: "2025-02-10T09:00:00Z"},
				{ID: "m2", Email: "bob@co.com", Role: "member", Active: true, License: "full"},
				{ID: "m3", Email: "carol@co.com", Role: "member", Active: false, License: "free"},
			},
			Cursor: "",
			Limit:  100,
			Size:   3,
			Total:  3,
		})
	}))
	defer server.Close()

	p := New("test-token", "org-123").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "alice@co.com", users[0].DisplayName)
	assert.Equal(t, "admin", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "m1", users[0].ProviderID)
	require.NotNil(t, users[0].LastActivityAt)
	assert.Equal(t, time.Date(2025, 2, 10, 9, 0, 0, 0, time.UTC), *users[0].LastActivityAt)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "member", users[1].Role)
	assert.Equal(t, "active", users[1].Status)
	assert.Nil(t, users[1].LastActivityAt)

	assert.Equal(t, "carol@co.com", users[2].Email)
	assert.Equal(t, "suspended", users[2].Status)
	assert.Nil(t, users[2].LastActivityAt)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Empty(t, r.URL.Query().Get("cursor"))
			json.NewEncoder(w).Encode(membersResponse{
				Data: []member{
					{ID: "m1", Email: "alice@co.com", Role: "admin", Active: true, License: "full"},
				},
				Cursor: "next-page",
				Limit:  1,
				Size:   1,
				Total:  2,
			})
		} else {
			assert.Equal(t, "next-page", r.URL.Query().Get("cursor"))
			json.NewEncoder(w).Encode(membersResponse{
				Data: []member{
					{ID: "m2", Email: "bob@co.com", Role: "member", Active: true, License: "full"},
				},
				Cursor: "",
				Limit:  1,
				Size:   1,
				Total:  2,
			})
		}
	}))
	defer server.Close()

	p := New("test-token", "org-123").WithBaseURL(server.URL)
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
			// ListUsers call
			json.NewEncoder(w).Encode(membersResponse{
				Data: []member{
					{ID: "m1", Email: "alice@co.com", Role: "admin", Active: true, License: "full"},
				},
			})
		} else {
			// DELETE call
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/v2/orgs/org-123/members/m1", r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	p := New("test-token", "org-123").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(membersResponse{
			Data: []member{},
		})
	}))
	defer server.Close()

	p := New("test-token", "org-123").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestListUsersAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"status":401,"message":"Unauthorized"}`))
	}))
	defer server.Close()

	p := New("bad-token", "org-123").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-token", "org-123")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token", "org-123")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
