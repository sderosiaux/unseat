package slack

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
	p := New("test-token")
	assert.Equal(t, "slack", p.Name())
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
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/scim/v2/Users", r.URL.Path)

		json.NewEncoder(w).Encode(scimListResponse{
			Resources: []scimUser{
				{
					ID:          "U123",
					UserName:    "alice",
					DisplayName: "Alice Smith",
					Emails:      []scimEmail{{Value: "alice@co.com", Primary: true}},
					Active:      true,
					Title:       "Engineer",
				},
				{
					ID:          "U456",
					UserName:    "bob",
					DisplayName: "Bob Jones",
					Emails:      []scimEmail{{Value: "bob@co.com", Primary: true}},
					Active:      false,
					Title:       "Designer",
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
	assert.Equal(t, "U123", users[0].ProviderID)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "suspended", users[1].Status)
}

func TestListUsersPagination(t *testing.T) {
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

// Slack may omit itemsPerPage; advancing by it would pin startIndex and hang.
func TestListUsersPaginationNoItemsPerPage(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// Safety net: a regressed loop must fail the assertion below, not spin forever.
		if callCount > 5 {
			json.NewEncoder(w).Encode(scimListResponse{TotalResults: 3})
			return
		}
		switch r.URL.Query().Get("startIndex") {
		case "1":
			json.NewEncoder(w).Encode(scimListResponse{
				Resources: []scimUser{
					{ID: "U1", DisplayName: "User 1", Emails: []scimEmail{{Value: "u1@co.com", Primary: true}}, Active: true},
					{ID: "U2", DisplayName: "User 2", Emails: []scimEmail{{Value: "u2@co.com", Primary: true}}, Active: true},
				},
				TotalResults: 3,
			})
		case "3":
			json.NewEncoder(w).Encode(scimListResponse{
				Resources: []scimUser{
					{ID: "U3", DisplayName: "User 3", Emails: []scimEmail{{Value: "u3@co.com", Primary: true}}, Active: true},
				},
				TotalResults: 3,
			})
		default:
			t.Errorf("unexpected startIndex %q", r.URL.Query().Get("startIndex"))
			json.NewEncoder(w).Encode(scimListResponse{TotalResults: 3})
		}
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)
	assert.Equal(t, 2, callCount)
	assert.Equal(t, "u3@co.com", users[2].Email)
}

// An empty page while totalResults still claims more must end the crawl — and
// fail loudly. Returning the partial list would let a truncated inventory
// replace the real one in the cache, making absent users look removed.
func TestListUsersStopsOnEmptyPage(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			json.NewEncoder(w).Encode(scimListResponse{
				Resources: []scimUser{
					{ID: "U1", DisplayName: "User 1", Emails: []scimEmail{{Value: "u1@co.com", Primary: true}}, Active: true},
				},
				TotalResults: 99,
			})
			return
		}
		json.NewEncoder(w).Encode(scimListResponse{Resources: []scimUser{}, TotalResults: 99})
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pagination stalled")
	assert.Nil(t, users)
	assert.Equal(t, 2, callCount)
}

func TestListUsersDisplayNameFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(scimListResponse{
			Resources: []scimUser{
				{ID: "U1", UserName: "alice", DisplayName: "Alice Smith", Name: scimName{GivenName: "Alice", FamilyName: "Smith"}, Emails: []scimEmail{{Value: "alice@co.com"}}, Active: true},
				{ID: "U2", UserName: "bob", Name: scimName{GivenName: "Bob", FamilyName: "Jones"}, Emails: []scimEmail{{Value: "bob@co.com"}}, Active: true},
				{ID: "U3", UserName: "carol", Name: scimName{GivenName: "Carol"}, Emails: []scimEmail{{Value: "carol@co.com"}}, Active: true},
				{ID: "U4", UserName: "dave@co.com", Emails: []scimEmail{{Value: "dave@co.com"}}, Active: true},
			},
			TotalResults: 4,
		})
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 4)

	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	assert.Equal(t, "Bob Jones", users[1].DisplayName)
	assert.Equal(t, "Carol", users[2].DisplayName)
	assert.Equal(t, "dave@co.com", users[3].DisplayName)
}

func TestRemoveUser(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			assert.Equal(t, `email eq "alice@co.com"`, r.URL.Query().Get("filter"))
			assert.Equal(t, "1", r.URL.Query().Get("count"))
			json.NewEncoder(w).Encode(scimListResponse{
				Resources: []scimUser{
					{ID: "U123", DisplayName: "Alice", Emails: []scimEmail{{Value: "alice@co.com", Primary: true}}, Active: true},
				},
				TotalResults: 1,
				ItemsPerPage: 100,
				StartIndex:   1,
			})
		} else {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/scim/v2/Users/U123", r.URL.Path)
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
		json.NewEncoder(w).Encode(scimListResponse{
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
		w.Write([]byte(`{"Errors":{"description":"invalid_authentication","code":401}}`))
	}))
	defer server.Close()

	p := New("bad-token").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
	assert.Contains(t, err.Error(), "401")
}

func TestRemoveUserFallsBackToFullCrawlWhenFilterFails(t *testing.T) {
	var gets, deletes int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes++
			assert.Equal(t, "/scim/v2/Users/U456", r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		gets++
		if r.URL.Query().Get("filter") != "" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"Errors":{"description":"invalid_filter","code":400}}`))
			return
		}
		json.NewEncoder(w).Encode(scimListResponse{
			Resources: []scimUser{
				{ID: "U456", DisplayName: "Bob", Emails: []scimEmail{{Value: "bob@co.com", Primary: true}}, Active: true},
			},
			TotalResults: 1,
		})
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	require.NoError(t, p.RemoveUser(context.Background(), "bob@co.com"))
	assert.Equal(t, 2, gets)
	assert.Equal(t, 1, deletes)
}

func TestRemoveUserSurfacesCrawlError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"Errors":{"description":"not_allowed_token_type","code":403}}`))
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "bob@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "Business+")
}

func TestPlanGateErrorMessage(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			w.Write([]byte(`{"Errors":{"description":"nope"}}`))
		}))

		p := New("test-token").WithBaseURL(server.URL)
		_, err := p.ListUsers(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Business+")
		assert.Contains(t, err.Error(), "Enterprise Grid")
		assert.Contains(t, err.Error(), "admin-scoped token")
		server.Close()
	}
}

func TestNonPlanGateErrorStaysRaw(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"Errors":{"description":"ratelimited"}}`))
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
	assert.NotContains(t, err.Error(), "Business+")
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
