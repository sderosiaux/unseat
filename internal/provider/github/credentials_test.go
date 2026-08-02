package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The payloads below are the shapes the live API returns, trimmed and with the
// identities replaced. Marshalling our own structs instead is how six phantom
// fields got shipped: the test agreed with the code and both disagreed with
// the vendor.
const installationsBody = `{"total_count":3,"installations":[
  {"id":7762498,"client_id":"Iv1.26dd47f3a7a88f72","app_slug":"legacy-scanner",
   "repository_selection":"selected","created_at":"2021-10-18T09:12:00Z",
   "updated_at":"2026-07-29T11:00:00Z","suspended_at":"2026-04-16T12:44:46Z",
   "suspended_by":{"login":"admin-login"},
   "html_url":"https://github.com/organizations/acme/settings/installations/7762498",
   "permissions":{"contents":"read","metadata":"read"}},
  {"id":881234,"client_id":"Iv23liDEPLOYER","app_slug":"deployer",
   "repository_selection":"all","created_at":"2021-12-20T08:00:00Z",
   "updated_at":"2026-04-17T08:00:00Z","suspended_at":null,"suspended_by":null,
   "permissions":{"administration":"write","contents":"write","metadata":"read"}},
  {"id":990001,"client_id":"Iv23liREADER","app_slug":"reader",
   "repository_selection":"all","created_at":"2023-01-05T08:00:00Z",
   "updated_at":"2023-01-05T08:00:00Z","suspended_at":null,"suspended_by":null,
   "permissions":{"contents":"read","metadata":"read"}}
]}`

const installEventsBody = `[
  {"action":"integration_installation.create","actor":"deployer-installer",
   "application_client_id":"Iv23liDEPLOYER","@timestamp":1784888440019}
]`

func credentialServer(t *testing.T, auditStatus int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/orgs/acme/installations":
			assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
			_, err := fmt.Fprint(w, installationsBody)
			require.NoError(t, err)
		case "/orgs/acme/audit-log":
			assert.Equal(t, "action:integration_installation.create", r.URL.Query().Get("phrase"))
			if auditStatus != http.StatusOK {
				w.WriteHeader(auditStatus)
				return
			}
			_, err := fmt.Fprint(w, installEventsBody)
			require.NoError(t, err)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestListCredentials(t *testing.T) {
	server := credentialServer(t, http.StatusOK)
	defer server.Close()

	creds, err := New("tok", "acme").WithBaseURL(server.URL).ListCredentials(context.Background())
	require.NoError(t, err)
	require.Len(t, creds, 3)

	byLabel := map[string]core.Credential{}
	for _, c := range creds {
		byLabel[c.Label] = c
	}

	legacy := byLabel["legacy-scanner"]
	assert.Equal(t, core.CredentialAppInstallation, legacy.Kind)
	assert.Equal(t, "7762498", legacy.ID, "the installation id is what an operator acts on")
	assert.True(t, legacy.Disabled)
	require.NotNil(t, legacy.DisabledAt)
	assert.Equal(t, "2026-04-16", legacy.DisabledAt.Format("2006-01-02"))
	assert.Equal(t, "admin-login", legacy.Metadata["suspended_by"])
	assert.Equal(t, core.ReachSelected, legacy.Reach)

	deployer := byLabel["deployer"]
	assert.Equal(t, core.ReachAll, deployer.Reach)
	assert.Equal(t, []string{"administration", "contents"}, deployer.PrivilegedScopes)
	assert.Equal(t, []string{"administration", "contents", "metadata"}, deployer.Scopes)
	assert.False(t, deployer.Disabled)
	require.NotNil(t, deployer.CreatedAt)
	assert.Equal(t, 2021, deployer.CreatedAt.Year())

	// read-only everywhere is not privileged, however wide the reach
	assert.Empty(t, byLabel["reader"].PrivilegedScopes)
}

// GitHub does not name the installer anywhere in the installations payload.
// The audit log does, and the two are joined on client id — application_client_id
// on the event, client_id on the installation. Nothing else in either payload
// identifies the same app.
func TestListCredentialsAttributesInstallerFromAuditLog(t *testing.T) {
	server := credentialServer(t, http.StatusOK)
	defer server.Close()

	creds, err := New("tok", "acme").WithBaseURL(server.URL).ListCredentials(context.Background())
	require.NoError(t, err)

	for _, c := range creds {
		switch c.Label {
		case "deployer":
			assert.Equal(t, "deployer-installer", c.CreatedBy)
		default:
			assert.Empty(t, c.CreatedBy, "%s has no event in the log and must not be attributed", c.Label)
		}
	}
}

// The audit log is Enterprise-only and answers 403 elsewhere. That is an
// absence of evidence about who installed what — it must not fail the listing,
// which is the part that works everywhere.
func TestListCredentialsSurvivesAnUnreadableAuditLog(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound} {
		server := credentialServer(t, status)

		creds, err := New("tok", "acme").WithBaseURL(server.URL).ListCredentials(context.Background())
		require.NoError(t, err, "status %d", status)
		require.Len(t, creds, 3)
		for _, c := range creds {
			assert.Empty(t, c.CreatedBy)
		}
		server.Close()
	}
}

// Attribution flows through the same alias index every provider uses, so a
// login the directory cannot resolve leaves the credential unowned rather than
// pinned on a name that merely looks similar.
func TestListCredentialsLeavesLoginResolutionToCore(t *testing.T) {
	server := credentialServer(t, http.StatusOK)
	defer server.Close()

	creds, err := New("tok", "acme").WithBaseURL(server.URL).ListCredentials(context.Background())
	require.NoError(t, err)

	classified := core.ClassifyCredentials(core.ClassifyCredentialsInput{
		Credentials: creds,
		Directory:   map[string]core.DirectoryUser{"someone@acme.com": {Email: "someone@acme.com"}},
		Domain:      "acme.com",
		AliasIndex:  map[string]string{"deployer-installer": "someone@acme.com"},
	})

	byLabel := map[string]core.ClassifiedCredential{}
	for _, c := range classified {
		byLabel[c.Credential.Label] = c
	}
	assert.Equal(t, core.CredentialLive, byLabel["deployer"].Class)
	assert.True(t, byLabel["deployer"].Overreaching)
	assert.Equal(t, core.CredentialUnowned, byLabel["reader"].Class)
	assert.Equal(t, core.CredentialDormant, byLabel["legacy-scanner"].Class)
}

// total_count terminates the walk; a page that comes back full must not end it.
func TestListCredentialsPaginates(t *testing.T) {
	pages := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/orgs/acme/audit-log" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		pages++
		if r.URL.Query().Get("page") == "1" {
			var b string
			for i := 0; i < 100; i++ {
				if i > 0 {
					b += ","
				}
				b += fmt.Sprintf(`{"id":%d,"app_slug":"app%d","repository_selection":"selected"}`, i, i)
			}
			_, err := fmt.Fprintf(w, `{"total_count":101,"installations":[%s]}`, b)
			require.NoError(t, err)
			return
		}
		_, err := fmt.Fprint(w, `{"total_count":101,"installations":[{"id":999,"app_slug":"last","repository_selection":"selected"}]}`)
		require.NoError(t, err)
	}))
	defer server.Close()

	creds, err := New("tok", "acme").WithBaseURL(server.URL).ListCredentials(context.Background())
	require.NoError(t, err)
	assert.Len(t, creds, 101)
	assert.Equal(t, 2, pages)
}

// A missing scope and a revoked token answer the same opaque 403. Naming the
// scope is the difference between a two-click fix and a dead end.
func TestListCredentialsNamesTheScopeOnAuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	_, err := New("tok", "acme").WithBaseURL(server.URL).ListCredentials(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "admin:org")
}

// An absent suspended_at must leave the date nil, not decode to 1970 and
// render as "dormant since 1970-01-01".
func TestParseGitHubTimeRefusesToInventADate(t *testing.T) {
	assert.Nil(t, parseGitHubTime(""))
	assert.Nil(t, parseGitHubTime("not-a-date"))
	assert.Nil(t, parseGitHubTime("1784888440019"), "the audit log's epoch millis are not RFC3339")

	got := parseGitHubTime("2026-04-16T12:44:46Z")
	require.NotNil(t, got)
	assert.Equal(t, 2026, got.Year())
}

// The connector must not claim usage it cannot see: an installation's
// updated_at moves when permissions change, never when the app acts.
func TestCapabilitiesDoNotClaimCredentialUsage(t *testing.T) {
	assert.False(t, New("tok", "acme").Capabilities().ReportsCredentialUsage)
}
