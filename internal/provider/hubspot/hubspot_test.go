package hubspot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderName(t *testing.T) {
	p := New("test-token")
	assert.Equal(t, "hubspot", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token")
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.False(t, caps.CanAdd)
	assert.False(t, caps.CanSuspend)
	assert.False(t, caps.CanSetRole)
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/settings/v3/users/", r.URL.Path)

		// Raw JSON in the shape a live portal returns. Encoding our own struct
		// would round-trip whatever we declared and hide a field that the API
		// does not actually send — which is how a non-existent lastActiveTime
		// came to flag an entire portal as inactive.
		fmt.Fprint(w, `{"results":[
		  {"id":"1","email":"alice@co.com","firstName":"Alice","lastName":"Smith",
		   "roleIds":[],"seatNames":["sales-enterprise"],"superAdmin":true},
		  {"id":"2","email":"bob@co.com","roleIds":[],"seatNames":["core"],"superAdmin":false},
		  {"id":"3","email":"carol@co.com","firstName":"Carol","roleIds":[],"seatNames":[],"superAdmin":false}
		]}`)
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "super_admin", users[0].Role)
	assert.Equal(t, core.StatusActive, users[0].Status)
	assert.Equal(t, "1", users[0].ProviderID)
	// Seat type is what HubSpot bills on, and the types differ by an order of
	// magnitude in price.
	assert.Equal(t, "sales-enterprise", users[0].Metadata["seat"])

	// No name in the payload: fall back to the email rather than showing blank.
	assert.Equal(t, "bob@co.com", users[1].DisplayName)
	assert.Equal(t, "member", users[1].Role)
	assert.Equal(t, "core", users[1].Metadata["seat"])

	assert.Equal(t, "Carol", users[2].DisplayName)
	assert.Empty(t, users[2].Metadata["seat"])

	// The endpoint carries no activity data at all, so none may be invented.
	for _, u := range users {
		assert.Nil(t, u.LastActivityAt)
	}
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		assert.Equal(t, http.MethodGet, r.Method)

		if callCount == 1 {
			assert.Empty(t, r.URL.Query().Get("after"))
			json.NewEncoder(w).Encode(usersResponse{
				Results: []hubspotUser{
					{ID: "1", Email: "alice@co.com"},
				},
				Paging: &paging{
					Next: &pagingNext{After: "100"},
				},
			})
		} else {
			assert.Equal(t, "100", r.URL.Query().Get("after"))
			json.NewEncoder(w).Encode(usersResponse{
				Results: []hubspotUser{
					{ID: "2", Email: "bob@co.com"},
				},
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
			// ListUsers call
			assert.Equal(t, http.MethodGet, r.Method)
			json.NewEncoder(w).Encode(usersResponse{
				Results: []hubspotUser{
					{ID: "42", Email: "alice@co.com"},
				},
			})
		} else {
			// DELETE call
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/settings/v3/users/42", r.URL.Path)
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
		json.NewEncoder(w).Encode(usersResponse{Results: []hubspotUser{}})
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
		w.Write([]byte(`{"status":"error","message":"Authentication credentials not found"}`))
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
