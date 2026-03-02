package gcpiam

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
	p := New("tok", "C01234abc")
	assert.Equal(t, "gcp-iam", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("tok", "C01234abc")
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
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/admin/directory/v1/users", r.URL.Path)
		assert.Equal(t, "C01234abc", r.URL.Query().Get("customer"))

		json.NewEncoder(w).Encode(listUsersResponse{
			Users: []gcpUser{
				{
					ID:           "100",
					PrimaryEmail: "alice@co.com",
					Name:         userName{FullName: "Alice Smith"},
					Suspended:    false,
					IsAdmin:      true,
				},
				{
					ID:           "200",
					PrimaryEmail: "bob@co.com",
					Name:         userName{FullName: "Bob Jones"},
					Suspended:    true,
					IsAdmin:      false,
				},
			},
		})
	}))
	defer server.Close()

	p := New("tok", "C01234abc").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "admin", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "100", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "suspended", users[1].Status)
	assert.Equal(t, "member", users[1].Role)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Empty(t, r.URL.Query().Get("pageToken"))
			json.NewEncoder(w).Encode(listUsersResponse{
				Users: []gcpUser{
					{ID: "1", PrimaryEmail: "u1@co.com", Name: userName{FullName: "User 1"}},
					{ID: "2", PrimaryEmail: "u2@co.com", Name: userName{FullName: "User 2"}},
				},
				NextPageToken: "next-page",
			})
		} else {
			assert.Equal(t, "next-page", r.URL.Query().Get("pageToken"))
			json.NewEncoder(w).Encode(listUsersResponse{
				Users: []gcpUser{
					{ID: "3", PrimaryEmail: "u3@co.com", Name: userName{FullName: "User 3"}},
				},
			})
		}
	}))
	defer server.Close()

	p := New("tok", "C01234abc").WithBaseURL(server.URL)
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
			json.NewEncoder(w).Encode(listUsersResponse{
				Users: []gcpUser{
					{ID: "100", PrimaryEmail: "alice@co.com", Name: userName{FullName: "Alice"}},
				},
			})
		} else {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/admin/directory/v1/users/100", r.URL.Path)
			assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	p := New("tok", "C01234abc").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(listUsersResponse{Users: []gcpUser{}})
	}))
	defer server.Close()

	p := New("tok", "C01234abc").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"code":403,"message":"insufficient permission"}}`))
	}))
	defer server.Close()

	p := New("bad", "C01234abc").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "403")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("tok", "C01234abc")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("tok", "C01234abc")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
