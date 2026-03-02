package docusign

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
	p := New("test-token", "org-123")
	assert.Equal(t, "docusign", p.Name())
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
		assert.Equal(t, "/v2/organizations/org-123/users", r.URL.Path)
		assert.Equal(t, "0", r.URL.Query().Get("start"))
		assert.Equal(t, "100", r.URL.Query().Get("take"))

		json.NewEncoder(w).Encode(usersResponse{
			Users: []dsUser{
				{ID: "u1", UserName: "Alice Smith", Email: "alice@co.com", UserStatus: "active"},
				{ID: "u2", UserName: "Bob Jones", Email: "bob@co.com", UserStatus: "active"},
				{ID: "u3", UserName: "Carol White", Email: "carol@co.com", UserStatus: "closed"},
			},
			Paging: struct {
				ResultSetSize          int `json:"result_set_size"`
				ResultSetStartPosition int `json:"result_set_start_position"`
				TotalSetSize           int `json:"total_set_size"`
			}{ResultSetSize: 3, ResultSetStartPosition: 0, TotalSetSize: 3},
		})
	}))
	defer server.Close()

	p := New("test-token", "org-123").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "u1", users[0].ProviderID)

	assert.Equal(t, "carol@co.com", users[2].Email)
	assert.Equal(t, "closed", users[2].Status)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Equal(t, "0", r.URL.Query().Get("start"))
			json.NewEncoder(w).Encode(usersResponse{
				Users: []dsUser{
					{ID: "u1", UserName: "Alice", Email: "alice@co.com", UserStatus: "active"},
				},
				Paging: struct {
					ResultSetSize          int `json:"result_set_size"`
					ResultSetStartPosition int `json:"result_set_start_position"`
					TotalSetSize           int `json:"total_set_size"`
				}{ResultSetSize: 1, ResultSetStartPosition: 0, TotalSetSize: 200},
			})
		} else {
			assert.Equal(t, "100", r.URL.Query().Get("start"))
			json.NewEncoder(w).Encode(usersResponse{
				Users: []dsUser{
					{ID: "u2", UserName: "Bob", Email: "bob@co.com", UserStatus: "active"},
				},
				Paging: struct {
					ResultSetSize          int `json:"result_set_size"`
					ResultSetStartPosition int `json:"result_set_start_position"`
					TotalSetSize           int `json:"total_set_size"`
				}{ResultSetSize: 1, ResultSetStartPosition: 100, TotalSetSize: 200},
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
			json.NewEncoder(w).Encode(usersResponse{
				Users: []dsUser{
					{ID: "u1", UserName: "Alice", Email: "alice@co.com", UserStatus: "active"},
				},
				Paging: struct {
					ResultSetSize          int `json:"result_set_size"`
					ResultSetStartPosition int `json:"result_set_start_position"`
					TotalSetSize           int `json:"total_set_size"`
				}{ResultSetSize: 1, ResultSetStartPosition: 0, TotalSetSize: 1},
			})
		} else {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/v2/organizations/org-123/users/u1/profiles", r.URL.Path)
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
		json.NewEncoder(w).Encode(usersResponse{
			Users: []dsUser{},
			Paging: struct {
				ResultSetSize          int `json:"result_set_size"`
				ResultSetStartPosition int `json:"result_set_start_position"`
				TotalSetSize           int `json:"total_set_size"`
			}{ResultSetSize: 0, ResultSetStartPosition: 0, TotalSetSize: 0},
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
		w.Write([]byte(`{"error":"invalid_token"}`))
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
