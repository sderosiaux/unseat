package provider

import (
	"context"
	"testing"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockIdentity satisfies IdentityProvider for testing.
type mockIdentity struct {
	mockProvider
}

func (m *mockIdentity) ListGroups(_ context.Context) ([]core.Group, error) {
	return nil, nil
}

func (m *mockIdentity) ListGroupMembers(_ context.Context, _ string) ([]core.User, error) {
	return nil, nil
}

func TestBuildRegistry_UnknownIdentityProvider(t *testing.T) {
	cfg := &config.Config{
		IdentitySource: config.IdentitySource{
			Provider: "unknown-idp",
		},
	}
	_, _, err := BuildRegistry(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown identity provider")
}

func TestBuildRegistryWithIdentity_UnknownTargetProvider(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"nonexistent-saas": {APIKey: "key"},
		},
	}
	identity := &mockIdentity{mockProvider{name: "google-directory"}}
	_, _, err := BuildRegistryWithIdentity(cfg, identity)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}

func TestBuildRegistryWithIdentity_Linear(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"linear": {APIKey: "lin_api_test123"},
		},
	}

	identity := &mockIdentity{mockProvider{name: "google-directory"}}
	reg, idp, err := BuildRegistryWithIdentity(cfg, identity)
	require.NoError(t, err)
	assert.NotNil(t, idp)
	assert.Equal(t, "google-directory", idp.Name())

	// Identity provider registered
	p, err := reg.Get("google-directory")
	require.NoError(t, err)
	assert.Equal(t, "google-directory", p.Name())

	// Linear registered
	p, err = reg.Get("linear")
	require.NoError(t, err)
	assert.Equal(t, "linear", p.Name())
}

func TestBuildRegistryWithIdentity_NoProviders(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{},
	}

	identity := &mockIdentity{mockProvider{name: "google-directory"}}
	reg, idp, err := BuildRegistryWithIdentity(cfg, identity)
	require.NoError(t, err)
	assert.NotNil(t, idp)

	// Only identity provider registered
	names := reg.List()
	assert.Equal(t, []string{"google-directory"}, names)
}

func TestBuildRegistryWithIdentity_MultipleProviders(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"linear": {APIKey: "key1"},
		},
	}

	identity := &mockIdentity{mockProvider{name: "google-directory"}}
	reg, _, err := BuildRegistryWithIdentity(cfg, identity)
	require.NoError(t, err)

	names := reg.List()
	assert.Len(t, names, 2)
	assert.Contains(t, names, "google-directory")
	assert.Contains(t, names, "linear")
}

func TestBuildRegistryWithIdentity_AllProviders(t *testing.T) {
	extra := func(kv ...string) map[string]string {
		m := make(map[string]string)
		for i := 0; i < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			// Original
			"linear":      {APIKey: "k"},
			"figma":       {APIKey: "k", ExtraArgs: extra("tenant_id", "t")},
			"hubspot":     {APIKey: "k"},
			"miro":        {APIKey: "k", ExtraArgs: extra("org_id", "o")},
			"framer":      {},
			"slack":       {APIKey: "k"},
			"anthropic":   {APIKey: "k"},
			"claude-code": {APIKey: "k"},
			// Engineering
			"github":    {APIKey: "k", ExtraArgs: extra("org", "o")},
			"gitlab":    {APIKey: "k"},
			"atlassian": {APIKey: "k", ExtraArgs: extra("directory_id", "d")},
			"notion":    {APIKey: "k"},
			"shortcut":  {APIKey: "k"},
			// Project Management
			"asana":   {APIKey: "k", ExtraArgs: extra("workspace_id", "w")},
			"monday":  {APIKey: "k"},
			"clickup": {APIKey: "k", ExtraArgs: extra("team_id", "t")},
			"trello":  {APIKey: "k", ExtraArgs: extra("token", "t", "org_id", "o")},
			"vercel":  {APIKey: "k", ExtraArgs: extra("team_id", "t")},
			// Infrastructure
			"netlify":         {APIKey: "k", ExtraArgs: extra("account_slug", "a")},
			"aws-iam":         {APIKey: "k", ExtraArgs: extra("scim_endpoint", "https://scim.aws")},
			"gcp-iam":         {APIKey: "k", ExtraArgs: extra("customer_id", "C")},
			"azure-ad":        {APIKey: "k"},
			"microsoft-teams": {APIKey: "k"},
			// Observability
			"datadog":   {APIKey: "k", ExtraArgs: extra("app_key", "a")},
			"pagerduty": {APIKey: "k"},
			"grafana":   {APIKey: "k"},
			"newrelic":  {APIKey: "k"},
			"sentry":    {APIKey: "k", ExtraArgs: extra("org_slug", "o")},
			// CRM / Support
			"salesforce": {APIKey: "k", ExtraArgs: extra("instance_url", "https://sf.com")},
			"intercom":   {APIKey: "k"},
			"zendesk":    {APIKey: "k", ExtraArgs: extra("subdomain", "s")},
			"freshdesk":  {APIKey: "k", ExtraArgs: extra("subdomain", "s")},
			// Communication
			"zoom":    {APIKey: "k"},
			"discord": {APIKey: "k", ExtraArgs: extra("guild_id", "g")},
			"loom":    {APIKey: "k", ExtraArgs: extra("space_id", "s")},
			// Storage
			"dropbox": {APIKey: "k"},
			"box":     {APIKey: "k"},
			// Security / Identity
			"1password": {APIKey: "k"},
			"lastpass":  {APIKey: "k", ExtraArgs: extra("provisioning_hash", "h")},
			"okta":      {APIKey: "k", ExtraArgs: extra("domain", "d")},
			"auth0":     {APIKey: "k", ExtraArgs: extra("domain", "d")},
			"snyk":      {APIKey: "k", ExtraArgs: extra("org_id", "o")},
			// Design
			"canva":    {APIKey: "k"},
			"adobe":    {APIKey: "k", ExtraArgs: extra("org_id", "o")},
			"docusign": {APIKey: "k", ExtraArgs: extra("org_id", "o")},
			// Finance
			"stripe": {},
			"brex":   {APIKey: "k"},
			// HR
			"rippling": {APIKey: "k"},
			"bamboohr": {APIKey: "k", ExtraArgs: extra("subdomain", "s")},
			"deel":     {APIKey: "k"},
			// Data
			"airtable":   {APIKey: "k", ExtraArgs: extra("enterprise_account_id", "e")},
			"snowflake":  {APIKey: "k", ExtraArgs: extra("account", "a")},
			"databricks": {APIKey: "k", ExtraArgs: extra("workspace", "w")},
		},
	}

	identity := &mockIdentity{mockProvider{name: "google-directory"}}
	reg, _, err := BuildRegistryWithIdentity(cfg, identity)
	require.NoError(t, err)

	names := reg.List()
	assert.Len(t, names, 54) // google-directory + 53 providers

	expectedNames := []string{
		"google-directory",
		"linear", "figma", "hubspot", "miro", "framer", "slack", "anthropic", "claude-code",
		"github", "gitlab", "atlassian", "notion", "shortcut",
		"asana", "monday", "clickup", "trello", "vercel",
		"netlify", "aws-iam", "gcp-iam", "azure-ad", "microsoft-teams",
		"datadog", "pagerduty", "grafana", "newrelic", "sentry",
		"salesforce", "intercom", "zendesk", "freshdesk",
		"zoom", "discord", "loom",
		"dropbox", "box",
		"1password", "lastpass", "okta", "auth0", "snyk",
		"canva", "adobe", "docusign",
		"stripe", "brex",
		"rippling", "bamboohr", "deel",
		"airtable", "snowflake", "databricks",
	}
	for _, name := range expectedNames {
		p, err := reg.Get(name)
		require.NoError(t, err, "provider %s should be registered", name)
		assert.Equal(t, name, p.Name())
	}
}
