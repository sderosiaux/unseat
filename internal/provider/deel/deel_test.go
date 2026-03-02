package deel

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
	p := New("test-token")
	assert.Equal(t, "deel", p.Name())
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
		assert.Equal(t, "/rest/v2/people", r.URL.Path)

		json.NewEncoder(w).Encode(deelListResponse{
			Data: []deelPerson{
				{ID: "D1", FullName: "Alice Smith", WorkEmail: "alice@co.com", State: "active"},
				{ID: "D2", FullName: "Bob Jones", WorkEmail: "bob@co.com", State: "invited"},
			},
			Page: deelPage{Offset: 0, Limit: 100, TotalRows: 2},
		})
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "member", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "D1", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "invited", users[1].Status)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Equal(t, "0", r.URL.Query().Get("offset"))
			json.NewEncoder(w).Encode(deelListResponse{
				Data: []deelPerson{
					{ID: "D1", FullName: "User 1", WorkEmail: "u1@co.com", State: "active"},
					{ID: "D2", FullName: "User 2", WorkEmail: "u2@co.com", State: "active"},
				},
				Page: deelPage{Offset: 0, Limit: 2, TotalRows: 3},
			})
		} else {
			assert.Equal(t, "2", r.URL.Query().Get("offset"))
			json.NewEncoder(w).Encode(deelListResponse{
				Data: []deelPerson{
					{ID: "D3", FullName: "User 3", WorkEmail: "u3@co.com", State: "active"},
				},
				Page: deelPage{Offset: 2, Limit: 2, TotalRows: 3},
			})
		}
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
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
			json.NewEncoder(w).Encode(deelListResponse{
				Data: []deelPerson{
					{ID: "D1", FullName: "Alice", WorkEmail: "alice@co.com", State: "active"},
				},
				Page: deelPage{Offset: 0, Limit: 100, TotalRows: 1},
			})
		} else {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/rest/v2/people/D1/terminate", r.URL.Path)
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
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
		json.NewEncoder(w).Encode(deelListResponse{
			Data: []deelPerson{},
			Page: deelPage{Offset: 0, Limit: 100, TotalRows: 0},
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
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer server.Close()

	p := New("bad-token").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "403")
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
