package stripe

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
	assert.Equal(t, "stripe", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token")
	caps := p.Capabilities()
	assert.True(t, caps.CanAdd)
	assert.True(t, caps.CanRemove)
	assert.False(t, caps.CanSuspend)
	assert.False(t, caps.CanSetRole)
	assert.False(t, caps.HasWebhook)
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/scim/v2/Users", r.URL.Path)

		json.NewEncoder(w).Encode(scimListResponse{
			Resources: []scimUser{
				{
					ID:          "usr_1",
					UserName:    "alice@co.com",
					DisplayName: "Alice Smith",
					Emails:      []scimEmail{{Value: "alice@co.com", Primary: true}},
					Active:      true,
				},
				{
					ID:          "usr_2",
					UserName:    "bob@co.com",
					DisplayName: "Bob Jones",
					Emails:      []scimEmail{{Value: "bob@co.com", Primary: true}},
					Active:      false,
				},
			},
			TotalResults: 2,
			ItemsPerPage: 100,
			StartIndex:   1,
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
	assert.Equal(t, "usr_1", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "suspended", users[1].Status)
}

func TestListUsers_Pagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Equal(t, "1", r.URL.Query().Get("startIndex"))
			json.NewEncoder(w).Encode(scimListResponse{
				Resources: []scimUser{
					{ID: "U1", DisplayName: "User 1", Emails: []scimEmail{{Value: "u1@co.com", Primary: true}}, Active: true},
					{ID: "U2", DisplayName: "User 2", Emails: []scimEmail{{Value: "u2@co.com", Primary: true}}, Active: true},
				},
				TotalResults: 3,
				ItemsPerPage: 2,
				StartIndex:   1,
			})
		} else {
			assert.Equal(t, "3", r.URL.Query().Get("startIndex"))
			json.NewEncoder(w).Encode(scimListResponse{
				Resources: []scimUser{
					{ID: "U3", DisplayName: "User 3", Emails: []scimEmail{{Value: "u3@co.com", Primary: true}}, Active: true},
				},
				TotalResults: 3,
				ItemsPerPage: 2,
				StartIndex:   3,
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

func TestAddUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/scim/v2/Users", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/scim+json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var req scimCreateRequest
		require.NoError(t, json.Unmarshal(body, &req))
		assert.Equal(t, "alice@co.com", req.UserName)
		assert.Equal(t, "alice", req.Name.GivenName)
		assert.Equal(t, "", req.Name.FamilyName)
		assert.Len(t, req.Emails, 1)
		assert.Equal(t, "alice@co.com", req.Emails[0].Value)
		assert.True(t, req.Emails[0].Primary)
		assert.True(t, req.Active)
		assert.Contains(t, req.Schemas, "urn:ietf:params:scim:schemas:core:2.0:User")

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": "usr_new"})
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	err := p.AddUser(context.Background(), "alice@co.com", "member")
	require.NoError(t, err)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(scimListResponse{
				Resources: []scimUser{
					{ID: "usr_123", DisplayName: "Alice", Emails: []scimEmail{{Value: "alice@co.com", Primary: true}}, Active: true},
				},
				TotalResults: 1,
				ItemsPerPage: 100,
				StartIndex:   1,
			})
		} else {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/scim/v2/Users/usr_123", r.URL.Path)
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

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestListUsers_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"detail":"invalid token"}`))
	}))
	defer server.Close()

	p := New("bad-token").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "401")
}
