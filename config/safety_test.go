package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "unseat.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// The single most important default in the product: unless the operator has
// explicitly opted in, unseat must never be in a state where it can write to a
// SaaS provider. Every one of these shapes has to come back dry.
func TestDryRunDefaultsTrue(t *testing.T) {
	cases := map[string]string{
		"empty file": ``,

		"no policies section": `
identity_source:
  provider: google-directory
  domain: co.com
`,

		"policies present but silent on dry_run": `
identity_source:
  provider: google-directory
  domain: co.com
policies:
  grace_period: 72h
  notify_on_remove: true
`,

		"policies with other flags set": `
policies:
  notify_on_remove: false
  sync_interval: 10m
  exceptions:
    - email: cto@co.com
      providers: ["*"]
`,
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(writeConfig(t, body))
			require.NoError(t, err)
			assert.True(t, cfg.Policies.DryRun,
				"a config that does not say dry_run: false must never be able to mutate a provider")
		})
	}
}

// Turning it off has to be deliberate and explicit.
func TestDryRunOnlyDisabledExplicitly(t *testing.T) {
	cfg, err := Load(writeConfig(t, "policies:\n  dry_run: false\n"))
	require.NoError(t, err)
	assert.False(t, cfg.Policies.DryRun)
}

// An undefined credential reference must fail loudly rather than reach a
// provider as a literal string and come back as an unexplained 401.
func TestUndefinedEnvVarIsFatal(t *testing.T) {
	_, err := Load(writeConfig(t, `
providers:
  linear:
    api_key: "${DEFINITELY_NOT_SET_ANYWHERE}"
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DEFINITELY_NOT_SET_ANYWHERE")
}

// scan must be able to spot external identities without an identity source:
// comparing an email suffix should not require Google credentials.
func TestCorporateDomainFallsBackToIdentitySource(t *testing.T) {
	t.Run("top-level domain wins", func(t *testing.T) {
		cfg, err := Load(writeConfig(t, "domain: co.com\nidentity_source:\n  domain: legacy.com\n"))
		require.NoError(t, err)
		assert.Equal(t, "co.com", cfg.CorporateDomain())
	})

	t.Run("falls back to the identity source", func(t *testing.T) {
		cfg, err := Load(writeConfig(t, "identity_source:\n  domain: co.com\n"))
		require.NoError(t, err)
		assert.Equal(t, "co.com", cfg.CorporateDomain())
	})

	t.Run("neither set", func(t *testing.T) {
		cfg, err := Load(writeConfig(t, "currency: EUR\n"))
		require.NoError(t, err)
		assert.Empty(t, cfg.CorporateDomain())
	})
}
