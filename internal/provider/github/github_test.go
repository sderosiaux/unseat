package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
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
	// /orgs/{org}/events only carries public events, so a missing timestamp is
	// not evidence of inactivity and must not be reported as such.
	assert.False(t, caps.ReportsActivity)
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

// handleCommon answers the endpoints every ListUsers call touches but which most
// tests don't exercise: the org-visibility preflight, SAML, and the event feed.
func handleCommon(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/user/memberships/orgs/my-org":
		json.NewEncoder(w).Encode(map[string]any{"state": "active", "role": "member"})
		return true
	case "/graphql":
		noSAMLHandler(w, r)
		return true
	case "/orgs/my-org/events":
		json.NewEncoder(w).Encode([]map[string]any{})
		return true
	}
	return false
}

func TestListUsers(t *testing.T) {
	activityTime := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user/memberships/orgs/my-org" || r.URL.Path == "/graphql" {
			handleCommon(w, r)
			return
		}
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))

		switch {
		case r.URL.Path == "/orgs/my-org/members" && r.URL.Query().Get("role") == "admin":
			json.NewEncoder(w).Encode([]orgMember{{Login: "bob", ID: 102}})
		case r.URL.Path == "/orgs/my-org/members":
			json.NewEncoder(w).Encode([]orgMember{
				{Login: "alice", ID: 101},
				{Login: "bob", ID: 102},
			})
		case r.URL.Path == "/orgs/my-org/events":
			json.NewEncoder(w).Encode([]map[string]any{
				{"actor": map[string]any{"login": "alice"}, "created_at": activityTime.Format(time.RFC3339)},
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
	assert.Equal(t, "profile", users[0].Metadata["email_source"])
	require.NotNil(t, users[0].LastActivityAt)
	assert.Equal(t, activityTime, *users[0].LastActivityAt)

	assert.Equal(t, "bob@co.com", users[1].Email)
	assert.Equal(t, "Bob Jones", users[1].DisplayName)
	assert.Equal(t, "admin", users[1].Role) // listed by ?role=admin
	assert.Nil(t, users[1].LastActivityAt)  // not in org events
}

func TestListUsersNoPublicEmail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handleCommon(w, r) {
			return
		}
		switch {
		case r.URL.Path == "/orgs/my-org/members" && r.URL.Query().Get("role") == "admin":
			json.NewEncoder(w).Encode([]orgMember{})
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

	// The bare login is the contract: no invented @users.noreply.github.com.
	assert.Equal(t, "private-user", users[0].Email)
	assert.NotContains(t, users[0].Email, "@")
	assert.Equal(t, "unresolved", users[0].Metadata["email_source"])
	assert.Equal(t, "Private User", users[0].DisplayName)
}

// An unresolvable login must reach core as SeatUnresolved — the whole point of
// keeping it free of an "@".
func TestUnresolvedLoginClassifiesAsUnresolved(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handleCommon(w, r) {
			return
		}
		switch {
		case r.URL.Path == "/orgs/my-org/members" && r.URL.Query().Get("role") == "admin":
			json.NewEncoder(w).Encode([]orgMember{})
		case r.URL.Path == "/orgs/my-org/members":
			json.NewEncoder(w).Encode([]orgMember{{Login: "ghost", ID: 300}})
		case r.URL.Path == "/users/ghost":
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 1)

	seats := core.ClassifySeats(core.ClassifyInput{
		ProviderName: "github",
		ActualUsers:  users,
		Directory:    map[string]core.DirectoryUser{"someone@co.com": {}},
		Domain:       "co.com",
	})
	require.Len(t, seats, 1)
	assert.Equal(t, core.SeatUnresolved, seats[0].Class)
}

// Members absent from the public event feed must never be scored as inactive:
// the feed is public-only, so silence about them is missing data, not evidence.
func TestMissingActivityIsNotReportedAsInactive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handleCommon(w, r) {
			return
		}
		switch {
		case r.URL.Path == "/orgs/my-org/members" && r.URL.Query().Get("role") == "admin":
			json.NewEncoder(w).Encode([]orgMember{})
		case r.URL.Path == "/orgs/my-org/members":
			json.NewEncoder(w).Encode([]orgMember{{Login: "quiet", ID: 400}})
		case r.URL.Path == "/users/quiet":
			json.NewEncoder(w).Encode(map[string]any{"email": "quiet@co.com", "name": "Quiet", "login": "quiet"})
		}
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Nil(t, users[0].LastActivityAt)

	scan := core.Scan(core.ScanInput{
		Provider:          "github",
		Users:             users,
		Domain:            "co.com",
		ReportsActivity:   p.Capabilities().ReportsActivity,
		InactiveThreshold: 90 * 24 * time.Hour,
		Now:               time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	})
	kinds := make(map[core.FindingKind]bool, len(scan.Findings))
	for _, f := range scan.Findings {
		kinds[f.Kind] = true
	}
	assert.False(t, kinds[core.FindingInactive], "silence in the public event feed is not inactivity")
	assert.True(t, kinds[core.FindingNoActivityData])
}

func TestListUsersPagination(t *testing.T) {
	memberCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handleCommon(w, r) {
			return
		}
		if strings.HasPrefix(r.URL.Path, "/users/") {
			login := strings.TrimPrefix(r.URL.Path, "/users/")
			json.NewEncoder(w).Encode(map[string]any{"email": login + "@co.com", "name": login, "login": login})
			return
		}
		if r.URL.Query().Get("role") == "admin" {
			json.NewEncoder(w).Encode([]orgMember{})
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
		if r.URL.Path == "/user/memberships/orgs/my-org" || r.URL.Path == "/orgs/my-org/events" {
			handleCommon(w, r)
			return
		}
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
		case r.URL.Path == "/orgs/my-org/members" && r.URL.Query().Get("role") == "admin":
			json.NewEncoder(w).Encode([]orgMember{})
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
	assert.Equal(t, "saml", users[0].Metadata["email_source"])

	assert.Equal(t, "bob@corp.com", users[1].Email)
}

// A token that is not an org member gets HTTP 200 and a public-members-only list
// from GitHub. Reporting that as the truth would fabricate "add user" actions for
// every private member, so ListUsers must fail instead.
func TestListUsersRejectsNonMemberToken(t *testing.T) {
	var membersListed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user/memberships/orgs/my-org" {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
			return
		}
		if r.URL.Path == "/orgs/my-org/members" {
			membersListed = true
			json.NewEncoder(w).Encode([]orgMember{{Login: "public-only", ID: 1}})
		}
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot verify org membership")
	assert.False(t, membersListed, "must not fall back to the public-only member list")
}

func TestListUsersRejectsTokenWithoutOrgScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user/memberships/orgs/my-org" {
			w.Header().Set("X-OAuth-Scopes", "repo, user:email")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message":"Resource not accessible"}`))
			return
		}
		t.Errorf("unexpected call to %s", r.URL.Path)
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read:org")
	assert.Contains(t, err.Error(), "repo, user:email")
}

func TestListUsersRejectsPendingMembership(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user/memberships/orgs/my-org" {
			json.NewEncoder(w).Encode(map[string]any{"state": "pending", "role": "member"})
			return
		}
		t.Errorf("unexpected call to %s", r.URL.Path)
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pending")
}

func TestListUsersAcceptsAdminScopedToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user/memberships/orgs/my-org" {
			w.Header().Set("X-OAuth-Scopes", "admin:org, repo")
			json.NewEncoder(w).Encode(map[string]any{"state": "active", "role": "admin"})
			return
		}
		if handleCommon(w, r) {
			return
		}
		json.NewEncoder(w).Encode([]orgMember{})
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	assert.Empty(t, users)
}

func TestHasOrgReadScope(t *testing.T) {
	assert.True(t, hasOrgReadScope("admin:org, repo"))
	assert.True(t, hasOrgReadScope("read:org"))
	assert.True(t, hasOrgReadScope("repo,write:org"))
	assert.False(t, hasOrgReadScope("repo, user:email"))
	assert.False(t, hasOrgReadScope(""))
}

// /orgs/{org}/events is capped by GitHub at 300 events / 30 days, so pagination
// must stop at 3 pages even when the API keeps advertising a next link.
func TestOrgActivityStopsAtEventCap(t *testing.T) {
	eventCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/orgs/my-org/events" {
			eventCalls++
			w.Header().Set("Link", fmt.Sprintf(`<%s/orgs/my-org/events?page=99>; rel="next"`, "http://"+r.Host))
			json.NewEncoder(w).Encode([]map[string]any{
				{"actor": map[string]any{"login": fmt.Sprintf("user%d", eventCalls)}, "created_at": "2026-03-01T12:00:00Z"},
			})
			return
		}
		if handleCommon(w, r) {
			return
		}
		json.NewEncoder(w).Encode([]orgMember{})
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, eventCalls)
}

func TestRemoveUser(t *testing.T) {
	var deleteCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handleCommon(w, r) {
			return
		}
		switch {
		case r.URL.Path == "/orgs/my-org/members" && r.Method == http.MethodGet && r.URL.Query().Get("role") == "admin":
			json.NewEncoder(w).Encode([]orgMember{})
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
		if handleCommon(w, r) {
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
		if handleCommon(w, r) {
			return
		}
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

// GitHub states no price, but it states how many seats were purchased against
// how many are filled — the gap is the most expensive thing this connector can
// surface, and it is invisible from the member list.
func TestBilling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/orgs/acme", r.URL.Path)
		fmt.Fprint(w, `{"login":"acme","plan":{"name":"enterprise","seats":80,"filled_seats":41}}`)
	}))
	defer server.Close()

	b, err := New("tok", "acme").WithBaseURL(server.URL).Billing(context.Background())
	require.NoError(t, err)
	require.NotNil(t, b)

	assert.Equal(t, "enterprise", b.Plan)
	assert.Equal(t, 80, b.BilledSeats)
	assert.Equal(t, 41, b.FilledSeats)
	// An Enterprise agreement is unknowable from here: no price may be invented.
	assert.Zero(t, b.CostPerSeat)
	assert.Empty(t, b.Source)
}

// plan is only returned to org admins. A member-scoped token seeing no plan is
// an absence, not a failure — the seat findings still stand.
func TestBillingWithoutAdminAccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"login":"acme"}`)
	}))
	defer server.Close()

	b, err := New("tok", "acme").WithBaseURL(server.URL).Billing(context.Background())
	require.NoError(t, err)
	assert.Nil(t, b)
}
