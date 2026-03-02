package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderName(t *testing.T) {
	p := New("test-token", "my-org")
	assert.Equal(t, "github", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New("test-token", "my-org")
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.False(t, caps.CanAdd)
	assert.False(t, caps.CanSuspend)
	assert.False(t, caps.CanSetRole)
	assert.False(t, caps.HasWebhook)
}

func noSAMLHandler(w http.ResponseWriter, _ *http.Request) {
	json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"organization": map[string]any{
				"samlIdentityProvider": nil,
			},
		},
	})
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			noSAMLHandler(w, r)
			return
		}
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))

		switch {
		case r.URL.Path == "/orgs/my-org/members":
			json.NewEncoder(w).Encode([]orgMember{
				{Login: "alice", ID: 101},
				{Login: "bob", ID: 102},
			})
		case r.URL.Path == "/users/alice":
			json.NewEncoder(w).Encode(map[string]any{"email": "alice@co.com", "name": "Alice Smith", "login": "alice"})
		case r.URL.Path == "/users/bob":
			json.NewEncoder(w).Encode(map[string]any{"email": "bob@co.com", "name": "Bob Jones", "login": "bob"})
		}
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "member", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "101", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "Bob Jones", users[1].DisplayName)
}

func TestListUsersNoPublicEmail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			noSAMLHandler(w, r)
			return
		}
		switch {
		case r.URL.Path == "/orgs/my-org/members":
			json.NewEncoder(w).Encode([]orgMember{{Login: "private-user", ID: 200}})
		case r.URL.Path == "/users/private-user":
			json.NewEncoder(w).Encode(map[string]any{"email": nil, "name": "Private User", "login": "private-user"})
		}
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 1)
	assert.Equal(t, "private-user", users[0].Email) // falls back to login
	assert.Equal(t, "Private User", users[0].DisplayName)
}

func TestListUsersPagination(t *testing.T) {
	memberCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			noSAMLHandler(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/users/") {
			login := strings.TrimPrefix(r.URL.Path, "/users/")
			json.NewEncoder(w).Encode(map[string]any{"email": login + "@co.com", "name": login, "login": login})
			return
		}
		memberCalls++
		if memberCalls == 1 {
			w.Header().Set("Link", fmt.Sprintf(`<%s/orgs/my-org/members?page=2>; rel="next"`, "http://"+r.Host))
			json.NewEncoder(w).Encode([]orgMember{
				{Login: "alice", ID: 101},
				{Login: "bob", ID: 102},
			})
		} else {
			json.NewEncoder(w).Encode([]orgMember{
				{Login: "charlie", ID: 103},
			})
		}
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)
	assert.Equal(t, 2, memberCalls)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "charlie@co.com", users[2].Email)
}

func TestListUsersWithSAML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/graphql":
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"organization": map[string]any{
						"samlIdentityProvider": map[string]any{
							"externalIdentities": map[string]any{
								"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
								"edges": []map[string]any{
									{"node": map[string]any{
										"samlIdentity": map[string]any{"nameId": "alice@corp.com"},
										"user":         map[string]any{"login": "alice"},
									}},
									{"node": map[string]any{
										"samlIdentity": map[string]any{"nameId": "bob@corp.com"},
										"user":         map[string]any{"login": "bob"},
									}},
								},
							},
						},
					},
				},
			})
		case r.URL.Path == "/orgs/my-org/members":
			json.NewEncoder(w).Encode([]orgMember{
				{Login: "alice", ID: 101},
				{Login: "bob", ID: 102},
			})
		}
		// No /users/ calls should be made when SAML mapping is available
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice@corp.com", users[0].Email)
	assert.Equal(t, "alice", users[0].DisplayName) // SAML path uses login as display name
	assert.Equal(t, "alice", users[0].Metadata["login"])

	assert.Equal(t, "bob@corp.com", users[1].Email)
}

func TestRemoveUser(t *testing.T) {
	var deleteCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			noSAMLHandler(w, r)
			return
		}
		switch {
		case r.URL.Path == "/orgs/my-org/members" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode([]orgMember{{Login: "alice", ID: 101}})
		case strings.HasPrefix(r.URL.Path, "/users/"):
			json.NewEncoder(w).Encode(map[string]any{"email": "alice@co.com", "name": "Alice", "login": "alice"})
		case r.Method == http.MethodDelete:
			assert.Equal(t, "/orgs/my-org/members/alice", r.URL.Path)
			deleteCalled = true
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "alice@co.com")
	require.NoError(t, err)
	assert.True(t, deleteCalled)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			noSAMLHandler(w, r)
			return
		}
		if r.URL.Path == "/orgs/my-org/members" {
			json.NewEncoder(w).Encode([]orgMember{})
		}
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "nobody@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer server.Close()

	p := New("bad-token", "my-org").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "401")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New("test-token", "my-org")
	err := p.AddUser(context.Background(), "test@co.com", "member")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New("test-token", "my-org")
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
