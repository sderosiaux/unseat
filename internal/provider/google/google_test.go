package google

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admin "google.golang.org/api/admin/directory/v1"
	"google.golang.org/api/option"
)

// newTestProvider points the Admin SDK client at a local mock server.
func newTestProvider(t *testing.T, handler http.HandlerFunc) *Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	svc, err := admin.NewService(context.Background(),
		option.WithEndpoint(srv.URL),
		option.WithoutAuthentication(),
	)
	require.NoError(t, err)
	return &Provider{service: svc, domain: "example.com"}
}

type capturedRequest struct {
	Method string
	Path   string
	Query  string
	Body   string
}

func capturing(t *testing.T, response any) (*[]capturedRequest, http.HandlerFunc) {
	t.Helper()
	var reqs []capturedRequest
	return &reqs, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reqs = append(reqs, capturedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query().Encode(),
			Body:   string(body),
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}
}

func TestProviderName(t *testing.T) {
	p := &Provider{domain: "example.com"}
	assert.Equal(t, "google-directory", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := &Provider{}
	caps := p.Capabilities()
	assert.False(t, caps.CanAdd, "AddUser always errors, so CanAdd must be false")
	assert.True(t, caps.CanRemove)
	assert.True(t, caps.CanSuspend)
	assert.True(t, caps.CanSetRole)
	assert.True(t, caps.HasWebhook)
	assert.True(t, caps.ReportsActivity)
}

// Read-only by default: reading the directory is what scan, audit and sync
// plan need, and domain-wide delegation is authorized once against an exact
// scope list. Asking for write access to answer "who works here" would make
// every operator grant deprovisioning rights before seeing a single number.
func TestScopesAreReadOnlyByDefault(t *testing.T) {
	scopes := scopesFor(false)
	require.NotEmpty(t, scopes)
	for _, s := range scopes {
		assert.Contains(t, s, ".readonly", "the default must not be able to modify the directory")
	}
	// Group membership needs its own readonly scope; without it nested-group
	// expansion 403s and every indirect member looks like a non-member.
	assert.Contains(t, scopes, "https://www.googleapis.com/auth/admin.directory.group.member.readonly")
}

// Suspend and makeAdmin 403 on the readonly variants, so opting in must swap
// the whole list rather than append.
func TestWriteScopesRequestedOnlyWhenOptedIn(t *testing.T) {
	scopes := scopesFor(true)
	assert.Equal(t, []string{
		"https://www.googleapis.com/auth/admin.directory.user",
		"https://www.googleapis.com/auth/admin.directory.group",
	}, scopes)
	for _, s := range scopes {
		assert.NotContains(t, s, ".readonly")
	}
}

func TestWithWriteAccessOption(t *testing.T) {
	var o options
	assert.False(t, o.allowWrite, "write access is never acquired by default")
	WithWriteAccess(true)(&o)
	assert.True(t, o.allowWrite)
}

func TestListUsers(t *testing.T) {
	reqs, handler := capturing(t, admin.Users{
		Users: []*admin.User{
			{
				Id:            "1",
				PrimaryEmail:  "alice@example.com",
				Name:          &admin.UserName{FullName: "Alice"},
				IsAdmin:       true,
				LastLoginTime: "2026-07-01T10:30:00.000Z",
			},
			{
				Id:            "2",
				PrimaryEmail:  "bob@example.com",
				Name:          &admin.UserName{FullName: "Bob"},
				Suspended:     true,
				LastLoginTime: "2026-01-15T08:00:00.000Z",
			},
			{
				Id:            "3",
				PrimaryEmail:  "carol@example.com",
				Name:          &admin.UserName{FullName: "Carol"},
				Archived:      true,
				LastLoginTime: "1970-01-01T00:00:00.000Z",
			},
			{
				Id:           "4",
				PrimaryEmail: "dave@example.com",
				Name:         &admin.UserName{FullName: "Dave"},
			},
		},
	})
	p := newTestProvider(t, handler)

	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 4)

	assert.Equal(t, "alice@example.com", users[0].Email)
	assert.Equal(t, "Alice", users[0].DisplayName)
	assert.Equal(t, "admin", users[0].Role)
	assert.Equal(t, "active", users[0].Status)
	require.NotNil(t, users[0].LastActivityAt)
	assert.Equal(t, "2026-07-01T10:30:00Z", users[0].LastActivityAt.UTC().Format("2006-01-02T15:04:05Z"))

	assert.Equal(t, "suspended", users[1].Status)
	assert.Equal(t, "member", users[1].Role)
	require.NotNil(t, users[1].LastActivityAt)

	// Archived accounts cannot sign in — they must not be counted as live seats.
	assert.Equal(t, "suspended", users[2].Status)
	// Epoch lastLoginTime means "never signed in", not "logged in in 1970".
	assert.Nil(t, users[2].LastActivityAt)

	// Missing lastLoginTime stays unknown.
	assert.Nil(t, users[3].LastActivityAt)

	require.Len(t, *reqs, 1)
	assert.Equal(t, "/admin/directory/v1/users", (*reqs)[0].Path)
	assert.Contains(t, (*reqs)[0].Query, "domain=example.com")
}

func TestRemoveUserSuspendsByDefault(t *testing.T) {
	reqs, handler := capturing(t, admin.User{PrimaryEmail: "bob@example.com", Suspended: true})
	p := newTestProvider(t, handler)

	require.NoError(t, p.RemoveUser(context.Background(), "bob@example.com"))

	require.Len(t, *reqs, 1)
	got := (*reqs)[0]
	assert.Equal(t, http.MethodPut, got.Method, "must not DELETE the Workspace account")
	assert.Equal(t, "/admin/directory/v1/users/bob@example.com", got.Path)

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(got.Body), &payload))
	assert.Equal(t, true, payload["suspended"])
}

func TestRemoveUserHardDeleteIsOptIn(t *testing.T) {
	reqs, handler := capturing(t, struct{}{})
	p := newTestProvider(t, handler)
	p.hardDelete = true

	require.NoError(t, p.RemoveUser(context.Background(), "bob@example.com"))

	require.Len(t, *reqs, 1)
	assert.Equal(t, http.MethodDelete, (*reqs)[0].Method)
	assert.Equal(t, "/admin/directory/v1/users/bob@example.com", (*reqs)[0].Path)
}

func TestWithHardDeleteOption(t *testing.T) {
	var o options
	assert.False(t, o.hardDelete, "deletion must never be the default")
	WithHardDelete(true)(&o)
	assert.True(t, o.hardDelete)
}

func TestSetRoleUsesMakeAdmin(t *testing.T) {
	cases := []struct {
		role       string
		wantStatus bool
	}{
		{"admin", true},
		{"member", false},
	}
	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			reqs, handler := capturing(t, struct{}{})
			p := newTestProvider(t, handler)

			require.NoError(t, p.SetRole(context.Background(), "alice@example.com", tc.role))

			require.Len(t, *reqs, 1)
			got := (*reqs)[0]
			// users.update ignores isAdmin (output-only) and would report a fake success.
			assert.Equal(t, http.MethodPost, got.Method)
			assert.Equal(t, "/admin/directory/v1/users/alice@example.com/makeAdmin", got.Path)

			var payload map[string]any
			require.NoError(t, json.Unmarshal([]byte(got.Body), &payload))
			assert.Equal(t, tc.wantStatus, payload["status"], "status must be sent even when false")
		})
	}
}

func TestSetRoleRejectsUnknownRole(t *testing.T) {
	reqs, handler := capturing(t, struct{}{})
	p := newTestProvider(t, handler)

	err := p.SetRole(context.Background(), "alice@example.com", "editor")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported role")
	assert.Empty(t, *reqs, "no call should be made for a role Google does not have")
}

func TestAddUserNotSupported(t *testing.T) {
	p := &Provider{}
	err := p.AddUser(context.Background(), "alice@example.com", "member")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestListGroups(t *testing.T) {
	reqs, handler := capturing(t, admin.Groups{
		Groups: []*admin.Group{
			{Id: "g1", Email: "eng@example.com", Name: "Engineering", Description: "Devs", DirectMembersCount: 12},
		},
	})
	p := newTestProvider(t, handler)

	groups, err := p.ListGroups(context.Background())
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, "g1", groups[0].ID)
	assert.Equal(t, "eng@example.com", groups[0].Email)
	assert.Equal(t, "Engineering", groups[0].Name)
	assert.Equal(t, 12, groups[0].MemberCount)

	require.Len(t, *reqs, 1)
	assert.Equal(t, "/admin/directory/v1/groups", (*reqs)[0].Path)
}

func TestListGroupMembersIncludesNestedMembers(t *testing.T) {
	reqs, handler := capturing(t, admin.Members{
		Members: []*admin.Member{
			{Id: "1", Email: "alice@example.com", Type: "USER", Role: "OWNER", Status: "ACTIVE"},
			// Derived membership from a nested group — same type USER.
			{Id: "2", Email: "bob@example.com", Type: "USER", Role: "MEMBER", Status: "ACTIVE"},
			// Direct + indirect membership yields the same person twice.
			{Id: "2", Email: "Bob@example.com", Type: "USER", Role: "MEMBER", Status: "ACTIVE"},
			{Id: "g2", Email: "nested@example.com", Type: "GROUP", Role: "MEMBER"},
			{Id: "sa", Email: "bot@example.com", Type: "CUSTOMER"},
		},
	})
	p := newTestProvider(t, handler)

	members, err := p.ListGroupMembers(context.Background(), "eng@example.com")
	require.NoError(t, err)
	require.Len(t, members, 2)
	assert.Equal(t, "alice@example.com", members[0].Email)
	assert.Equal(t, "OWNER", members[0].Role)
	assert.Equal(t, "bob@example.com", members[1].Email)

	require.Len(t, *reqs, 1)
	got := (*reqs)[0]
	assert.Equal(t, "/admin/directory/v1/groups/eng@example.com/members", got.Path)
	assert.Contains(t, got.Query, "includeDerivedMembership=true")
}

func TestListUsersPaginates(t *testing.T) {
	var calls int
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		resp := admin.Users{}
		if r.URL.Query().Get("pageToken") == "" {
			resp.Users = []*admin.User{{Id: "1", PrimaryEmail: "a@example.com", Name: &admin.UserName{FullName: "A"}}}
			resp.NextPageToken = "page2"
		} else {
			resp.Users = []*admin.User{{Id: "2", PrimaryEmail: "b@example.com", Name: &admin.UserName{FullName: "B"}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	users, err := p.ListUsers(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	require.Len(t, users, 2)
	assert.Equal(t, "b@example.com", users[1].Email)
}

func TestListUsersPropagatesAPIError(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"code":403,"message":"Not Authorized to access this resource/api"}}`)
	})

	_, err := p.ListUsers(context.Background())
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "403"), err.Error())
}
