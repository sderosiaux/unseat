package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLintAcceptsCurrentConfigShape(t *testing.T) {
	body := `
domain: conduktor.io

identity_source:
  provider: google-directory
  domain: conduktor.io
  credentials_file: ./gcp-credentials.json
  admin_email: admin@conduktor.io
  allow_write: false

providers:
  linear:
    api_key: "${LINEAR_API_KEY}"
  github:
    base_url: https://api.github.com
    extra:
      org: conduktor

mappings:
  - group: engineering@conduktor.io
    providers:
      - name: linear
        role: member
      - name: github
        role: member

aliases:
  person@conduktor.io: [person-gh]
  other@conduktor.io:
    - other-gh

policies:
  dry_run: true
  grace_period: 72h
  sync_interval: "${UNSEAT_SYNC_INTERVAL}"
  notify_on_remove: false
  notify_channels:
    - slack:#it-ops
  exceptions:
    - email: automation@conduktor.io
      providers: ["github"]
  notify:
    slack_webhook_url: "${SLACK_WEBHOOK_URL}"
    smtp_host: smtp.conduktor.io
    smtp_port: 587
    smtp_from: unseat@conduktor.io
    smtp_user: unseat
    smtp_pass: "${SMTP_PASS}"
`

	diagnostics := LintBytes("unseat.yaml", []byte(body),
		WithKnownProviders([]string{"google-directory", "linear", "github"}))

	require.Empty(t, diagnostics)
}

func TestLintRejectsRemovedManualBillingKeys(t *testing.T) {
	body := `
currency: EUR
providers:
  linear:
    cost_per_seat: 14
    monthly_spend: 592
    bills_suspended_seats: false
`

	diagnostics := LintBytes("unseat.yaml", []byte(body), WithKnownProviders([]string{"linear"}))

	assertDiagnostic(t, diagnostics, "currency", "removed config key")
	assertDiagnostic(t, diagnostics, "providers.linear.cost_per_seat", "billing is API-only")
	assertDiagnostic(t, diagnostics, "providers.linear.monthly_spend", "billing is API-only")
	assertDiagnostic(t, diagnostics, "providers.linear.bills_suspended_seats", "billing is API-only")
}

func TestLintRejectsUnknownKeys(t *testing.T) {
	body := `
identity_source:
  provider: google-directory
  service_account: ./creds.json
providers:
  linear:
    token: abc
mappings:
  - group: engineering@conduktor.io
    providerz:
      - name: linear
policies:
  dryrun: true
  exceptions:
    - email: automation@conduktor.io
      provider: github
`

	diagnostics := LintBytes("unseat.yaml", []byte(body), WithKnownProviders([]string{"google-directory", "linear", "github"}))

	assertDiagnostic(t, diagnostics, "identity_source.service_account", "unknown config key")
	assertDiagnostic(t, diagnostics, "providers.linear.token", "unknown config key")
	assertDiagnostic(t, diagnostics, "mappings[0].providerz", "unknown config key")
	assertDiagnostic(t, diagnostics, "policies.dryrun", "unknown config key")
	assertDiagnostic(t, diagnostics, "policies.exceptions[0].provider", "unknown config key")
}

func TestLintRejectsBadTypesAndFormats(t *testing.T) {
	body := `
domain:
  nested: bad
providers:
  linear:
    extra:
      org:
        nested: bad
mappings:
  group: engineering@conduktor.io
aliases:
  person@conduktor.io: person-gh
policies:
  dry_run: definitely
  grace_period: soon
  notify:
    smtp_port: not-a-port
`

	diagnostics := LintBytes("unseat.yaml", []byte(body), WithKnownProviders([]string{"linear"}))

	assertDiagnostic(t, diagnostics, "domain", "must be a scalar value")
	assertDiagnostic(t, diagnostics, "providers.linear.extra.org", "must be a scalar value")
	assertDiagnostic(t, diagnostics, "mappings", "must be a sequence")
	assertDiagnostic(t, diagnostics, "aliases.person@conduktor.io", "must be a sequence")
	assertDiagnostic(t, diagnostics, "policies.dry_run", "must be a boolean")
	assertDiagnostic(t, diagnostics, "policies.grace_period", "must be a duration")
	assertDiagnostic(t, diagnostics, "policies.notify.smtp_port", "must be an integer")
}

func TestLintRejectsUnknownProviderNames(t *testing.T) {
	body := `
identity_source:
  provider: google-directory
providers:
  githb:
    api_key: test
mappings:
  - group: engineering@conduktor.io
    providers:
      - name: lienar
        role: member
policies:
  exceptions:
    - email: automation@conduktor.io
      providers: ["githb", "*"]
`

	diagnostics := LintBytes("unseat.yaml", []byte(body), WithKnownProviders([]string{"google-directory", "linear", "github"}))

	assertDiagnostic(t, diagnostics, "providers.githb", "unknown provider")
	assertDiagnostic(t, diagnostics, "mappings[0].providers[0].name", "unknown provider")
	assertDiagnostic(t, diagnostics, "policies.exceptions[0].providers[0]", "unknown provider")
	assertNoDiagnostic(t, diagnostics, "policies.exceptions[0].providers[1]", "unknown provider")
}

func TestLintReportsDuplicateKeys(t *testing.T) {
	body := `
providers:
  linear:
    api_key: one
    api_key: two
`

	diagnostics := LintBytes("unseat.yaml", []byte(body), WithKnownProviders([]string{"linear"}))

	assertDiagnostic(t, diagnostics, "providers.linear.api_key", "duplicate key")
}

func TestLintDoesNotRequireEnvironmentExpansion(t *testing.T) {
	body := `
providers:
  linear:
    api_key: "${DEFINITELY_NOT_SET}"
policies:
  dry_run: "${UNSEAT_DRY_RUN}"
  grace_period: "${UNSEAT_GRACE_PERIOD}"
`

	diagnostics := LintBytes("unseat.yaml", []byte(body), WithKnownProviders([]string{"linear"}))

	require.Empty(t, diagnostics)
}

func assertDiagnostic(t *testing.T, diagnostics []LintDiagnostic, path string, messagePart string) {
	t.Helper()
	for _, d := range diagnostics {
		if d.Path == path && strings.Contains(d.Message, messagePart) {
			assert.Positive(t, d.Line)
			assert.Positive(t, d.Column)
			return
		}
	}
	t.Fatalf("missing diagnostic for %s containing %q in %#v", path, messagePart, diagnostics)
}

func assertNoDiagnostic(t *testing.T, diagnostics []LintDiagnostic, path string, messagePart string) {
	t.Helper()
	for _, d := range diagnostics {
		if d.Path == path && strings.Contains(d.Message, messagePart) {
			t.Fatalf("unexpected diagnostic for %s containing %q in %#v", path, messagePart, diagnostics)
		}
	}
}
