package canva

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sderosiaux/unseat/internal/provider/httpclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scimListResponse is the SCIM envelope Canva returns; the connector now
// decodes it through httpclient.ListSCIM, so the tests build it explicitly.
type scimListResponse = httpclient.SCIMListResponse[scimUser]

func TestProviderName(t *testing.T) {
	p := New("test-token")
	assert.Equal(t, "canva", p.Name())
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
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/_scim/v2/Users", r.URL.Path)

		json.NewEncoder(w).Encode(scimListResponse{
			TotalResults: 3,
			StartIndex:   1,
			ItemsPerPage: 10,
			Resources: []scimUser{
				{
					ID:       "s1",
					UserName: "alice@co.com",
					Name:     scimName{GivenName: "Alice", FamilyName: "Smith"},
					Emails:   []scimEmail{{Value: "alice@co.com", Type: "work", Primary: true}},
					Active:   true,
					Role:     "admin",
				},
				{
					ID:       "s2",
					UserName: "bob@co.com",
					Name:     scimName{GivenName: "Bob", FamilyName: "Jones"},
					Emails:   []scimEmail{{Value: "bob@co.com", Type: "work", Primary: true}},
					Active:   true,
				},
				{
					ID:       "s3",
					UserName: "carol@co.com",
					Name:     scimName{GivenName: "Carol"},
					Emails:   []scimEmail{{Value: "carol@co.com", Type: "work", Primary: true}},
					Active:   false,
				},
			},
		})
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)

	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "admin", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "s1", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "member", users[1].Role)

	assert.Equal(t, "carol@co.com", users[2].Email)
	assert.Equal(t, "Carol", users[2].DisplayName)
	assert.Equal(t, "suspended", users[2].Status)
}

func page(startIndex, n, total int) scimListResponse {
	resources := make([]scimUser, n)
	for i := range resources {
		id := startIndex + i
		email := fmt.Sprintf("u%d@co.com", id)
		resources[i] = scimUser{ID: fmt.Sprintf("s%d", id), UserName: email, Emails: []scimEmail{{Value: email}}, Active: true}
	}
	return scimListResponse{TotalResults: total, StartIndex: startIndex, ItemsPerPage: n, Resources: resources}
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		assert.Equal(t, "10", r.URL.Query().Get("count"))
		if callCount == 1 {
			assert.Equal(t, "1", r.URL.Query().Get("startIndex"))
			json.NewEncoder(w).Encode(page(1, 10, 12))
		} else {
			assert.Equal(t, "11", r.URL.Query().Get("startIndex"))
			json.NewEncoder(w).Encode(page(11, 2, 12))
		}
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 12)
	assert.Equal(t, 2, callCount)
	assert.Equal(t, "u1@co.com", users[0].Email)
	assert.Equal(t, "u12@co.com", users[11].Email)
}

// A short page must advance startIndex by what the page actually contained.
// Advancing by the requested count instead skipped every user the server did
// not return, so a throttled tenant reported seats as unassigned.
func TestListUsersShortPage(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Equal(t, "1", r.URL.Query().Get("startIndex"))
			json.NewEncoder(w).Encode(page(1, 3, 5))
		} else {
			assert.Equal(t, "4", r.URL.Query().Get("startIndex"))
			json.NewEncoder(w).Encode(page(4, 2, 5))
		}
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 5)
	assert.Equal(t, 2, callCount)
}

// An empty page while the server still claims more results must fail loudly
// rather than spin forever or silently truncate the inventory.
func TestListUsersStalledPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			json.NewEncoder(w).Encode(page(1, 1, 50))
		} else {
			json.NewEncoder(w).Encode(scimListResponse{TotalResults: 50, StartIndex: 2, Resources: []scimUser{}})
		}
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pagination stalled")
	assert.Equal(t, 2, callCount)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			json.NewEncoder(w).Encode(scimListResponse{
				TotalResults: 1,
				StartIndex:   1,
				ItemsPerPage: 10,
				Resources: []scimUser{
					{ID: "s1", UserName: "alice@co.com", Emails: []scimEmail{{Value: "alice@co.com"}}, Active: true},
				},
			})
		} else {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/_scim/v2/Users/s1", r.URL.Path)
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
		json.NewEncoder(w).Encode(scimListResponse{
			TotalResults: 0,
			StartIndex:   1,
			ItemsPerPage: 10,
			Resources:    []scimUser{},
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
		w.Write([]byte(`{"detail":"Invalid token"}`))
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
