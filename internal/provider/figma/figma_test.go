package figma

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
	p := New("test-token", "tenant-1")
	assert.Equal(t, "figma", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token", "tenant-1")
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.True(t, caps.CanSuspend)
	assert.False(t, caps.CanAdd)
	assert.False(t, caps.CanSetRole)
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/Users", r.URL.Path)

		json.NewEncoder(w).Encode(scimListResponse{
			Schemas:      []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
			TotalResults: 2,
			Resources: []scimUser{
				{ID: "1001", UserName: "alice@co.com", DisplayName: "Alice Smith", Active: true, Emails: []scimEmail{{Value: "alice@co.com", Primary: true}}, Title: "Designer"},
				{ID: "1002", UserName: "bob@co.com", DisplayName: "Bob Jones", Active: false, Emails: []scimEmail{{Value: "bob@co.com", Primary: true}}, Title: "Engineer"},
			},
		})
	}))
	defer server.Close()

	p := New("test-token", "tenant-1").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "1001", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "suspended", users[1].Status)
	assert.Equal(t, "1002", users[1].ProviderID)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		startIndex := r.URL.Query().Get("startIndex")

		if startIndex == "" || startIndex == "1" {
			json.NewEncoder(w).Encode(scimListResponse{
				TotalResults: 3,
				Resources: []scimUser{
					{ID: "1", UserName: "a@co.com", DisplayName: "A", Active: true, Emails: []scimEmail{{Value: "a@co.com", Primary: true}}},
					{ID: "2", UserName: "b@co.com", DisplayName: "B", Active: true, Emails: []scimEmail{{Value: "b@co.com", Primary: true}}},
				},
			})
		} else {
			json.NewEncoder(w).Encode(scimListResponse{
				TotalResults: 3,
				Resources: []scimUser{
					{ID: "3", UserName: "c@co.com", DisplayName: "C", Active: true, Emails: []scimEmail{{Value: "c@co.com", Primary: true}}},
				},
			})
		}
	}))
	defer server.Close()

	p := New("test-token", "tenant-1").WithBaseURL(server.URL)
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
			json.NewEncoder(w).Encode(scimListResponse{
				TotalResults: 1,
				Resources: []scimUser{
					{ID: "u42", UserName: "alice@co.com", DisplayName: "Alice", Active: true, Emails: []scimEmail{{Value: "alice@co.com", Primary: true}}},
				},
			})
			return
		}

		// PATCH to deactivate
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/Users/u42", r.URL.Path)

		var body scimPatchOp
		json.NewDecoder(r.Body).Decode(&body)
		require.Len(t, body.Operations, 1)
		assert.Equal(t, "replace", body.Operations[0].Op)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"id": "u42", "active": false})
	}))
	defer server.Close()

	p := New("test-token", "tenant-1").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(scimListResponse{TotalResults: 0, Resources: []scimUser{}})
	}))
	defer server.Close()

	p := New("test-token", "tenant-1").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"detail":"Invalid token"}`)
	}))
	defer server.Close()

	p := New("bad-token", "tenant-1").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-token", "tenant-1")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token", "tenant-1")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
