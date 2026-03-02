package auth

import "sort"

// ProviderAuth defines the authentication configuration for a SaaS provider.
type ProviderAuth struct {
	Name         string   // Provider identifier
	AuthMethod   string   // "oauth2" or "api_key"
	AuthURL      string   // OAuth2 authorization endpoint
	TokenURL     string   // OAuth2 token endpoint
	Scopes       []string // OAuth2 scopes
	ClientID     string   // Built-in client ID (can be overridden via env/flag)
	Instructions string   // Help text shown to the user
}

// KnownProviders maps provider names to their auth configurations.
var KnownProviders = map[string]ProviderAuth{
	"figma": {
		Name:         "figma",
		AuthMethod:   "oauth2",
		AuthURL:      "https://www.figma.com/oauth",
		TokenURL:     "https://api.figma.com/v1/oauth/token",
		Scopes:       []string{"files:read", "file_variables:read"},
		Instructions: "Requires a Figma OAuth app. Set FIGMA_CLIENT_ID and FIGMA_CLIENT_SECRET.",
	},
	"linear": {
		Name:         "linear",
		AuthMethod:   "api_key",
		Instructions: "Create an API key at https://linear.app/settings/api",
	},
	"hubspot": {
		Name:         "hubspot",
		AuthMethod:   "oauth2",
		AuthURL:      "https://app.hubspot.com/oauth/authorize",
		TokenURL:     "https://api.hubapi.com/oauth/v1/token",
		Scopes:       []string{"crm.objects.contacts.read", "settings.users.read"},
		Instructions: "Requires a HubSpot private app. Set HUBSPOT_CLIENT_ID and HUBSPOT_CLIENT_SECRET.",
	},
	"miro": {
		Name:         "miro",
		AuthMethod:   "oauth2",
		AuthURL:      "https://miro.com/oauth/authorize",
		TokenURL:     "https://api.miro.com/v1/oauth/token",
		Scopes:       []string{"team:read"},
		Instructions: "Requires a Miro OAuth app. Set MIRO_CLIENT_ID and MIRO_CLIENT_SECRET.",
	},
	"framer": {
		Name:         "framer",
		AuthMethod:   "api_key",
		Instructions: "Create an API key in Framer project settings.",
	},
	"google-directory": {
		Name:         "google-directory",
		AuthMethod:   "oauth2",
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		Scopes:       []string{"https://www.googleapis.com/auth/admin.directory.user.readonly", "https://www.googleapis.com/auth/admin.directory.group.readonly"},
		Instructions: "Requires a Google Cloud service account with domain-wide delegation.",
	},
	"slack": {
		Name:         "slack",
		AuthMethod:   "api_key",
		Instructions: "Create a SCIM token at https://my.slack.com/admin/settings#scim (requires Business+ or Enterprise Grid).",
	},
	"anthropic": {
		Name:         "anthropic",
		AuthMethod:   "api_key",
		Instructions: "Create an Admin API key at https://console.anthropic.com/settings/admin-keys",
	},
	"claude-code": {
		Name:         "claude-code",
		AuthMethod:   "api_key",
		Instructions: "Uses the same Anthropic Admin API key. Create at https://console.anthropic.com/settings/admin-keys",
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
