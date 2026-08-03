package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/golden"
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

func encodeTestJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err)
	}
}

func noSAMLHandler(w http.ResponseWriter, _ *http.Request) {
	encodeTestJSON(w, map[string]any{
		"data": map[string]any{
			"organization": map[string]any{
				"samlIdentityProvider": nil,
			},
		},
	})
}

// handleCommon answers the endpoints every ListUsers call touches but which most
// tests don't exercise: the org-visibility preflight, SAML, the audit log, and
// the event feed. The audit log defaults to 404 (as it would for the median
// non-Enterprise org), which drives every test not explicitly about it down
// the /events fallback path — matching what these tests were written against.
func handleCommon(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/user/memberships/orgs/my-org":
		encodeTestJSON(w, map[string]any{"state": "active", "role": "member"})
		return true
	case "/graphql":
		noSAMLHandler(w, r)
		return true
	case "/orgs/my-org/audit-log":
		w.WriteHeader(http.StatusNotFound)
		return true
	case "/orgs/my-org/events":
		encodeTestJSON(w, []map[string]any{})
		return true
	case "/orgs/my-org/outside_collaborators":
		encodeTestJSON(w, []orgMember{})
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
			encodeTestJSON(w, []orgMember{{Login: "bob", ID: 102}})
		case r.URL.Path == "/orgs/my-org/members":
			encodeTestJSON(w, []orgMember{
				{Login: "alice", ID: 101},
				{Login: "bob", ID: 102},
			})
		case r.URL.Path == "/orgs/my-org/outside_collaborators":
			encodeTestJSON(w, []orgMember{})
		case r.URL.Path == "/orgs/my-org/events":
			encodeTestJSON(w, []map[string]any{
				{"actor": map[string]any{"login": "alice"}, "created_at": activityTime.Format(time.RFC3339)},
			})
		case r.URL.Path == "/users/alice":
			encodeTestJSON(w, map[string]any{"email": "alice@co.com", "name": "Alice Smith", "login": "alice"})
		case r.URL.Path == "/users/bob":
			encodeTestJSON(w, map[string]any{"email": "bob@co.com", "name": "Bob Jones", "login": "bob"})
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

func TestListUsersIncludesOutsideCollaborators(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/orgs/my-org/outside_collaborators":
			assert.Equal(t, "all", r.URL.Query().Get("filter"))
			encodeTestJSON(w, []orgMember{{Login: "contractor", ID: 501}})
		case r.URL.Path == "/orgs/my-org/members" && r.URL.Query().Get("role") == "admin":
			encodeTestJSON(w, []orgMember{})
		case r.URL.Path == "/orgs/my-org/members":
			encodeTestJSON(w, []orgMember{})
		case r.URL.Path == "/users/contractor":
			encodeTestJSON(w, map[string]any{"email": "contractor@gmail.com", "name": "Contractor", "login": "contractor"})
		default:
			if handleCommon(w, r) {
				return
			}
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 1)

	assert.Equal(t, "contractor@gmail.com", users[0].Email)
	assert.Equal(t, "outside_collaborator", users[0].Role)
	assert.Equal(t, "outside_collaborator", users[0].Metadata["github_access_kind"])
	assert.Equal(t, "contractor", users[0].Metadata["login"])
}

func TestListUsersNoPublicEmail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handleCommon(w, r) {
			return
		}
		switch {
		case r.URL.Path == "/orgs/my-org/members" && r.URL.Query().Get("role") == "admin":
			encodeTestJSON(w, []orgMember{})
		case r.URL.Path == "/orgs/my-org/members":
			encodeTestJSON(w, []orgMember{{Login: "private-user", ID: 200}})
		case r.URL.Path == "/users/private-user":
			encodeTestJSON(w, map[string]any{"email": nil, "name": "Private User", "login": "private-user"})
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
			encodeTestJSON(w, []orgMember{})
		case r.URL.Path == "/orgs/my-org/members":
			encodeTestJSON(w, []orgMember{{Login: "ghost", ID: 300}})
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
			encodeTestJSON(w, []orgMember{})
		case r.URL.Path == "/orgs/my-org/members":
			encodeTestJSON(w, []orgMember{{Login: "quiet", ID: 400}})
		case r.URL.Path == "/users/quiet":
			encodeTestJSON(w, map[string]any{"email": "quiet@co.com", "name": "Quiet", "login": "quiet"})
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
			encodeTestJSON(w, map[string]any{"email": login + "@co.com", "name": login, "login": login})
			return
		}
		if r.URL.Query().Get("role") == "admin" {
			encodeTestJSON(w, []orgMember{})
			return
		}
		memberCalls++
		if memberCalls == 1 {
			w.Header().Set("Link", fmt.Sprintf(`<%s/orgs/my-org/members?page=2>; rel="next"`, "http://"+r.Host))
			encodeTestJSON(w, []orgMember{
				{Login: "alice", ID: 101},
				{Login: "bob", ID: 102},
			})
		} else {
			encodeTestJSON(w, []orgMember{
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
			encodeTestJSON(w, map[string]any{
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
			encodeTestJSON(w, []orgMember{})
		case r.URL.Path == "/orgs/my-org/members":
			encodeTestJSON(w, []orgMember{
				{Login: "alice", ID: 101},
				{Login: "bob", ID: 102},
			})
		case r.URL.Path == "/orgs/my-org/outside_collaborators":
			encodeTestJSON(w, []orgMember{})
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
			_, err := w.Write([]byte(`{"message":"Not Found"}`))
			require.NoError(t, err)
			return
		}
		if r.URL.Path == "/orgs/my-org/members" {
			membersListed = true
			require.NoError(t, json.NewEncoder(w).Encode([]orgMember{{Login: "public-only", ID: 1}}))
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
			_, err := w.Write([]byte(`{"message":"Resource not accessible"}`))
			require.NoError(t, err)
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
			encodeTestJSON(w, map[string]any{"state": "pending", "role": "member"})
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
			encodeTestJSON(w, map[string]any{"state": "active", "role": "admin"})
			return
		}
		if handleCommon(w, r) {
			return
		}
		encodeTestJSON(w, []orgMember{})
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
			encodeTestJSON(w, []map[string]any{
				{"actor": map[string]any{"login": fmt.Sprintf("user%d", eventCalls)}, "created_at": "2026-03-01T12:00:00Z"},
			})
			return
		}
		if handleCommon(w, r) {
			return
		}
		encodeTestJSON(w, []orgMember{})
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
			encodeTestJSON(w, []orgMember{})
		case r.URL.Path == "/orgs/my-org/members" && r.Method == http.MethodGet:
			encodeTestJSON(w, []orgMember{{Login: "alice", ID: 101}})
		case strings.HasPrefix(r.URL.Path, "/users/"):
			encodeTestJSON(w, map[string]any{"email": "alice@co.com", "name": "Alice", "login": "alice"})
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

func TestRemoveOutsideCollaborator(t *testing.T) {
	var deleteCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/orgs/my-org/outside_collaborators" && r.Method == http.MethodGet:
			encodeTestJSON(w, []orgMember{{Login: "contractor", ID: 501}})
		case r.URL.Path == "/orgs/my-org/members" && r.Method == http.MethodGet:
			encodeTestJSON(w, []orgMember{})
		case r.URL.Path == "/users/contractor":
			encodeTestJSON(w, map[string]any{"email": "contractor@gmail.com", "name": "Contractor", "login": "contractor"})
		case r.Method == http.MethodDelete:
			assert.Equal(t, "/orgs/my-org/outside_collaborators/contractor", r.URL.Path)
			deleteCalled = true
			w.WriteHeader(http.StatusNoContent)
		default:
			if handleCommon(w, r) {
				return
			}
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	err := p.RemoveUser(context.Background(), "contractor@gmail.com")
	require.NoError(t, err)
	assert.True(t, deleteCalled)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handleCommon(w, r) {
			return
		}
		if r.URL.Path == "/orgs/my-org/members" {
			encodeTestJSON(w, []orgMember{})
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
		_, err := w.Write([]byte(`{"message":"Bad credentials"}`))
		require.NoError(t, err)
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
		_, err := fmt.Fprint(w, `{"login":"acme","plan":{"name":"enterprise","seats":80,"filled_seats":41}}`)
		require.NoError(t, err)
	}))
	defer server.Close()

	b, err := New("tok", "acme").WithBaseURL(server.URL).Billing(context.Background())
	require.NoError(t, err)
	require.NotNil(t, b)

	assert.Equal(t, "enterprise", b.Plan)
	require.NotNil(t, b.BilledSeats)
	assert.Equal(t, 80, *b.BilledSeats)
	require.NotNil(t, b.FilledSeats)
	assert.Equal(t, 41, *b.FilledSeats)
	// An Enterprise agreement is unknowable from here: no price may be invented.
	assert.Nil(t, b.CostPerSeatMinor)
	assert.Equal(t, core.BillingSourceAPISeatCount, b.Source)
	assert.Equal(t, core.BillingConfidencePartial, b.Confidence)
	assert.NotEmpty(t, b.UnavailableReason)
}

// plan is only returned to org admins. A member-scoped token seeing no plan is
// an absence, not a failure — the seat findings still stand.
func TestBillingWithoutAdminAccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := fmt.Fprint(w, `{"login":"acme"}`)
		require.NoError(t, err)
	}))
	defer server.Close()

	b, err := New("tok", "acme").WithBaseURL(server.URL).Billing(context.Background())
	require.NoError(t, err)
	assert.Nil(t, b)
}

// The audit log's created_at is milliseconds since epoch — every other
// timestamp this connector parses (events, memberships) is RFC3339 seconds.
// Parsing it as seconds lands the timestamp in the year 58547; confirmed
// against the live tenant, this is the whole reason fetchOrgAuditLog uses
// time.UnixMilli instead of json's default time.Time unmarshalling.
func TestAuditLogParsesMillisecondTimestamp(t *testing.T) {
	wantTime := time.Date(2026, 6, 15, 9, 30, 0, 0, time.UTC)
	wantMillis := wantTime.UnixMilli()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// audit-log is checked before handleCommon: handleCommon's own case for
		// this path answers 404 (the "no Enterprise" default every other test
		// relies on), which would shadow the 200 this test needs to serve.
		if r.URL.Path == "/orgs/my-org/audit-log" {
			_, err := fmt.Fprintf(w, `[{"actor":"alice","action":"org.add_member","created_at":%d}]`, wantMillis)
			require.NoError(t, err)
			return
		}
		if handleCommon(w, r) {
			return
		}
		switch {
		case r.URL.Path == "/orgs/my-org/members" && r.URL.Query().Get("role") == "admin":
			encodeTestJSON(w, []orgMember{})
		case r.URL.Path == "/orgs/my-org/members":
			encodeTestJSON(w, []orgMember{{Login: "alice", ID: 101}})
		case r.URL.Path == "/users/alice":
			encodeTestJSON(w, map[string]any{"email": "alice@co.com", "name": "Alice", "login": "alice"})
		}
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 1)

	require.NotNil(t, users[0].LastActivityAt)
	assert.True(t, wantTime.Equal(*users[0].LastActivityAt),
		"want %s, got %s — a seconds-parse of this millisecond value would land around year 58547", wantTime, *users[0].LastActivityAt)
	assert.True(t, p.Capabilities().ReportsActivity, "a 200 from the audit log must flip ReportsActivity on")
}

// Multiple audit-log entries for the same actor must reduce to the single
// most recent timestamp, regardless of what order the pages return them in.
func TestAuditLogMostRecentWinsPerActor(t *testing.T) {
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// See TestAuditLogParsesMillisecondTimestamp for why audit-log is
		// checked before handleCommon.
		if r.URL.Path == "/orgs/my-org/audit-log" {
			// order=desc is requested, so the newest entry is first and only
			// that one is read. The trailing older entry proves the connector
			// takes entries[0] rather than the last it happens to decode.
			assert.Equal(t, "desc", r.URL.Query().Get("order"))
			assert.Contains(t, r.URL.Query().Get("phrase"), "actor:alice")
			_, err := fmt.Fprintf(w, `[{"actor":"alice","action":"org.add_member","created_at":%d},{"actor":"Alice","action":"repo.access","created_at":%d}]`,
				newer.UnixMilli(), older.UnixMilli())
			require.NoError(t, err)
			return
		}
		if handleCommon(w, r) {
			return
		}
		switch {
		case r.URL.Path == "/orgs/my-org/members" && r.URL.Query().Get("role") == "admin":
			encodeTestJSON(w, []orgMember{})
		case r.URL.Path == "/orgs/my-org/members":
			encodeTestJSON(w, []orgMember{{Login: "alice", ID: 101}})
		case r.URL.Path == "/users/alice":
			encodeTestJSON(w, map[string]any{"email": "alice@co.com", "name": "Alice", "login": "alice"})
		}
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 1)

	require.NotNil(t, users[0].LastActivityAt)
	assert.True(t, newer.Equal(*users[0].LastActivityAt), "must keep the most recent of the two entries, not the first seen")
}

// The audit log is Enterprise-only: a 403 must not be treated as "org has no
// activity". The connector must fall back to the public events feed and keep
// ReportsActivity false, exactly as if the audit log were never attempted.
func TestAuditLog403FallsBackToEventsAndReportsActivityStaysFalse(t *testing.T) {
	activityTime := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user/memberships/orgs/my-org":
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"state": "active", "role": "member"}))
		case "/graphql":
			noSAMLHandler(w, r)
		case "/orgs/my-org/audit-log":
			w.WriteHeader(http.StatusForbidden)
			_, err := w.Write([]byte(`{"message":"Audit log access requires a GitHub Enterprise Cloud plan"}`))
			require.NoError(t, err)
		case "/orgs/my-org/events":
			require.NoError(t, json.NewEncoder(w).Encode([]map[string]any{
				{"actor": map[string]any{"login": "alice"}, "created_at": activityTime.Format(time.RFC3339)},
			}))
		case "/orgs/my-org/members":
			if r.URL.Query().Get("role") == "admin" {
				require.NoError(t, json.NewEncoder(w).Encode([]orgMember{}))
				return
			}
			require.NoError(t, json.NewEncoder(w).Encode([]orgMember{{Login: "alice", ID: 101}}))
		case "/orgs/my-org/outside_collaborators":
			require.NoError(t, json.NewEncoder(w).Encode([]orgMember{}))
		case "/users/alice":
			encodeTestJSON(w, map[string]any{"email": "alice@co.com", "name": "Alice", "login": "alice"})
		}
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 1)

	// The fallback feed still found alice's event — so LastActivityAt is set —
	// but the *source* was the untrustworthy public feed, so the connector must
	// not upgrade its claim about what it knows.
	require.NotNil(t, users[0].LastActivityAt)
	assert.True(t, activityTime.Equal(*users[0].LastActivityAt))
	assert.False(t, p.Capabilities().ReportsActivity, "403 from the audit log must not be reported as activity coverage")
}

// A 404 (the status a non-Enterprise org's audit-log endpoint actually
// returns, as opposed to 403 for a missing scope) must fall back identically.
func TestAuditLog404FallsBackAndReportsActivityStaysFalse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/orgs/my-org/audit-log" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if handleCommon(w, r) {
			return
		}
		encodeTestJSON(w, []orgMember{})
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	assert.False(t, p.Capabilities().ReportsActivity)
}

// Every member is asked about individually, and nobody is skipped.
//
// A single bulk walk was measured on a live org to resolve six members out of
// thirty-nine for ten requests, the rest of the budget going to CI bots. Bot
// noise scales with repositories while a page budget does not, so the walk
// converged on answering nothing while still costing its full price.
func TestAuditLogQueriesEveryMemberIndividually(t *testing.T) {
	var mu sync.Mutex
	asked := map[string]bool{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/orgs/my-org/audit-log" {
			phrase := r.URL.Query().Get("phrase")
			// include=all: the default include=web omits Git operations, so a
			// member who only pushes would look idle.
			assert.Equal(t, "all", r.URL.Query().Get("include"))

			mu.Lock()
			for _, login := range []string{"alice", "bob", "carol"} {
				if strings.Contains(phrase, "actor:"+login) {
					asked[login] = true
				}
			}
			mu.Unlock()

			if strings.Contains(phrase, "actor:carol") {
				_, err := fmt.Fprint(w, `[]`) // genuinely nothing in the window
				require.NoError(t, err)
				return
			}
			_, err := fmt.Fprintf(w, `[{"actor":"x","action":"repo.access","created_at":%d}]`, time.Now().UnixMilli())
			require.NoError(t, err)
			return
		}
		if handleCommon(w, r) {
			return
		}
		switch {
		case r.URL.Path == "/orgs/my-org/members" && r.URL.Query().Get("role") == "admin":
			encodeTestJSON(w, []orgMember{})
		case r.URL.Path == "/orgs/my-org/members":
			encodeTestJSON(w, []orgMember{
				{Login: "alice", ID: 1}, {Login: "bob", ID: 2}, {Login: "carol", ID: 3},
			})
		default:
			encodeTestJSON(w, map[string]any{})
		}
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, map[string]bool{"alice": true, "bob": true, "carol": true}, asked,
		"a member nobody asked about would be reported idle on no evidence")
	assert.True(t, p.Capabilities().ReportsActivity)
}

// TestListUsersGoldenMembersShape replays the real /orgs/{org}/members
// response shape (internal/provider/golden). orgMember only reads login, id
// and html_url; this proves those three survive decoding of the actual
// vendor payload rather than a mock we wrote to match our own struct.
func TestListUsersGoldenMembersShape(t *testing.T) {
	membersBody := golden.Load(t, "github-members.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handleCommon(w, r) {
			return
		}
		switch {
		case r.URL.Path == "/orgs/my-org/members" && r.URL.Query().Get("role") == "admin":
			require.NoError(t, json.NewEncoder(w).Encode([]orgMember{}))
		case r.URL.Path == "/orgs/my-org/members":
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write(membersBody)
			require.NoError(t, err)
		case strings.HasPrefix(r.URL.Path, "/users/"):
			// A real org token overwhelmingly sees no public email on members;
			// the bare login is the contract (see ListUsers).
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	p := New("test-token", "my-org").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, "alice-example", users[0].Email) // bare login: no public email resolved
	assert.Equal(t, "alice-example", users[0].DisplayName)
	assert.Equal(t, "member", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	assert.Equal(t, "10000001", users[0].ProviderID)
	assert.Equal(t, "unresolved", users[0].Metadata["email_source"])
	assert.Nil(t, users[0].LastActivityAt) // no org event for this login

	assert.Equal(t, "bob-example", users[1].Email)
	assert.Equal(t, "10000002", users[1].ProviderID)
}

// TestBillingGoldenOrgShape replays the real /orgs/{org} response shape
// (internal/provider/golden), which carries dozens of fields Billing never
// reads. Only plan.name / plan.seats / plan.filled_seats matter here — this
// proves they decode correctly out of the full real payload, not a
// hand-picked subset.
func TestBillingGoldenOrgShape(t *testing.T) {
	body := golden.Load(t, "github-org.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/orgs/my-org", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write(body)
		require.NoError(t, err)
	}))
	defer server.Close()

	b, err := New("test-token", "my-org").WithBaseURL(server.URL).Billing(context.Background())
	require.NoError(t, err)
	require.NotNil(t, b)

	assert.Equal(t, "enterprise", b.Plan)
	// Synthetic figures: a golden file preserves the payload's SHAPE, not the
	// captured tenant's scale. Real repository counts and disk usage were
	// scrubbed — this repo is public.
	require.NotNil(t, b.BilledSeats)
	assert.Equal(t, 25, *b.BilledSeats)
	require.NotNil(t, b.FilledSeats)
	assert.Equal(t, 12, *b.FilledSeats)
	assert.Nil(t, b.CostPerSeatMinor)
	assert.Equal(t, core.BillingSourceAPISeatCount, b.Source)
	assert.Equal(t, core.BillingConfidencePartial, b.Confidence)
	assert.NotEmpty(t, b.UnavailableReason)
}

// Losing audit-log access partway through invalidates the whole map: the
// remaining members would be indistinguishable from genuinely idle ones.
func TestAuditLogPartialFailureDropsActivityClaim(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/audit-log") && strings.Contains(r.URL.Query().Get("phrase"), "actor:"):
			w.WriteHeader(http.StatusForbidden)
		case strings.Contains(r.URL.Path, "/audit-log"):
			_, err := fmt.Fprint(w, `[{"actor":"noisy","created_at":1755000000000}]`)
			require.NoError(t, err)
		case strings.HasSuffix(r.URL.Path, "/members"):
			if r.URL.Query().Get("role") == "admin" {
				_, err := fmt.Fprint(w, `[]`)
				require.NoError(t, err)
				return
			}
			_, err := fmt.Fprint(w, `[{"login":"noisy","id":1},{"login":"other","id":2}]`)
			require.NoError(t, err)
		case strings.Contains(r.URL.Path, "/outside_collaborators"):
			_, err := fmt.Fprint(w, `[]`)
			require.NoError(t, err)
		case strings.Contains(r.URL.Path, "/memberships/orgs/"):
			_, err := fmt.Fprint(w, `{"state":"active"}`)
			require.NoError(t, err)
		case strings.Contains(r.URL.Path, "/graphql"):
			_, err := fmt.Fprint(w, `{"data":{"organization":{"samlIdentityProvider":null}}}`)
			require.NoError(t, err)
		default:
			_, err := fmt.Fprint(w, `{}`)
			require.NoError(t, err)
		}
	}))
	defer server.Close()

	p := New("tok", "acme").WithBaseURL(server.URL)
	_, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	assert.False(t, p.Capabilities().ReportsActivity,
		"a partial audit-log read must not be presented as activity reporting")
}

// Git events — the ones include=all adds — carry only @timestamp and send
// created_at as null. Reading created_at alone decoded those to zero, i.e.
// 1970, which then read as "last active fifty years ago" and reported active
// engineers as idle seats. Verified against a live org: an engineer with 69
// pull requests was flagged inactive by exactly this.
func TestAuditLogReadsTimestampFromEitherSpelling(t *testing.T) {
	recent := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/orgs/my-org/audit-log" {
			phrase := r.URL.Query().Get("phrase")
			switch {
			case strings.Contains(phrase, "actor:gituser"):
				// A git event, exactly as the API returns it.
				_, err := fmt.Fprintf(w, `[{"actor":"gituser","action":"git.fetch","created_at":null,"@timestamp":%d}]`, recent.UnixMilli())
				require.NoError(t, err)
			case strings.Contains(phrase, "actor:webuser"):
				_, err := fmt.Fprintf(w, `[{"actor":"webuser","action":"repo.access","created_at":%d}]`, recent.UnixMilli())
				require.NoError(t, err)
			default:
				// Neither spelling: not evidence of idleness, so no timestamp.
				_, err := fmt.Fprint(w, `[{"actor":"undated","action":"repo.access"}]`)
				require.NoError(t, err)
			}
			return
		}
		if handleCommon(w, r) {
			return
		}
		switch {
		case r.URL.Path == "/orgs/my-org/members" && r.URL.Query().Get("role") == "admin":
			encodeTestJSON(w, []orgMember{})
		case r.URL.Path == "/orgs/my-org/members":
			encodeTestJSON(w, []orgMember{
				{Login: "gituser", ID: 1}, {Login: "webuser", ID: 2}, {Login: "undated", ID: 3},
			})
		default:
			encodeTestJSON(w, map[string]any{})
		}
	}))
	defer server.Close()

	users, err := New("test-token", "my-org").WithBaseURL(server.URL).ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 3)

	byLogin := map[string]*core.User{}
	for i := range users {
		byLogin[users[i].Metadata["login"]] = &users[i]
	}

	require.NotNil(t, byLogin["gituser"].LastActivityAt, "@timestamp must be read when created_at is null")
	assert.True(t, recent.Equal(*byLogin["gituser"].LastActivityAt))
	require.NotNil(t, byLogin["webuser"].LastActivityAt)
	assert.True(t, recent.Equal(*byLogin["webuser"].LastActivityAt))
	assert.Nil(t, byLogin["undated"].LastActivityAt, "an undated entry is not a 1970 timestamp")
}

func TestAuditLogEntryOccurredAt(t *testing.T) {
	ms := int64(1785511973065)

	got, ok := auditLogEntry{CreatedAt: ms}.occurredAt()
	require.True(t, ok)
	assert.Equal(t, 2026, got.Year())

	got, ok = auditLogEntry{Timestamp: ms}.occurredAt()
	require.True(t, ok)
	assert.Equal(t, 2026, got.Year())

	// created_at wins when both are present, but a null one must not shadow it.
	got, ok = auditLogEntry{CreatedAt: 0, Timestamp: ms}.occurredAt()
	require.True(t, ok)
	assert.Equal(t, 2026, got.Year())

	_, ok = auditLogEntry{}.occurredAt()
	assert.False(t, ok, "no timestamp is unknown, never the epoch")
}
