package core

import (
	"sort"
	"strings"
	"time"
)

// CredentialKind is what sort of non-human access a credential represents.
//
// The kinds differ in what revoking one costs. Uninstalling a dormant app is
// free; revoking the integration that posts every Linear update into Slack
// breaks a workflow a whole team relies on. The report has to say which is
// which, so the kind travels with the finding.
type CredentialKind string

const (
	// CredentialAppInstallation: a third-party app installed on the org, e.g.
	// a GitHub App with permissions across repositories.
	CredentialAppInstallation CredentialKind = "app_installation"
	// CredentialIntegration: a first-party connection the SaaS itself offers,
	// e.g. Linear's Slack or Zendesk integration.
	CredentialIntegration CredentialKind = "integration"
	// CredentialWebhook: an outbound hook carrying org data to a URL.
	CredentialWebhook CredentialKind = "webhook"
	// CredentialAPIKey: a token issued for programmatic access.
	CredentialAPIKey CredentialKind = "api_key"
	// CredentialDeployKey: a key granting repository access outside the user model.
	CredentialDeployKey CredentialKind = "deploy_key"
	// CredentialOAuthGrant: a third party authorised against one person's account.
	CredentialOAuthGrant CredentialKind = "oauth_grant"
	// CredentialServiceAccount: a directory identity that is not a person.
	CredentialServiceAccount CredentialKind = "service_account"
)

// Credential reach describes how much of the org a credential can touch.
const (
	// ReachAll: every resource, including ones created after the grant.
	ReachAll = "all"
	// ReachSelected: an explicit subset chosen at install time.
	ReachSelected = "selected"
	// ReachUnknown: the API does not say. Not the same as narrow.
	ReachUnknown = ""
)

// Credential is a non-human identity holding access to a SaaS: an installed
// app, an integration, a webhook, a token.
//
// It matters because the whole deprovisioning model assumes access is attached
// to people. These are not. Suspending a Google account does nothing to the
// OAuth integration that person authorised, which keeps running under their
// name until it breaks or somebody notices.
type Credential struct {
	Provider string         `json:"provider"`
	Kind     CredentialKind `json:"kind"`
	// ID is the provider's own identifier, so a finding can be acted on in the
	// vendor's UI.
	ID string `json:"id"`
	// Label is what an operator would recognise it by.
	Label     string     `json:"label"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	// CreatedBy is the identity that authorised this credential, as the
	// provider reports it — usually an email. Empty means the API does not say,
	// which is a different thing from nobody owning it.
	CreatedBy string `json:"created_by,omitempty"`
	// LastUsedAt is populated only by connectors whose Capabilities declare
	// ReportsCredentialUsage. Everywhere else it stays nil and means "unknown",
	// never "unused" — an install date that moves when permissions change is
	// not a usage signal.
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	// Scopes are the permissions as the vendor names them, for display.
	Scopes []string `json:"scopes,omitempty"`
	// PrivilegedScopes are the subset the connector judges to carry write or
	// administrative power. Only the connector can decide this: "write" means
	// something different in every vendor's vocabulary, and core guessing at it
	// would be inventing a severity.
	PrivilegedScopes []string `json:"privileged_scopes,omitempty"`
	// Reach is one of the Reach* constants.
	Reach string `json:"reach,omitempty"`
	// Disabled reports that the credential is installed but inert — suspended,
	// paused, or switched off. It still exists, and can usually be switched
	// back on by any admin.
	Disabled   bool              `json:"disabled"`
	DisabledAt *time.Time        `json:"disabled_at,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type CredentialInventory struct {
	Credentials []Credential `json:"credentials"`
	Warnings    []string     `json:"warnings,omitempty"`
}

// CredentialClass is the verdict on one credential relative to the directory.
type CredentialClass string

const (
	// CredentialOrphaned: authorised by an identity the directory no longer
	// carries as active. This is the one that matters — offboarding did not
	// and could not touch it.
	CredentialOrphaned CredentialClass = "orphaned"
	// CredentialDormant: installed but switched off. Dead weight, and a
	// standing invitation to be switched back on.
	CredentialDormant CredentialClass = "dormant"
	// CredentialUnowned: the provider exposes no author, so responsibility
	// cannot be assigned. Reported, never judged.
	CredentialUnowned CredentialClass = "unowned"
	// CredentialExternal: authorised from outside the corporate domain.
	CredentialExternal CredentialClass = "external"
	// CredentialLive: authorised by a current employee.
	CredentialLive CredentialClass = "live"
)

// ClassifiedCredential is one credential with its verdict.
type ClassifiedCredential struct {
	Credential Credential      `json:"credential"`
	Class      CredentialClass `json:"class"`
	Reason     string          `json:"reason"`
	// Overreaching is orthogonal to Class: a credential authorised by a current
	// employee can still hold write access to everything. Folding it into the
	// class would force a choice between two facts that are both true.
	Overreaching bool   `json:"overreaching"`
	ReachReason  string `json:"reach_reason,omitempty"`
}

// ClassifyCredentialsInput carries one provider's credentials and the
// directory to judge them against.
type ClassifyCredentialsInput struct {
	Credentials []Credential
	// Directory is every identity known to the identity source, keyed by
	// lowercased email. A nil map means it could not be read, and every
	// credential degrades to unowned rather than being called orphaned.
	Directory map[string]DirectoryUser
	// Domain is the corporate domain.
	Domain string
	// AliasIndex maps a provider-side identifier to a canonical corporate
	// email, reusing the same index seat classification uses.
	AliasIndex map[string]string
}

// ClassifyCredentials assigns a verdict to every credential.
//
// Order matters and encodes a judgement. A disabled credential is settled
// first, whoever created it: it cannot act, so the recommendation is to
// uninstall it, not to find it a new owner. Only among credentials that can
// actually do something does the author's employment status decide.
func ClassifyCredentials(input ClassifyCredentialsInput) []ClassifiedCredential {
	domain := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(input.Domain), "@"))
	out := make([]ClassifiedCredential, 0, len(input.Credentials))

	for _, c := range input.Credentials {
		cc := ClassifiedCredential{Credential: c}

		// Reach is a property of the grant, independent of who made it, so it
		// is assessed for every credential including dormant ones — a
		// suspended app with admin over every repository is one click from
		// being live again.
		if c.Reach == ReachAll && len(c.PrivilegedScopes) > 0 {
			cc.Overreaching = true
			cc.ReachReason = "write or admin access to every resource: " + strings.Join(c.PrivilegedScopes, ", ")
		}

		author := resolveAlias(input.AliasIndex, c.CreatedBy)

		switch {
		case c.Disabled:
			cc.Class = CredentialDormant
			cc.Reason = "installed but switched off" + sinceSuffix(c.DisabledAt) +
				" — it still exists and any admin can switch it back on"

		case author == "" || !strings.Contains(author, "@"):
			cc.Class = CredentialUnowned
			cc.Reason = "the provider does not report who authorised this"

		case domain != "" && !strings.HasSuffix(author, "@"+domain):
			cc.Class = CredentialExternal
			cc.Reason = "authorised from outside the corporate domain " + domain

		case input.Directory == nil:
			cc.Class = CredentialUnowned
			cc.Reason = "directory unavailable — cannot determine whether " + author + " is still here"

		default:
			dir, known := input.Directory[author]
			switch {
			case !known:
				cc.Class = CredentialOrphaned
				cc.Reason = author + " is not in the directory, and this access outlived them"
			case dir.Suspended:
				cc.Class = CredentialOrphaned
				cc.Reason = author + " is suspended in the directory, and this access outlived them"
			default:
				cc.Class = CredentialLive
				cc.Reason = "authorised by " + author + ", a current employee"
			}
		}

		out = append(out, cc)
	}

	sortCredentials(out)
	return out
}

// sinceSuffix renders " since 2021-07-26" when a date is known, and nothing
// when it is not — rather than a zero date that reads as 1970.
func sinceSuffix(t *time.Time) string {
	if t == nil {
		return ""
	}
	return " since " + t.Format("2006-01-02")
}

// sortCredentials puts the findings that need a decision first: orphaned
// before dormant before the rest, then oldest first within a class, because
// age is the argument.
func sortCredentials(c []ClassifiedCredential) {
	rank := map[CredentialClass]int{
		CredentialOrphaned: 0,
		CredentialExternal: 1,
		CredentialDormant:  2,
		CredentialUnowned:  3,
		CredentialLive:     4,
	}
	sort.SliceStable(c, func(i, j int) bool {
		if ri, rj := rank[c[i].Class], rank[c[j].Class]; ri != rj {
			return ri < rj
		}
		ti, tj := c[i].Credential.CreatedAt, c[j].Credential.CreatedAt
		switch {
		case ti != nil && tj != nil && !ti.Equal(*tj):
			return ti.Before(*tj)
		case ti != nil && tj == nil:
			return true
		case ti == nil && tj != nil:
			return false
		}
		if c[i].Credential.Provider != c[j].Credential.Provider {
			return c[i].Credential.Provider < c[j].Credential.Provider
		}
		return c[i].Credential.Label < c[j].Credential.Label
	})
}

// CredentialSummary counts credentials per class for a provider.
type CredentialSummary struct {
	Provider     string `json:"provider"`
	Orphaned     int    `json:"orphaned"`
	Dormant      int    `json:"dormant"`
	Unowned      int    `json:"unowned"`
	External     int    `json:"external"`
	Live         int    `json:"live"`
	Overreaching int    `json:"overreaching"`
	Total        int    `json:"total"`
	// UsageKnown reports whether the connector populates LastUsedAt from a
	// genuine usage signal. When false, no credential here can be called
	// unused, however old it looks.
	UsageKnown bool `json:"usage_known"`
}

// SummarizeCredentials aggregates classified credentials into per-class counts.
func SummarizeCredentials(provider string, creds []ClassifiedCredential, usageKnown bool) CredentialSummary {
	s := CredentialSummary{Provider: provider, Total: len(creds), UsageKnown: usageKnown}
	for _, c := range creds {
		if c.Overreaching {
			s.Overreaching++
		}
		switch c.Class {
		case CredentialOrphaned:
			s.Orphaned++
		case CredentialDormant:
			s.Dormant++
		case CredentialUnowned:
			s.Unowned++
		case CredentialExternal:
			s.External++
		case CredentialLive:
			s.Live++
		}
	}
	return s
}
