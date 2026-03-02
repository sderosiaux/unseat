package provider

import (
	"context"
	"testing"

	"github.com/sderosiaux/saas-watcher/config"
	"github.com/sderosiaux/saas-watcher/internal/core"
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
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"linear":  {APIKey: "lin_key"},
			"figma":   {APIKey: "fig_token", ExtraArgs: map[string]string{"tenant_id": "t123"}},
			"hubspot": {APIKey: "hub_token"},
			"miro":    {APIKey: "miro_token", ExtraArgs: map[string]string{"org_id": "org456"}},
			"framer":  {},
		},
	}

	identity := &mockIdentity{mockProvider{name: "google-directory"}}
	reg, _, err := BuildRegistryWithIdentity(cfg, identity)
	require.NoError(t, err)

	names := reg.List()
	assert.Len(t, names, 6) // google-directory + 5 providers
	for _, name := range []string{"google-directory", "linear", "figma", "hubspot", "miro", "framer"} {
		p, err := reg.Get(name)
		require.NoError(t, err, "provider %s should be registered", name)
		assert.Equal(t, name, p.Name())
	}
}
