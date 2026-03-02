package box

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderName(t *testing.T) {
	p := New("test-token")
	assert.Equal(t, "box", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token")
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
		assert.Equal(t, "/2.0/users", r.URL.Path)
		assert.Equal(t, "100", r.URL.Query().Get("limit"))
		assert.Equal(t, "0", r.URL.Query().Get("offset"))

		json.NewEncoder(w).Encode(boxListResponse{
			TotalCount: 2,
			Limit:      100,
			Offset:     0,
			Entries: []boxUser{
				{ID: "11111", Type: "user", Name: "Alice Smith", Login: "alice@co.com", Role: "admin", Status: "active", ModifiedAt: "2025-03-01T20:00:00Z"},
				{ID: "22222", Type: "user", Name: "Bob Jones", Login: "bob@co.com", Role: "user", Status: "active"},
			},
		})
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "admin", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "11111", users[0].ProviderID)
	require.NotNil(t, users[0].LastActivityAt)
	assert.Equal(t, time.Date(2025, 3, 1, 20, 0, 0, 0, time.UTC), *users[0].LastActivityAt)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "user", users[1].Role)
	assert.Nil(t, users[1].LastActivityAt)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Equal(t, "0", r.URL.Query().Get("offset"))
			json.NewEncoder(w).Encode(boxListResponse{
				TotalCount: 150,
				Limit:      100,
				Offset:     0,
				Entries: func() []boxUser {
					users := make([]boxUser, 100)
					for i := range users {
						users[i] = boxUser{ID: fmt.Sprintf("%d", i+1), Name: fmt.Sprintf("User %d", i+1), Login: fmt.Sprintf("u%d@co.com", i+1), Role: "user", Status: "active"}
					}
					return users
				}(),
			})
		} else {
			assert.Equal(t, "100", r.URL.Query().Get("offset"))
			json.NewEncoder(w).Encode(boxListResponse{
				TotalCount: 150,
				Limit:      100,
				Offset:     100,
				Entries: func() []boxUser {
					users := make([]boxUser, 50)
					for i := range users {
						users[i] = boxUser{ID: fmt.Sprintf("%d", i+101), Name: fmt.Sprintf("User %d", i+101), Login: fmt.Sprintf("u%d@co.com", i+101), Role: "user", Status: "active"}
					}
					return users
				}(),
			})
		}
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 150)
	assert.Equal(t, 2, callCount)
	assert.Equal(t, "u1@co.com", users[0].Email)
	assert.Equal(t, "u150@co.com", users[149].Email)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(boxListResponse{
				TotalCount: 1,
				Limit:      100,
				Offset:     0,
				Entries: []boxUser{
					{ID: "11111", Name: "Alice", Login: "alice@co.com", Role: "user", Status: "active"},
				},
			})
		} else {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/2.0/users/11111", r.URL.Path)
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(boxListResponse{
			TotalCount: 0,
			Limit:      100,
			Offset:     0,
			Entries:    []boxUser{},
		})
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"type":"error","status":401,"message":"Unauthorized"}`))
	}))
	defer server.Close()

	p := New("bad-token").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-token")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
