package hubspot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/golden"
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
	// The actual shipped defect was this flag claiming true while
	// LastActivityAt was never populated: scan.go treats a nil
	// LastActivityAt as "never active" only when ReportsActivity is true, so
	// the wrong flag — not the dead struct field — is what flagged an entire
	// portal as inactive. A reintroduced lastActiveTime field that the API
	// never sends leaves LastActivityAt nil regardless, so only this
	// assertion, not one on LastActivityAt, catches the regression.
	assert.False(t, caps.ReportsActivity)
}

func TestListUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		// Deactivation state lives on the owner record, so ListUsers consults
		// both endpoints. Nobody is archived here.
		if strings.HasPrefix(r.URL.Path, "/crm/v3/owners") {
			fmt.Fprint(w, `{"results":[]}`)
			return
		}
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
	userCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/crm/v3/owners") {
			fmt.Fprint(w, `{"results":[]}`)
			return
		}
		userCalls++
		if r.URL.Query().Get("after") == "" {
			fmt.Fprint(w, `{"results":[{"id":"1","email":"alice@co.com"}],"paging":{"next":{"after":"100"}}}`)
			return
		}
		assert.Equal(t, "100", r.URL.Query().Get("after"))
		fmt.Fprint(w, `{"results":[{"id":"2","email":"bob@co.com"}]}`)
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)
	assert.Equal(t, 2, userCalls)
	assert.Equal(t, "alice@co.com", users[0].Email)
	assert.Equal(t, "bob@co.com", users[1].Email)
}

// Routed by path rather than call order: ListUsers now consults the owners
// endpoint too, and a counter-based mock silently mislabels which call is which.
func TestRemoveUser(t *testing.T) {
	deleted := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/crm/v3/owners"):
			fmt.Fprint(w, `{"results":[]}`)
		case r.Method == http.MethodDelete:
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			deleted = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			fmt.Fprint(w, `{"results":[{"id":"42","email":"alice@co.com"}]}`)
		}
	}))
	defer server.Close()

	p := New("test-token").WithBaseURL(server.URL)
	require.NoError(t, p.RemoveUser(context.Background(), "alice@co.com"))
	assert.Equal(t, "/settings/v3/users/42", deleted)
}

func TestRemoveUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NotEqual(t, http.MethodDelete, r.Method, "an unknown user must not trigger a deletion")
		fmt.Fprint(w, `{"results":[]}`)
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

// /settings/v3/users lists deactivated accounts alongside live ones with
// nothing to tell them apart. Only the archived CRM owner record reveals the
// state, so ListUsers must consult it — reporting a deactivated account as a
// live seat makes departed staff look like they still hold access.
func TestListUsersMarksDeactivatedFromArchivedOwners(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/crm/v3/owners"):
			assert.Equal(t, "true", r.URL.Query().Get("archived"))
			fmt.Fprint(w, `{"results":[{"email":"Gone@Co.com","archived":true}]}`)
		default:
			fmt.Fprint(w, `{"results":[
			  {"id":"1","email":"alice@co.com","seatNames":["core"]},
			  {"id":"2","email":"gone@co.com","seatNames":["sales-enterprise"]}
			]}`)
		}
	}))
	defer server.Close()

	users, err := New("tok").WithBaseURL(server.URL).ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 2)

	assert.Equal(t, core.StatusActive, users[0].Status)
	// Matched case-insensitively: the owners API echoes whatever casing was
	// used at sign-up.
	assert.Equal(t, core.StatusSuspended, users[1].Status)
	// A deactivated account keeps its seat name, which is why the seat count
	// alone cannot reveal the state.
	assert.Equal(t, "sales-enterprise", users[1].Metadata["seat"])
}

func TestListUsersPaginatesArchivedOwners(t *testing.T) {
	ownerCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/crm/v3/owners") {
			ownerCalls++
			if r.URL.Query().Get("after") == "" {
				fmt.Fprint(w, `{"results":[{"email":"a@co.com"}],"paging":{"next":{"after":"P2"}}}`)
				return
			}
			assert.Equal(t, "P2", r.URL.Query().Get("after"))
			fmt.Fprint(w, `{"results":[{"email":"b@co.com"}]}`)
			return
		}
		fmt.Fprint(w, `{"results":[{"id":"1","email":"a@co.com"},{"id":"2","email":"b@co.com"}]}`)
	}))
	defer server.Close()

	users, err := New("tok").WithBaseURL(server.URL).ListUsers(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, ownerCalls, "a truncated owners walk would silently mark people active")
	for _, u := range users {
		assert.Equal(t, core.StatusSuspended, u.Status)
	}
}

// Without the owners scope there is no way to tell a deactivated account from
// a live one. Refusing is safer than returning every seat as active.
func TestListUsersRequiresOwnersScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/crm/v3/owners") {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"category":"MISSING_SCOPES"}`)
			return
		}
		fmt.Fprint(w, `{"results":[{"id":"1","email":"a@co.com"}]}`)
	}))
	defer server.Close()

	_, err := New("tok").WithBaseURL(server.URL).ListUsers(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "crm.objects.owners.read")
}

// TestListUsersGoldenShape replays the real /settings/v3/users/ and
// /crm/v3/owners response shapes (internal/provider/golden) instead of a
// mock built from hubspotUser/owner. It documents that the real payload
// carries no activity field and guards against the golden fixture itself
// drifting to include one that was never verified against a live portal.
//
// It does NOT catch the shipped defect (a lastActiveTime field our code
// parsed but the API never sent): with that field reintroduced, unmarshalling
// this fixture still leaves LastActivityAt nil, so the assertions below would
// stay green either way. TestProviderCapabilities' ReportsActivity assertion
// is what actually catches that regression, because the real damage was the
// capability flag claiming activity data existed, not the dead field itself.
func TestListUsersGoldenShape(t *testing.T) {
	usersBody := golden.Load(t, "hubspot-users.json")
	ownersBody := golden.Load(t, "hubspot-archived-owners.json")
	// Fixture-drift guard: if this ever starts containing lastActiveTime, the
	// fixture stopped reflecting the live portal and needs re-verifying
	// against vendor docs before anything below can be trusted.
	assert.NotContains(t, string(usersBody), "lastActiveTime")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/crm/v3/owners") {
			w.Write(ownersBody)
			return
		}
		assert.Equal(t, "/settings/v3/users/", r.URL.Path)
		w.Write(usersBody)
	}))
	defer server.Close()

	users, err := New("tok").WithBaseURL(server.URL).ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 4)

	assert.Equal(t, "alice.anderson@example.com", users[0].Email)
	assert.Equal(t, "Alice Anderson", users[0].DisplayName)
	assert.Equal(t, "member", users[0].Role)
	assert.Equal(t, core.StatusActive, users[0].Status)
	assert.Equal(t, "core", users[0].Metadata["seat"])

	assert.Equal(t, "Bob Brown", users[1].DisplayName)
	assert.Equal(t, "sales-enterprise", users[1].Metadata["seat"])

	// No first/last name in the payload, and archived per the owners feed.
	assert.Equal(t, "carol.chen@example.com", users[2].DisplayName)
	assert.Equal(t, core.StatusSuspended, users[2].Status)
	assert.Equal(t, "view-only", users[2].Metadata["seat"])

	assert.Equal(t, "super_admin", users[3].Role)
	assert.Equal(t, core.StatusActive, users[3].Status)

	for _, u := range users {
		assert.Nil(t, u.LastActivityAt, "the real payload carries no activity field at all")
	}
}
