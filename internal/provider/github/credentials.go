package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

// installation is one third-party GitHub App installed on the organisation.
//
// Only the fields the live API actually returns are declared. There is no
// installed_by: GitHub does not say who authorised an installation anywhere in
// this payload, which is why the audit log is consulted separately below.
type installation struct {
	ID   int64  `json:"id"`
	Slug string `json:"app_slug"`
	// ClientID is what ties an installation to its audit-log event, which
	// carries application_client_id rather than a slug or an app id.
	ClientID string `json:"client_id"`
	// RepositorySelection is "all" or "selected". "all" includes repositories
	// created after the installation, which is what makes it worth reporting.
	RepositorySelection string `json:"repository_selection"`
	// Permissions maps a permission name to "read", "write" or "admin".
	Permissions map[string]string `json:"permissions"`
	CreatedAt   string            `json:"created_at"`
	SuspendedAt string            `json:"suspended_at"`
	SuspendedBy *struct {
		Login string `json:"login"`
	} `json:"suspended_by"`
	HTMLURL string `json:"html_url"`
}

type installationsResponse struct {
	TotalCount    int            `json:"total_count"`
	Installations []installation `json:"installations"`
}

// installEvent is one integration_installation.create entry. The field names
// were read off a live response, not off the documentation: the actor's login
// is actor, and the app is identified by application_client_id.
type installEvent struct {
	Actor               string `json:"actor"`
	ApplicationClientID string `json:"application_client_id"`
}

// privilegedPermissions returns the permissions that carry write or
// administrative power, sorted so the output is stable.
//
// GitHub's vocabulary is "read" / "write" / "admin"; only this connector knows
// that, which is why core is handed the answer rather than the raw map.
func privilegedPermissions(perms map[string]string) []string {
	var out []string
	for name, level := range perms {
		if level == "write" || level == "admin" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func permissionNames(perms map[string]string) []string {
	out := make([]string, 0, len(perms))
	for name := range perms {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// parseGitHubTime reads an RFC3339 timestamp, returning nil rather than a zero
// time so an absent date never renders as 1970.
func parseGitHubTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}

// ListCredentials reports the third-party apps installed on the organisation.
//
// These are not seats: they cost nothing per month and suspending a person
// does not touch them. That is exactly why they belong here — an app installed
// by someone who has since left keeps its permissions until a human notices,
// and no offboarding process looks at this list.
//
// No usage is reported, and Capabilities says so. GitHub gives an installation
// a created_at and an updated_at, and updated_at moves when permissions change,
// never when the app acts. Treating it as activity would mark a dormant app
// "recently used" the day someone edits its scopes.
func (p *Provider) ListCredentials(ctx context.Context) ([]core.Credential, error) {
	installs, err := p.fetchInstallations(ctx)
	if err != nil {
		return nil, err
	}

	// Best-effort and deliberately non-fatal: the audit log is Enterprise-only
	// and has a retention window, so it names the installer of recent apps and
	// nothing about the older ones. Those stay unattributed, which classifies
	// them as unowned — an honest "GitHub will not say" rather than a guess.
	installers := p.fetchInstallers(ctx)

	creds := make([]core.Credential, 0, len(installs))
	for _, in := range installs {
		c := core.Credential{
			Provider:         "github",
			Kind:             core.CredentialAppInstallation,
			ID:               strconv.FormatInt(in.ID, 10),
			Label:            in.Slug,
			CreatedAt:        parseGitHubTime(in.CreatedAt),
			CreatedBy:        installers[in.ClientID],
			Scopes:           permissionNames(in.Permissions),
			PrivilegedScopes: privilegedPermissions(in.Permissions),
			Reach:            reachFromSelection(in.RepositorySelection),
		}

		if suspended := parseGitHubTime(in.SuspendedAt); suspended != nil {
			c.Disabled = true
			c.DisabledAt = suspended
		}

		meta := map[string]string{"app_slug": in.Slug}
		if in.HTMLURL != "" {
			meta["url"] = in.HTMLURL
		}
		if in.SuspendedBy != nil && in.SuspendedBy.Login != "" {
			meta["suspended_by"] = in.SuspendedBy.Login
		}
		c.Metadata = meta

		creds = append(creds, c)
	}

	return creds, nil
}

// reachFromSelection maps GitHub's repository_selection onto the shared
// vocabulary, leaving anything unrecognised as unknown rather than guessing
// narrow.
func reachFromSelection(selection string) string {
	switch selection {
	case "all":
		return core.ReachAll
	case "selected":
		return core.ReachSelected
	default:
		return core.ReachUnknown
	}
}

func (p *Provider) fetchInstallations(ctx context.Context) ([]installation, error) {
	var all []installation

	for page := 1; ; page++ {
		url := fmt.Sprintf("%s/orgs/%s/installations?per_page=100&page=%d", p.baseURL, p.org, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+p.token)
		req.Header.Set("Accept", "application/vnd.github+json")

		var resp installationsResponse
		if err := p.client.DoJSON(ctx, "github", req, &resp); err != nil {
			if httpclient.IsUnauthorized(err) {
				return nil, fmt.Errorf("%w\n  Grant the `admin:org` scope to the token: listing installed apps "+
					"is an org-administration read, and a `read:org` token cannot see it", err)
			}
			return nil, err
		}

		all = append(all, resp.Installations...)

		// TotalCount is the org-wide total, so it terminates the walk exactly
		// rather than relying on a short page — which would stop early if a
		// page ever came back trimmed.
		if len(resp.Installations) == 0 || len(all) >= resp.TotalCount {
			return all, nil
		}
	}
}

// fetchInstallers maps an app's client id to the login that installed it.
//
// Returns an empty map on any failure. The audit log answers 403 or 404 for a
// non-Enterprise org or a token without read:audit_log, and that is an absence
// of evidence, not an error worth failing the whole listing over.
//
// The login is returned as-is rather than resolved to an email: mapping a
// GitHub handle to a corporate identity is the alias index's job, and it is
// applied once, in core, for every provider.
func (p *Provider) fetchInstallers(ctx context.Context) map[string]string {
	installers := make(map[string]string)

	url := fmt.Sprintf("%s/orgs/%s/audit-log?include=all&per_page=100&phrase=%s",
		p.baseURL, p.org, "action%3Aintegration_installation.create")

	for url != "" {
		events, next, status, err := p.fetchInstallEventPage(ctx, url)
		if err != nil || status != http.StatusOK {
			return installers
		}
		for _, e := range events {
			// First entry wins: the log is newest-first, and a re-installed app
			// belongs to whoever installed the one that exists now.
			if e.ApplicationClientID != "" && e.Actor != "" {
				if _, seen := installers[e.ApplicationClientID]; !seen {
					installers[e.ApplicationClientID] = e.Actor
				}
			}
		}
		url = next
	}

	return installers
}

func (p *Provider) fetchInstallEventPage(ctx context.Context, pageURL string) (events []installEvent, nextURL string, statusCode int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	// Raw Do, like the activity walk: a 403/404 is the expected "not
	// Enterprise" answer rather than an error to surface.
	resp, err := p.client.Do(ctx, req)
	if err != nil {
		return nil, "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", resp.StatusCode, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", resp.StatusCode, fmt.Errorf("github: read response: %w", err)
	}
	if err := json.Unmarshal(body, &events); err != nil {
		return nil, "", resp.StatusCode, fmt.Errorf("github: decode response: %w", err)
	}

	return events, nextPageURL(resp.Header.Get("Link")), resp.StatusCode, nil
}
