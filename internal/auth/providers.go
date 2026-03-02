package auth

import "sort"

// ProviderAuth defines the authentication configuration for a SaaS provider.
type ProviderAuth struct {
	Name         string   // Provider identifier
	AuthMethod   string   // "oauth2", "api_key", "basic", "stub"
	AuthURL      string   // OAuth2 authorization endpoint
	TokenURL     string   // OAuth2 token endpoint
	Scopes       []string // OAuth2 scopes
	ClientID     string   // Built-in client ID (can be overridden via env/flag)
	Instructions string   // Help text shown to the user
}

// KnownProviders maps provider names to their auth configurations.
var KnownProviders = map[string]ProviderAuth{
	// --- Identity ---
	"google-directory": {
		Name:         "google-directory",
		AuthMethod:   "oauth2",
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		Scopes:       []string{"https://www.googleapis.com/auth/admin.directory.user.readonly", "https://www.googleapis.com/auth/admin.directory.group.readonly"},
		Instructions: "Requires a Google Cloud service account with domain-wide delegation.",
	},

	// --- Original providers ---
	"linear": {
		Name:         "linear",
		AuthMethod:   "api_key",
		Instructions: "Create an API key at https://linear.app/settings/api",
	},
	"figma": {
		Name:         "figma",
		AuthMethod:   "api_key",
		Instructions: "Create a SCIM token in Figma Admin settings (Enterprise plan). Also requires tenant_id.",
	},
	"hubspot": {
		Name:         "hubspot",
		AuthMethod:   "oauth2",
		AuthURL:      "https://app.hubspot.com/oauth/authorize",
		TokenURL:     "https://api.hubapi.com/oauth/v1/token",
		Scopes:       []string{"crm.objects.contacts.read", "settings.users.read"},
		Instructions: "Requires a HubSpot private app.",
	},
	"miro": {
		Name:         "miro",
		AuthMethod:   "api_key",
		Instructions: "Create a token at https://miro.com/app/settings/user-profile/apps. Requires org_id.",
	},
	"framer": {
		Name:         "framer",
		AuthMethod:   "stub",
		Instructions: "Framer has no public user management API.",
	},
	"slack": {
		Name:         "slack",
		AuthMethod:   "api_key",
		Instructions: "Create a SCIM token at https://my.slack.com/admin/settings#scim (Business+ or Enterprise Grid).",
	},
	"anthropic": {
		Name:         "anthropic",
		AuthMethod:   "api_key",
		Instructions: "Create an Admin API key at https://console.anthropic.com/settings/admin-keys",
	},
	"claude-code": {
		Name:         "claude-code",
		AuthMethod:   "api_key",
		Instructions: "Uses Anthropic Admin API key. https://console.anthropic.com/settings/admin-keys",
	},

	// --- Engineering ---
	"github": {
		Name:         "github",
		AuthMethod:   "api_key",
		Instructions: "Create a PAT (classic) with admin:org scope at https://github.com/settings/tokens. Requires extra.org.",
	},
	"gitlab": {
		Name:         "gitlab",
		AuthMethod:   "api_key",
		Instructions: "Create a PAT with admin scope at https://gitlab.com/-/user_settings/personal_access_tokens",
	},
	"atlassian": {
		Name:         "atlassian",
		AuthMethod:   "api_key",
		Instructions: "Create a SCIM API token in Atlassian admin. Requires extra.directory_id.",
	},
	"notion": {
		Name:         "notion",
		AuthMethod:   "api_key",
		Instructions: "Create a SCIM token in Notion Settings > Security & Identity > SCIM provisioning (Enterprise plan).",
	},
	"shortcut": {
		Name:         "shortcut",
		AuthMethod:   "api_key",
		Instructions: "Create an API token at https://app.shortcut.com/settings/account/api-tokens",
	},

	// --- Project Management ---
	"asana": {
		Name:         "asana",
		AuthMethod:   "api_key",
		Instructions: "Create a PAT at https://app.asana.com/0/developer-console. Requires extra.workspace_id.",
	},
	"monday": {
		Name:         "monday",
		AuthMethod:   "api_key",
		Instructions: "Create an API token at https://monday.com > Admin > Developers. Requires admin permissions.",
	},
	"clickup": {
		Name:         "clickup",
		AuthMethod:   "api_key",
		Instructions: "Create an API token at https://app.clickup.com/settings/apps. Requires extra.team_id.",
	},
	"trello": {
		Name:         "trello",
		AuthMethod:   "api_key",
		Instructions: "Create key at https://trello.com/power-ups/admin. Requires extra.token and extra.org_id.",
	},
	"vercel": {
		Name:         "vercel",
		AuthMethod:   "api_key",
		Instructions: "Create a token at https://vercel.com/account/tokens. Requires extra.team_id.",
	},

	// --- Infrastructure ---
	"netlify": {
		Name:         "netlify",
		AuthMethod:   "api_key",
		Instructions: "Create a PAT at https://app.netlify.com/user/applications#personal-access-tokens. Requires extra.account_slug.",
	},
	"aws-iam": {
		Name:         "aws-iam",
		AuthMethod:   "api_key",
		Instructions: "Use AWS IAM Identity Center SCIM token. Requires extra.scim_endpoint.",
	},
	"gcp-iam": {
		Name:         "gcp-iam",
		AuthMethod:   "api_key",
		Instructions: "Use a GCP service account with Cloud Identity read access. Requires extra.customer_id.",
	},
	"azure-ad": {
		Name:         "azure-ad",
		AuthMethod:   "api_key",
		Instructions: "Register an app in Azure AD, grant User.ReadWrite.All in Microsoft Graph.",
	},
	"microsoft-teams": {
		Name:         "microsoft-teams",
		AuthMethod:   "api_key",
		Instructions: "Same as azure-ad. Uses Microsoft Graph with Teams license filter.",
	},

	// --- Observability ---
	"datadog": {
		Name:         "datadog",
		AuthMethod:   "api_key",
		Instructions: "Create API + Application keys at https://app.datadoghq.com/organization-settings/api-keys. Requires extra.app_key.",
	},
	"pagerduty": {
		Name:         "pagerduty",
		AuthMethod:   "api_key",
		Instructions: "Create a REST API key at https://support.pagerduty.com/docs/api-access-keys",
	},
	"grafana": {
		Name:         "grafana",
		AuthMethod:   "api_key",
		Instructions: "Create a service account token in Grafana > Administration > Service accounts.",
	},
	"newrelic": {
		Name:         "newrelic",
		AuthMethod:   "api_key",
		Instructions: "Create a User API key at https://one.newrelic.com/api-keys",
	},
	"sentry": {
		Name:         "sentry",
		AuthMethod:   "api_key",
		Instructions: "Create an auth token at https://sentry.io/settings/account/api/auth-tokens/. Requires extra.org_slug.",
	},

	// --- CRM / Support ---
	"salesforce": {
		Name:         "salesforce",
		AuthMethod:   "oauth2",
		AuthURL:      "https://login.salesforce.com/services/oauth2/authorize",
		TokenURL:     "https://login.salesforce.com/services/oauth2/token",
		Scopes:       []string{"api", "manage_user"},
		Instructions: "Use a Connected App with OAuth. Requires extra.instance_url.",
	},
	"intercom": {
		Name:         "intercom",
		AuthMethod:   "api_key",
		Instructions: "Create a token at https://app.intercom.com/a/apps/_/developer-hub",
	},
	"zendesk": {
		Name:         "zendesk",
		AuthMethod:   "api_key",
		Instructions: "Create a token in Zendesk Admin > Channels > API. Requires extra.subdomain.",
	},
	"freshdesk": {
		Name:         "freshdesk",
		AuthMethod:   "basic",
		Instructions: "Find API key in Freshdesk > Profile settings. Uses Basic auth. Requires extra.subdomain.",
	},

	// --- Communication ---
	"zoom": {
		Name:         "zoom",
		AuthMethod:   "api_key",
		Instructions: "Create a Server-to-Server OAuth app at https://marketplace.zoom.us/develop/create",
	},
	"discord": {
		Name:         "discord",
		AuthMethod:   "api_key",
		Instructions: "Create a bot at https://discord.com/developers/applications. Requires extra.guild_id.",
	},
	"loom": {
		Name:         "loom",
		AuthMethod:   "api_key",
		Instructions: "Create an API token in Loom workspace settings. Requires extra.space_id.",
	},

	// --- Storage ---
	"dropbox": {
		Name:         "dropbox",
		AuthMethod:   "api_key",
		Instructions: "Create a team-scoped app at https://www.dropbox.com/developers/apps",
	},
	"box": {
		Name:         "box",
		AuthMethod:   "api_key",
		Instructions: "Create a server authentication app at https://developer.box.com/",
	},

	// --- Security / Identity ---
	"1password": {
		Name:         "1password",
		AuthMethod:   "api_key",
		Instructions: "Set up 1Password SCIM bridge. Use the bearer token from the bridge configuration.",
	},
	"lastpass": {
		Name:         "lastpass",
		AuthMethod:   "api_key",
		Instructions: "Find CID and Provisioning Hash in LastPass Admin Console > Advanced. Requires extra.provisioning_hash.",
	},
	"okta": {
		Name:         "okta",
		AuthMethod:   "api_key",
		Instructions: "Create an API token at https://{domain}.okta.com/admin/access/api/tokens. Requires extra.domain.",
	},
	"auth0": {
		Name:         "auth0",
		AuthMethod:   "api_key",
		Instructions: "Create a Management API token in Auth0 Dashboard > APIs. Requires extra.domain.",
	},
	"snyk": {
		Name:         "snyk",
		AuthMethod:   "api_key",
		Instructions: "Create a service account token at https://app.snyk.io/org/settings/service-accounts. Requires extra.org_id.",
	},

	// --- Design ---
	"canva": {
		Name:         "canva",
		AuthMethod:   "api_key",
		Instructions: "Create a SCIM token in Canva Team admin settings (Enterprise plan).",
	},
	"adobe": {
		Name:         "adobe",
		AuthMethod:   "api_key",
		Instructions: "Create OAuth credentials in Adobe Admin Console. Requires extra.org_id (format: 12345@AdobeOrg).",
	},
	"docusign": {
		Name:         "docusign",
		AuthMethod:   "oauth2",
		AuthURL:      "https://account.docusign.com/oauth/auth",
		TokenURL:     "https://account.docusign.com/oauth/token",
		Scopes:       []string{"user_management_read", "user_management_write", "organization_read"},
		Instructions: "Use DocuSign Admin API with OAuth. Requires extra.org_id.",
	},

	// --- Finance ---
	"stripe": {
		Name:         "stripe",
		AuthMethod:   "stub",
		Instructions: "Stripe has no public API for dashboard user management.",
	},
	"brex": {
		Name:         "brex",
		AuthMethod:   "api_key",
		Instructions: "Create a token at https://developer.brex.com/. Requires user.read and user.update scopes.",
	},

	// --- HR ---
	"rippling": {
		Name:         "rippling",
		AuthMethod:   "api_key",
		Instructions: "Set up SCIM provisioning in Rippling admin. Use the SCIM bearer token.",
	},
	"bamboohr": {
		Name:         "bamboohr",
		AuthMethod:   "basic",
		Instructions: "Create an API key in BambooHR > Account > API Keys. Uses Basic auth. Requires extra.subdomain.",
	},
	"deel": {
		Name:         "deel",
		AuthMethod:   "api_key",
		Instructions: "Create an API token at https://app.deel.com/developer-center",
	},

	// --- Data ---
	"airtable": {
		Name:         "airtable",
		AuthMethod:   "api_key",
		Instructions: "Create a PAT at https://airtable.com/create/tokens. Requires extra.enterprise_account_id.",
	},
	"snowflake": {
		Name:         "snowflake",
		AuthMethod:   "api_key",
		Instructions: "Set up SCIM provisioning in Snowflake. Requires extra.account.",
	},
	"databricks": {
		Name:         "databricks",
		AuthMethod:   "api_key",
		Instructions: "Create a PAT in Databricks workspace settings. Requires extra.workspace.",
	},
}

// ListKnownProviders returns sorted provider names.
func ListKnownProviders() []string {
	names := make([]string, 0, len(KnownProviders))
	for name := range KnownProviders {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
