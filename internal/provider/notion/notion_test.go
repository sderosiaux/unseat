package notion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func encodeTestJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err)
	}
}

func TestProviderName(t *testing.T) {
	p := New("test-token")
	assert.Equal(t, "notion", p.Name())
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
		assert.Equal(t, "/scim/v2/Users", r.URL.Path)

		encodeTestJSON(w, httpclient.SCIMListResponse[scimUser]{
			Resources: []scimUser{
				{
					ID:          "u-abc",
					UserName:    "alice@co.com",
					DisplayName: "Alice Smith",
					Emails:      []scimEmail{{Value: "alice@co.com", Primary: true}},
					Active:      true,
				},
				{
					ID:          "u-def",
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
	assert.Equal(t, "u-abc", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "suspended", users[1].Status)
}

func TestListUsersPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			assert.Equal(t, "1", r.URL.Query().Get("startIndex"))
			encodeTestJSON(w, httpclient.SCIMListResponse[scimUser]{
				Resources: []scimUser{
					{ID: "u1", UserName: "u1@co.com", DisplayName: "User 1", Emails: []scimEmail{{Value: "u1@co.com", Primary: true}}, Active: true},
					{ID: "u2", UserName: "u2@co.com", DisplayName: "User 2", Emails: []scimEmail{{Value: "u2@co.com", Primary: true}}, Active: true},
				},
				TotalResults: 3,
				ItemsPerPage: 2,
				StartIndex:   1,
			})
		} else {
			assert.Equal(t, "3", r.URL.Query().Get("startIndex"))
			encodeTestJSON(w, httpclient.SCIMListResponse[scimUser]{
				Resources: []scimUser{
					{ID: "u3", UserName: "u3@co.com", DisplayName: "User 3", Emails: []scimEmail{{Value: "u3@co.com", Primary: true}}, Active: true},
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

// A page with zero Resources cannot advance startIndex; if the loop keeps
// going because totalResults is still ahead, it spins forever with no HTTP
// timeout to break it. Bounded so a regression fails the suite instead of
// hanging it.
//
// The shared SCIM walker stops AND fails here rather than returning the two
// users it did get: a truncated inventory is cached with DELETE-then-INSERT,
// so the eight missing users would look absent from Notion and get re-invited.
func TestListUsersStopsOnEmptyPage(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			encodeTestJSON(w, httpclient.SCIMListResponse[scimUser]{
				Resources: []scimUser{
					{ID: "u1", UserName: "u1@co.com", DisplayName: "User 1", Emails: []scimEmail{{Value: "u1@co.com", Primary: true}}, Active: true},
					{ID: "u2", UserName: "u2@co.com", DisplayName: "User 2", Emails: []scimEmail{{Value: "u2@co.com", Primary: true}}, Active: true},
				},
				TotalResults: 10,
				ItemsPerPage: 2,
				StartIndex:   1,
			})
			return
		}
		// Server still claims 10 total but hands back nothing.
		encodeTestJSON(w, httpclient.SCIMListResponse[scimUser]{
			Resources:    []scimUser{},
			TotalResults: 10,
			ItemsPerPage: 2,
			StartIndex:   3,
		})
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := New("test-token").WithBaseURL(server.URL)

	type listResult struct {
		users []core.User
		err   error
	}
	done := make(chan listResult, 1)
	go func() {
		users, err := p.ListUsers(ctx)
		done <- listResult{users, err}
	}()

	select {
	case res := <-done:
		require.Error(t, res.err)
		assert.Contains(t, res.err.Error(), "pagination stalled")
		assert.Empty(t, res.users, "a partial roster must not be passed off as the full one")
		assert.Equal(t, int32(2), calls.Load())
	case <-time.After(5 * time.Second):
		cancel()
		<-done // unwind the loop so httptest.Server can shut down
		t.Fatal("ListUsers never returned: pagination loop spun on an empty page")
	}
}

func TestListUsersDisplayNameFallbacks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encodeTestJSON(w, httpclient.SCIMListResponse[scimUser]{
			Resources: []scimUser{
				{ID: "u1", UserName: "a@co.com", DisplayName: "Top Level", Name: scimName{Formatted: "Ignored", GivenName: "Ig", FamilyName: "Nored"}, Active: true},
				{ID: "u2", UserName: "b@co.com", Name: scimName{Formatted: "Bea Formatted", GivenName: "Bea", FamilyName: "Parts"}, Active: true},
				{ID: "u3", UserName: "c@co.com", Name: scimName{GivenName: "Carl", FamilyName: "Parts"}, Active: true},
				{ID: "u4", UserName: "d@co.com", Name: scimName{FamilyName: "Solo"}, Active: true},
				{ID: "u5", UserName: "e@co.com", Active: true},
			},
			TotalResults: 5,
			ItemsPerPage: 100,
			StartIndex:   1,
		})
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 5)

	assert.Equal(t, "Top Level", users[0].DisplayName)
	assert.Equal(t, "Bea Formatted", users[1].DisplayName)
	assert.Equal(t, "Carl Parts", users[2].DisplayName)
	assert.Equal(t, "Solo", users[3].DisplayName)
	assert.Equal(t, "e@co.com", users[4].DisplayName)
}

func TestListUsersRoleFromNotionExtension(t *testing.T) {
	raw := `{
	  "schemas": ["urn:ietf:params:scim:api:messages:2.0:ListResponse"],
	  "totalResults": 4,
	  "itemsPerPage": 100,
	  "startIndex": 1,
	  "Resources": [
	    {"id":"u1","userName":"owner@co.com","displayName":"Owner","active":true,
	     "urn:ietf:params:scim:schemas:extension:notion:2.0:User":{"role":"owner"}},
	    {"id":"u2","userName":"admin@co.com","displayName":"Admin","active":true,
	     "urn:ietf:params:scim:schemas:extension:notion:2.0:User":{"role":"membership_admin"}},
	    {"id":"u3","userName":"guest@co.com","displayName":"Guest","active":true,
	     "urn:ietf:params:scim:schemas:extension:notion:2.0:User":{"role":"restricted_member"}},
	    {"id":"u4","userName":"plain@co.com","displayName":"Plain","active":true}
	  ]
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(raw))
		require.NoError(t, err)
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 4)

	assert.Equal(t, "owner", users[0].Role)
	assert.Equal(t, "membership_admin", users[1].Role)
	assert.Equal(t, "restricted_member", users[2].Role)
	assert.Equal(t, "member", users[3].Role, "missing extension falls back to member")
}

func TestListUsersRoleEmptyExtensionFallsBack(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encodeTestJSON(w, httpclient.SCIMListResponse[scimUser]{
			Resources: []scimUser{
				{ID: "u1", UserName: "a@co.com", DisplayName: "A", Active: true, NotionExtension: &scimNotionExtension{}},
			},
			TotalResults: 1,
			ItemsPerPage: 100,
			StartIndex:   1,
		})
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 1)
	assert.Equal(t, "member", users[0].Role)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			encodeTestJSON(w, httpclient.SCIMListResponse[scimUser]{
				Resources: []scimUser{
					{ID: "u-abc", UserName: "alice@co.com", DisplayName: "Alice", Emails: []scimEmail{{Value: "alice@co.com", Primary: true}}, Active: true},
				},
				TotalResults: 1,
				ItemsPerPage: 100,
				StartIndex:   1,
			})
		} else {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/scim/v2/Users/u-abc", r.URL.Path)
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
		encodeTestJSON(w, httpclient.SCIMListResponse[scimUser]{
			Resources:    []scimUser{},
			TotalResults: 0,
			ItemsPerPage: 100,
			StartIndex:   1,
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
		w.WriteHeader(http.StatusUnauthorized)
		_, err := w.Write([]byte(`{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"detail":"Unauthorized"}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	p := New("bad-token").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
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
