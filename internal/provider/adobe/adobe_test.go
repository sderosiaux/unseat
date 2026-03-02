package adobe

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
	p := New("test-token", "12345@AdobeOrg")
	assert.Equal(t, "adobe", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token", "12345@AdobeOrg")
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
		assert.Equal(t, "/v2/usermanagement/users/12345@AdobeOrg/0", r.URL.Path)

		json.NewEncoder(w).Encode(usersResponse{
			LastPage: true,
			Result:   "success",
			Users: []adobeUser{
				{ID: "a1", Email: "alice@co.com", FirstName: "Alice", LastName: "Smith", Status: "active", Type: "enterpriseID"},
				{ID: "a2", Email: "bob@co.com", FirstName: "Bob", LastName: "Jones", Status: "active", Type: "federatedID"},
				{ID: "a3", Email: "carol@co.com", FirstName: "Carol", LastName: "", Status: "disabled", Type: "adobeID"},
			},
		})
	}))
	defer server.Close()

	p := New("test-token", "12345@AdobeOrg").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "enterpriseID", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "a1", users[0].ProviderID)

	assert.Equal(t, "carol@co.com", users[2].Email)
	assert.Equal(t, "Carol", users[2].DisplayName)
	assert.Equal(t, "disabled", users[2].Status)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Equal(t, "/v2/usermanagement/users/12345@AdobeOrg/0", r.URL.Path)
			json.NewEncoder(w).Encode(usersResponse{
				LastPage: false,
				Result:   "success",
				Users: []adobeUser{
					{ID: "a1", Email: "alice@co.com", FirstName: "Alice", Status: "active", Type: "enterpriseID"},
				},
			})
		} else {
			assert.Equal(t, "/v2/usermanagement/users/12345@AdobeOrg/1", r.URL.Path)
			json.NewEncoder(w).Encode(usersResponse{
				LastPage: true,
				Result:   "success",
				Users: []adobeUser{
					{ID: "a2", Email: "bob@co.com", FirstName: "Bob", Status: "active", Type: "enterpriseID"},
				},
			})
		}
	}))
	defer server.Close()

	p := New("test-token", "12345@AdobeOrg").WithBaseURL(server.URL)
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
				LastPage: true,
				Result:   "success",
				Users: []adobeUser{
					{ID: "a1", Email: "alice@co.com", FirstName: "Alice", Status: "active", Type: "enterpriseID"},
				},
			})
		} else {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/v2/usermanagement/action/12345@AdobeOrg", r.URL.Path)

			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)

			var commands []actionCommand
			require.NoError(t, json.Unmarshal(body, &commands))
			require.Len(t, commands, 1)
			assert.Equal(t, "alice@co.com", commands[0].User)
			require.Len(t, commands[0].Do, 1)
			assert.NotNil(t, commands[0].Do[0].RemoveFromOrg)

			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"result":"success"}`))
		}
	}))
	defer server.Close()

	p := New("test-token", "12345@AdobeOrg").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(usersResponse{
			LastPage: true,
			Result:   "success",
			Users:    []adobeUser{},
		})
	}))
	defer server.Close()

	p := New("test-token", "12345@AdobeOrg").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestListUsersAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error_code":"403003","message":"Api Key is invalid"}`))
	}))
	defer server.Close()

	p := New("bad-token", "12345@AdobeOrg").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-token", "12345@AdobeOrg")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token", "12345@AdobeOrg")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
