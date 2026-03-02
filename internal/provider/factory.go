package provider

import (
	"context"
	"fmt"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/provider/adobe"
	"github.com/sderosiaux/unseat/internal/provider/airtable"
	"github.com/sderosiaux/unseat/internal/provider/anthropic"
	"github.com/sderosiaux/unseat/internal/provider/asana"
	"github.com/sderosiaux/unseat/internal/provider/atlassian"
	"github.com/sderosiaux/unseat/internal/provider/auth0"
	"github.com/sderosiaux/unseat/internal/provider/awsiam"
	"github.com/sderosiaux/unseat/internal/provider/azuread"
	"github.com/sderosiaux/unseat/internal/provider/bamboohr"
	"github.com/sderosiaux/unseat/internal/provider/box"
	"github.com/sderosiaux/unseat/internal/provider/brex"
	"github.com/sderosiaux/unseat/internal/provider/canva"
	"github.com/sderosiaux/unseat/internal/provider/claudecode"
	"github.com/sderosiaux/unseat/internal/provider/clickup"
	"github.com/sderosiaux/unseat/internal/provider/databricks"
	"github.com/sderosiaux/unseat/internal/provider/datadog"
	"github.com/sderosiaux/unseat/internal/provider/deel"
	"github.com/sderosiaux/unseat/internal/provider/discord"
	"github.com/sderosiaux/unseat/internal/provider/docusign"
	"github.com/sderosiaux/unseat/internal/provider/dropbox"
	"github.com/sderosiaux/unseat/internal/provider/figma"
	"github.com/sderosiaux/unseat/internal/provider/framer"
	"github.com/sderosiaux/unseat/internal/provider/freshdesk"
	"github.com/sderosiaux/unseat/internal/provider/gcpiam"
	githubprovider "github.com/sderosiaux/unseat/internal/provider/github"
	"github.com/sderosiaux/unseat/internal/provider/gitlab"
	googleprovider "github.com/sderosiaux/unseat/internal/provider/google"
	"github.com/sderosiaux/unseat/internal/provider/grafana"
	"github.com/sderosiaux/unseat/internal/provider/hubspot"
	"github.com/sderosiaux/unseat/internal/provider/intercom"
	"github.com/sderosiaux/unseat/internal/provider/lastpass"
	"github.com/sderosiaux/unseat/internal/provider/linear"
	"github.com/sderosiaux/unseat/internal/provider/loom"
	"github.com/sderosiaux/unseat/internal/provider/miro"
	"github.com/sderosiaux/unseat/internal/provider/monday"
	"github.com/sderosiaux/unseat/internal/provider/msteams"
	"github.com/sderosiaux/unseat/internal/provider/netlify"
	"github.com/sderosiaux/unseat/internal/provider/newrelic"
	"github.com/sderosiaux/unseat/internal/provider/notion"
	"github.com/sderosiaux/unseat/internal/provider/okta"
	"github.com/sderosiaux/unseat/internal/provider/onepassword"
	"github.com/sderosiaux/unseat/internal/provider/pagerduty"
	"github.com/sderosiaux/unseat/internal/provider/rippling"
	"github.com/sderosiaux/unseat/internal/provider/salesforce"
	"github.com/sderosiaux/unseat/internal/provider/sentry"
	"github.com/sderosiaux/unseat/internal/provider/shortcut"
	slackprovider "github.com/sderosiaux/unseat/internal/provider/slack"
	"github.com/sderosiaux/unseat/internal/provider/snowflake"
	"github.com/sderosiaux/unseat/internal/provider/snyk"
	"github.com/sderosiaux/unseat/internal/provider/stripe"
	"github.com/sderosiaux/unseat/internal/provider/trello"
	"github.com/sderosiaux/unseat/internal/provider/vercel"
	"github.com/sderosiaux/unseat/internal/provider/zendesk"
	"github.com/sderosiaux/unseat/internal/provider/zoom"
)

// BuildRegistry creates a Registry and IdentityProvider from config, initializing
// real provider clients (Google Directory, Linear, etc.).
func BuildRegistry(ctx context.Context, cfg *config.Config) (*Registry, IdentityProvider, error) {
	var identity IdentityProvider
	switch cfg.IdentitySource.Provider {
	case "google-directory":
		var gopts []googleprovider.Option
		if cfg.IdentitySource.AdminEmail != "" {
			gopts = append(gopts, googleprovider.WithAdminEmail(cfg.IdentitySource.AdminEmail))
		}
		gp, err := googleprovider.New(ctx, cfg.IdentitySource.CredentialsFile, cfg.IdentitySource.Domain, gopts...)
		if err != nil {
			return nil, nil, fmt.Errorf("init google directory: %w", err)
		}
		identity = gp
	default:
		return nil, nil, fmt.Errorf("unknown identity provider: %s", cfg.IdentitySource.Provider)
	}
	return BuildRegistryWithIdentity(cfg, identity)
}

// BuildRegistryWithIdentity creates a Registry using a pre-built IdentityProvider.
func BuildRegistryWithIdentity(cfg *config.Config, identity IdentityProvider) (*Registry, IdentityProvider, error) {
	reg := NewRegistry()
	if identity != nil {
		reg.Register(identity)
	}

	for name, pcfg := range cfg.Providers {
		p, err := buildProvider(name, pcfg)
		if err != nil {
			return nil, nil, err
		}
		reg.Register(p)
	}

	return reg, identity, nil
}

// withBase applies BaseURL override when configured and the provider supports it.
type baseURLSetter interface {
	WithBaseURL(string) *struct{} // marker — not used directly
}

func buildProvider(name string, pcfg config.ProviderConfig) (Provider, error) {
	switch name {
	// --- Original providers ---
	case "linear":
		return optBase(linear.New(pcfg.APIKey), pcfg.BaseURL), nil
	case "figma":
		return optBase(figma.New(pcfg.APIKey, pcfg.ExtraArgs["tenant_id"]), pcfg.BaseURL), nil
	case "hubspot":
		return optBase(hubspot.New(pcfg.APIKey), pcfg.BaseURL), nil
	case "miro":
		return optBase(miro.New(pcfg.APIKey, pcfg.ExtraArgs["org_id"]), pcfg.BaseURL), nil
	case "framer":
		return framer.New(), nil
	case "slack":
		return optBase(slackprovider.New(pcfg.APIKey), pcfg.BaseURL), nil
	case "anthropic":
		return optBase(anthropic.New(pcfg.APIKey), pcfg.BaseURL), nil
	case "claude-code":
		return optBase(claudecode.New(pcfg.APIKey), pcfg.BaseURL), nil

	// --- Engineering ---
	case "github":
		return optBase(githubprovider.New(pcfg.APIKey, pcfg.ExtraArgs["org"]), pcfg.BaseURL), nil
	case "gitlab":
		return optBase(gitlab.New(pcfg.APIKey), pcfg.BaseURL), nil
	case "atlassian":
		return optBase(atlassian.New(pcfg.APIKey, pcfg.ExtraArgs["directory_id"]), pcfg.BaseURL), nil
	case "notion":
		return optBase(notion.New(pcfg.APIKey), pcfg.BaseURL), nil
	case "shortcut":
		return optBase(shortcut.New(pcfg.APIKey), pcfg.BaseURL), nil

	// --- Project Management ---
	case "asana":
		return optBase(asana.New(pcfg.APIKey, pcfg.ExtraArgs["workspace_id"]), pcfg.BaseURL), nil
	case "monday":
		return optBase(monday.New(pcfg.APIKey), pcfg.BaseURL), nil
	case "clickup":
		return optBase(clickup.New(pcfg.APIKey, pcfg.ExtraArgs["team_id"]), pcfg.BaseURL), nil
	case "trello":
		return optBase(trello.New(pcfg.APIKey, pcfg.ExtraArgs["token"], pcfg.ExtraArgs["org_id"]), pcfg.BaseURL), nil
	case "vercel":
		return optBase(vercel.New(pcfg.APIKey, pcfg.ExtraArgs["team_id"]), pcfg.BaseURL), nil

	// --- Infrastructure ---
	case "netlify":
		return optBase(netlify.New(pcfg.APIKey, pcfg.ExtraArgs["account_slug"]), pcfg.BaseURL), nil
	case "aws-iam":
		return optBase(awsiam.New(pcfg.APIKey, pcfg.ExtraArgs["scim_endpoint"]), pcfg.BaseURL), nil
	case "gcp-iam":
		return optBase(gcpiam.New(pcfg.APIKey, pcfg.ExtraArgs["customer_id"]), pcfg.BaseURL), nil
	case "azure-ad":
		return optBase(azuread.New(pcfg.APIKey), pcfg.BaseURL), nil
	case "microsoft-teams":
		return optBase(msteams.New(pcfg.APIKey), pcfg.BaseURL), nil

	// --- Observability ---
	case "datadog":
		return optBase(datadog.New(pcfg.APIKey, pcfg.ExtraArgs["app_key"]), pcfg.BaseURL), nil
	case "pagerduty":
		return optBase(pagerduty.New(pcfg.APIKey), pcfg.BaseURL), nil
	case "grafana":
		return optBase(grafana.New(pcfg.APIKey), pcfg.BaseURL), nil
	case "newrelic":
		return optBase(newrelic.New(pcfg.APIKey), pcfg.BaseURL), nil
	case "sentry":
		return optBase(sentry.New(pcfg.APIKey, pcfg.ExtraArgs["org_slug"]), pcfg.BaseURL), nil

	// --- CRM / Support ---
	case "salesforce":
		return optBase(salesforce.New(pcfg.APIKey, pcfg.ExtraArgs["instance_url"]), pcfg.BaseURL), nil
	case "intercom":
		return optBase(intercom.New(pcfg.APIKey), pcfg.BaseURL), nil
	case "zendesk":
		return optBase(zendesk.New(pcfg.APIKey, pcfg.ExtraArgs["subdomain"]), pcfg.BaseURL), nil
	case "freshdesk":
		return optBase(freshdesk.New(pcfg.APIKey, pcfg.ExtraArgs["subdomain"]), pcfg.BaseURL), nil

	// --- Communication ---
	case "zoom":
		return optBase(zoom.New(pcfg.APIKey), pcfg.BaseURL), nil
	case "discord":
		return optBase(discord.New(pcfg.APIKey, pcfg.ExtraArgs["guild_id"]), pcfg.BaseURL), nil
	case "loom":
		return optBase(loom.New(pcfg.APIKey, pcfg.ExtraArgs["space_id"]), pcfg.BaseURL), nil

	// --- Storage ---
	case "dropbox":
		return optBase(dropbox.New(pcfg.APIKey), pcfg.BaseURL), nil
	case "box":
		return optBase(box.New(pcfg.APIKey), pcfg.BaseURL), nil

	// --- Security / Identity ---
	case "1password":
		return optBase(onepassword.New(pcfg.APIKey), pcfg.BaseURL), nil
	case "lastpass":
		return optBase(lastpass.New(pcfg.APIKey, pcfg.ExtraArgs["provisioning_hash"]), pcfg.BaseURL), nil
	case "okta":
		return optBase(okta.New(pcfg.APIKey, pcfg.ExtraArgs["domain"]), pcfg.BaseURL), nil
	case "auth0":
		return optBase(auth0.New(pcfg.APIKey, pcfg.ExtraArgs["domain"]), pcfg.BaseURL), nil
	case "snyk":
		return optBase(snyk.New(pcfg.APIKey, pcfg.ExtraArgs["org_id"]), pcfg.BaseURL), nil

	// --- Design ---
	case "canva":
		return optBase(canva.New(pcfg.APIKey), pcfg.BaseURL), nil
	case "adobe":
		return optBase(adobe.New(pcfg.APIKey, pcfg.ExtraArgs["org_id"]), pcfg.BaseURL), nil
	case "docusign":
		return optBase(docusign.New(pcfg.APIKey, pcfg.ExtraArgs["org_id"]), pcfg.BaseURL), nil

	// --- Finance ---
	case "stripe":
		return stripe.New(), nil
	case "brex":
		return optBase(brex.New(pcfg.APIKey), pcfg.BaseURL), nil

	// --- HR ---
	case "rippling":
		return optBase(rippling.New(pcfg.APIKey), pcfg.BaseURL), nil
	case "bamboohr":
		return optBase(bamboohr.New(pcfg.APIKey, pcfg.ExtraArgs["subdomain"]), pcfg.BaseURL), nil
	case "deel":
		return optBase(deel.New(pcfg.APIKey), pcfg.BaseURL), nil

	// --- Data ---
	case "airtable":
		return optBase(airtable.New(pcfg.APIKey, pcfg.ExtraArgs["enterprise_account_id"]), pcfg.BaseURL), nil
	case "snowflake":
		return optBase(snowflake.New(pcfg.APIKey, pcfg.ExtraArgs["account"]), pcfg.BaseURL), nil
	case "databricks":
		return optBase(databricks.New(pcfg.APIKey, pcfg.ExtraArgs["workspace"]), pcfg.BaseURL), nil

	default:
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
}

// optBase is a helper that applies WithBaseURL if baseURL is non-empty.
// It uses an interface to avoid repeating the pattern for every provider.
type withBaseURL interface {
	WithBaseURL(string)
}

// We use a generic approach since each provider's WithBaseURL returns its own *Provider type.
func optBase[T interface{ WithBaseURL(string) T }](p T, baseURL string) T {
	if baseURL != "" {
		return p.WithBaseURL(baseURL)
	}
	return p
}
