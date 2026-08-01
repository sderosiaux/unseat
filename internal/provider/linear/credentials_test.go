package linear

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// credentialServer answers the integrations and webhooks queries with the
// shapes the live API returns. It dispatches on the query text because both
// go to the same GraphQL endpoint.
func credentialServer(t *testing.T, integrations, webhooks string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.Equal(t, "test-key", r.Header.Get("Authorization"))
		var req struct {
			Query string `json:"query"`
		}
		require.NoError(t, json.Unmarshal(body, &req))

		switch {
		case strings.Contains(req.Query, "integrations("):
			fmt.Fprint(w, integrations)
		case strings.Contains(req.Query, "webhooks("):
			fmt.Fprint(w, webhooks)
		default:
			t.Errorf("unexpected query %s", req.Query)
		}
	}))
}

const integrationsBody = `{"data":{"integrations":{"nodes":[
  {"id":"i1","service":"slack","createdAt":"2025-09-18T10:00:00.000Z","archivedAt":null,
   "creator":{"email":"Departed@Acme.com"},"team":null},
  {"id":"i2","service":"figma","createdAt":"2022-08-29T10:00:00.000Z","archivedAt":null,
   "creator":{"email":"ada@acme.com"},"team":{"name":"Design"}},
  {"id":"i3","service":"zendesk","createdAt":"2025-07-21T10:00:00.000Z",
   "archivedAt":"2026-02-01T10:00:00.000Z","creator":{"email":"ada@acme.com"},"team":null},
  {"id":"i4","service":"legacy","createdAt":"2021-01-01T10:00:00.000Z","archivedAt":null,
   "creator":null,"team":null}
],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}`

const webhooksBody = `{"data":{"webhooks":{"nodes":[
  {"id":"w1","label":"GitHub Sync","url":"https://hooks.example.com/gh","enabled":false,
   "createdAt":"2022-11-14T10:00:00.000Z","creator":{"email":"departed@acme.com"}},
  {"id":"w2","label":"","url":"https://hooks.example.com/anon","enabled":true,
   "createdAt":"2024-01-01T10:00:00.000Z","creator":{"email":"ada@acme.com"}}
],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}`

func TestListCredentials(t *testing.T) {
	server := credentialServer(t, integrationsBody, webhooksBody)
	defer server.Close()

	creds, err := New("test-key").WithBaseURL(server.URL).ListCredentials(context.Background())
	require.NoError(t, err)
	require.Len(t, creds, 6)

	byID := map[string]core.Credential{}
	for _, c := range creds {
		byID[c.ID] = c
	}

	slack := byID["i1"]
	assert.Equal(t, core.CredentialIntegration, slack.Kind)
	assert.Equal(t, "slack", slack.Label)
	assert.Equal(t, "departed@acme.com", slack.CreatedBy, "the author is normalised for directory lookup")
	require.NotNil(t, slack.CreatedAt)
	assert.Equal(t, 2025, slack.CreatedAt.Year())
	assert.False(t, slack.Disabled)

	assert.Equal(t, "Design", byID["i2"].Metadata["team"])

	// Archived is Linear's word for switched off, and it carries a date.
	assert.True(t, byID["i3"].Disabled)
	require.NotNil(t, byID["i3"].DisabledAt)

	// A null creator is an absence, not an empty person.
	assert.Empty(t, byID["i4"].CreatedBy)

	hook := byID["w1"]
	assert.Equal(t, core.CredentialWebhook, hook.Kind)
	assert.Equal(t, "GitHub Sync", hook.Label)
	assert.True(t, hook.Disabled, "enabled:false is a disabled webhook")
	assert.Nil(t, hook.DisabledAt, "Linear exposes the boolean and no date; none is invented")
	assert.Equal(t, "https://hooks.example.com/anon", byID["w2"].Label, "an unlabelled hook is named by its destination")
}

// The reason this connector exists: an integration whose author has left runs
// on a grant no offboarding touched.
func TestListCredentialsSurfacesOrphanedIntegrations(t *testing.T) {
	server := credentialServer(t, integrationsBody, webhooksBody)
	defer server.Close()

	creds, err := New("test-key").WithBaseURL(server.URL).ListCredentials(context.Background())
	require.NoError(t, err)

	classified := core.ClassifyCredentials(core.ClassifyCredentialsInput{
		Credentials: creds,
		Directory:   map[string]core.DirectoryUser{"ada@acme.com": {Email: "ada@acme.com"}},
		Domain:      "acme.com",
	})

	byID := map[string]core.ClassifiedCredential{}
	for _, c := range classified {
		byID[c.Credential.ID] = c
	}
	assert.Equal(t, core.CredentialOrphaned, byID["i1"].Class, "slack, authored by someone who left")
	// The webhook's author has left too, but it is switched off, and a switched
	// off credential is settled as dormant whoever made it: the recommendation
	// is to delete it, not to find it a new owner.
	assert.Equal(t, core.CredentialDormant, byID["w1"].Class)
	assert.Equal(t, core.CredentialLive, byID["i2"].Class)
	assert.Equal(t, core.CredentialDormant, byID["i3"].Class)
	assert.Equal(t, core.CredentialUnowned, byID["i4"].Class)
}

// Linear states no scopes for these objects, so reach must stay unknown rather
// than defaulting to something that reads as narrow.
func TestListCredentialsClaimsNoReach(t *testing.T) {
	server := credentialServer(t, integrationsBody, webhooksBody)
	defer server.Close()

	creds, err := New("test-key").WithBaseURL(server.URL).ListCredentials(context.Background())
	require.NoError(t, err)
	for _, c := range creds {
		assert.Equal(t, core.ReachUnknown, c.Reach, c.Label)
		assert.Empty(t, c.PrivilegedScopes, c.Label)
	}
}

func TestListCredentialsPaginates(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		require.NoError(t, json.Unmarshal(body, &req))

		if strings.Contains(req.Query, "webhooks(") {
			fmt.Fprint(w, `{"data":{"webhooks":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}`)
			return
		}
		calls++
		if req.Variables["after"] == nil {
			fmt.Fprint(w, `{"data":{"integrations":{"nodes":[{"id":"a","service":"one"}],
			  "pageInfo":{"hasNextPage":true,"endCursor":"cursor-1"}}}}`)
			return
		}
		assert.Equal(t, "cursor-1", req.Variables["after"])
		fmt.Fprint(w, `{"data":{"integrations":{"nodes":[{"id":"b","service":"two"}],
		  "pageInfo":{"hasNextPage":false,"endCursor":""}}}}`)
	}))
	defer server.Close()

	creds, err := New("test-key").WithBaseURL(server.URL).ListCredentials(context.Background())
	require.NoError(t, err)
	assert.Len(t, creds, 2)
	assert.Equal(t, 2, calls)
}

func TestListCredentialsPropagatesErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"errors":[{"message":"Access denied"}]}`)
	}))
	defer server.Close()

	_, err := New("test-key").WithBaseURL(server.URL).ListCredentials(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Access denied")
}

func TestParseLinearTimeRefusesToInventADate(t *testing.T) {
	assert.Nil(t, parseLinearTime(""))
	assert.Nil(t, parseLinearTime("nope"))
	got := parseLinearTime("2025-09-18T10:00:00.000Z")
	require.NotNil(t, got)
	assert.Equal(t, 2025, got.Year())
}

func TestCapabilitiesDoNotClaimCredentialUsage(t *testing.T) {
	assert.False(t, New("test-key").Capabilities().ReportsCredentialUsage)
}
