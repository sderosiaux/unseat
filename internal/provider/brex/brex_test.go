package brex

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderName(t *testing.T) {
	p := New("test-token")
	assert.Equal(t, "brex", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token")
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.True(t, caps.CanSuspend)
	assert.False(t, caps.CanAdd)
	assert.False(t, caps.CanSetRole)
	assert.False(t, caps.HasWebhook)
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/v2/users", r.URL.Path)

		json.NewEncoder(w).Encode(usersResponse{
			Items: []brexUser{
				{ID: "b1", FirstName: "Alice", LastName: "Smith", Email: "alice@co.com", Status: "ACTIVE"},
				{ID: "b2", FirstName: "Bob", LastName: "Jones", Email: "bob@co.com", Status: "INVITED"},
				{ID: "b3", FirstName: "Carol", LastName: "White", Email: "carol@co.com", Status: "DISABLED"},
			},
			NextCursor: nil,
		})
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "b1", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "invited", users[1].Status)

	assert.Equal(t, "carol@co.com", users[2].Email)
	assert.Equal(t, "suspended", users[2].Status)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	nextCur := "page2cursor"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Empty(t, r.URL.Query().Get("cursor"))
			json.NewEncoder(w).Encode(usersResponse{
				Items: []brexUser{
					{ID: "b1", FirstName: "Alice", LastName: "Smith", Email: "alice@co.com", Status: "ACTIVE"},
				},
				NextCursor: &nextCur,
			})
		} else {
			assert.Equal(t, "page2cursor", r.URL.Query().Get("cursor"))
			json.NewEncoder(w).Encode(usersResponse{
				Items: []brexUser{
					{ID: "b2", FirstName: "Bob", LastName: "Jones", Email: "bob@co.com", Status: "ACTIVE"},
				},
				NextCursor: nil,
			})
		}
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
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
			json.NewEncoder(w).Encode(usersResponse{
				Items: []brexUser{
					{ID: "b1", FirstName: "Alice", LastName: "Smith", Email: "alice@co.com", Status: "ACTIVE"},
				},
				NextCursor: nil,
			})
		} else {
			assert.Equal(t, http.MethodPut, r.Method)
			assert.Equal(t, "/v2/users/b1/status", r.URL.Path)

			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			var status statusUpdate
			require.NoError(t, json.Unmarshal(body, &status))
			assert.Equal(t, "DISABLED", status.Status)

			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"DISABLED"}`))
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
		json.NewEncoder(w).Encode(usersResponse{
			Items:      []brexUser{},
			NextCursor: nil,
		})
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestListUsersAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	p := New("bad-token").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
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
