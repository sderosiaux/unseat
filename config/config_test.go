package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	yaml := `
identity_source:
  provider: google-directory
  domain: mycompany.com
  credentials_file: /path/to/creds.json

providers:
  linear:
    api_key: "${LINEAR_API_KEY}"
  figma:
    api_key: "${FIGMA_API_KEY}"

mappings:
  - group: design-team@mycompany.com
    providers:
      - name: figma
        role: editor
      - name: miro
        role: member

  - group: engineering@mycompany.com
    providers:
      - name: linear
        role: member

policies:
  grace_period: 72h
  dry_run: false
  notify_on_remove: true
  notify_channels:
    - slack:#it-ops
  exceptions:
    - email: cto@mycompany.com
      providers: ["*"]
`
	tmpFile, err := os.CreateTemp("", "unseat-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	_, err = tmpFile.WriteString(yaml)
	require.NoError(t, err)
	tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	require.NoError(t, err)

	assert.Equal(t, "google-directory", cfg.IdentitySource.Provider)
	assert.Equal(t, "mycompany.com", cfg.IdentitySource.Domain)
	assert.Len(t, cfg.Mappings, 2)
	assert.Equal(t, "design-team@mycompany.com", cfg.Mappings[0].Group)
	assert.Len(t, cfg.Mappings[0].Providers, 2)
	assert.Equal(t, "figma", cfg.Mappings[0].Providers[0].Name)
	assert.Equal(t, "editor", cfg.Mappings[0].Providers[0].Role)
	assert.Equal(t, 72*time.Hour, cfg.Policies.GracePeriod)
	assert.False(t, cfg.Policies.DryRun)
	assert.Len(t, cfg.Policies.Exceptions, 1)
	assert.Equal(t, "cto@mycompany.com", cfg.Policies.Exceptions[0].Email)
}

func TestLoadConfigInvalid(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "bad-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString("not: [valid: yaml: {{")
	tmpFile.Close()

	_, err = Load(tmpFile.Name())
	assert.Error(t, err)
}

func TestGroupsForProvider(t *testing.T) {
	cfg := &Config{
		Mappings: []Mapping{
			{
				Group: "design@co.com",
				Providers: []ProviderMapping{
					{Name: "figma", Role: "editor"},
				},
			},
			{
				Group: "eng@co.com",
				Providers: []ProviderMapping{
					{Name: "figma", Role: "viewer"},
					{Name: "linear", Role: "member"},
				},
			},
		},
	}

	groups := cfg.GroupsForProvider("figma")
	assert.Len(t, groups, 2)
	assert.Equal(t, "design@co.com", groups[0].Group)
	assert.Equal(t, "eng@co.com", groups[1].Group)

	groups = cfg.GroupsForProvider("linear")
	assert.Len(t, groups, 1)

	groups = cfg.GroupsForProvider("unknown")
	assert.Len(t, groups, 0)
}

func TestIsException(t *testing.T) {
	cfg := &Config{
		Policies: Policies{
			Exceptions: []Exception{
				{Email: "cto@co.com", Providers: []string{"*"}},
				{Email: "contractor@co.com", Providers: []string{"linear"}},
			},
		},
	}

	assert.True(t, cfg.IsException("cto@co.com", "figma"))
	assert.True(t, cfg.IsException("cto@co.com", "linear"))
	assert.True(t, cfg.IsException("contractor@co.com", "linear"))
	assert.False(t, cfg.IsException("contractor@co.com", "figma"))
	assert.False(t, cfg.IsException("nobody@co.com", "figma"))
}

func TestLoadAliases(t *testing.T) {
	yaml := `
identity_source:
  provider: google-directory
  domain: mycompany.com

providers:
  linear:
    api_key: test

mappings:
  - group: eng@mycompany.com
    providers:
      - name: linear
        role: member

aliases:
  dana@mycompany.com:
    - dana99
  river@mycompany.com:
    - river@personal.net
    - river-gh
`
	tmpFile, err := os.CreateTemp("", "unseat-alias-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	_, err = tmpFile.WriteString(yaml)
	require.NoError(t, err)
	tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	require.NoError(t, err)

	assert.Len(t, cfg.Aliases, 2)
	assert.Equal(t, []string{"dana99"}, cfg.Aliases["dana@mycompany.com"])
	assert.Equal(t, []string{"river@personal.net", "river-gh"}, cfg.Aliases["river@mycompany.com"])
}
