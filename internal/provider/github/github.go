package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const (
	defaultBaseURL    = "https://api.github.com"
	defaultGraphQLURL = "https://api.github.com/graphql"
)

type Provider struct {
	token      string
	org        string
	baseURL    string
	graphqlURL string
	client     *httpclient.Client
}

func New(token, org string) *Provider {
	return &Provider{token: token, org: org, baseURL: defaultBaseURL, graphqlURL: defaultGraphQLURL, client: httpclient.New()}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	p.graphqlURL = url + "/graphql"
	return p
}

func (p *Provider) Name() string { return "github" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
		// Deliberately false even though ListUsers does populate LastActivityAt.
		//
		// core reads ReportsActivity as "a nil LastActivityAt means this person
		// has never been active". The only activity source available to a normal
		// org token is /orgs/{org}/events, which carries PUBLIC events only,
		// capped at 300 events over 30 days — so in any real org the majority of
		// members legitimately have no event, and claiming activity reporting
		// would flag them all as inactive and bill the "waste" to them.
		// A timestamp we do set is trustworthy; its absence proves nothing.
		ReportsActivity: false,
	}
}

type orgMember struct {
	Login   string `json:"login"`
	ID      int    `json:"id"`
	SiteURL string `json:"html_url"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	if err := p.verifyOrgVisibility(ctx); err != nil {
		return nil, err
	}

	members, err := p.fetchMembers(ctx, "")
	if err != nil {
		return nil, err
	}

	// Org owners are only visible through the role filter: the member objects
	// returned by the unfiltered listing carry no role at all.
	admins, err := p.fetchMembers(ctx, "admin")
	if err != nil {
		return nil, err
	}
	isAdmin := make(map[string]bool, len(admins))
	for _, a := range admins {
		isAdmin[strings.ToLower(a.Login)] = true
	}

	// Try SAML identity mapping first (orgs with SSO).
	// Maps login -> corporate email (nameId).
	samlMap := p.fetchSAMLIdentities(ctx)

	activityMap := p.fetchOrgActivity(ctx)

	// Resolve an email per member: SAML nameId first, public profile second.
	//
	// When neither resolves, Email keeps the bare GitHub login, and it must stay
	// bare: core.ClassifySeats keys on the absence of "@" to class the seat
	// SeatUnresolved ("provider reports a username with no email"), which is the
	// honest verdict for a member GitHub refuses to identify. Synthesising an
	// address such as {login}@users.noreply.github.com would instead produce a
	// confident external/orphan verdict about a mailbox nobody can act on.
	all := make([]core.User, 0, len(members))
	for _, m := range members {
		var email, displayName, emailSource string
		if samlEmail, ok := samlMap[m.Login]; ok {
			email, displayName, emailSource = samlEmail, m.Login, "saml"
		} else {
			var resolved bool
			email, displayName, resolved = p.fetchUserEmail(ctx, m.Login)
			if displayName == "" {
				displayName = m.Login
			}
			emailSource = "profile"
			if !resolved {
				emailSource = "unresolved"
			}
		}
		role := "member"
		if isAdmin[strings.ToLower(m.Login)] {
			role = "admin"
		}
		u := core.User{
			Email:       email,
			DisplayName: displayName,
			Role:        role,
			Status:      "active",
			ProviderID:  fmt.Sprintf("%d", m.ID),
			Metadata:    map[string]string{"login": m.Login, "email_source": emailSource},
		}
		if t, ok := activityMap[strings.ToLower(m.Login)]; ok {
			u.LastActivityAt = &t
		}
		all = append(all, u)
	}

	return all, nil
}

// verifyOrgVisibility refuses to sync unless the token provably sees private members.
//
// GET /orgs/{org}/members does not reject an outsider: it answers 200 with only
// the PUBLIC members. That truncated list is indistinguishable from a complete
// one, so reconciliation reads every private member as missing from the provider
// and proposes adding them back. Failing here is the only safe outcome.
//
// A 200 from /user/memberships/orgs/{org} proves both facts at once: the
// endpoint requires org read access, and it only exists for actual members.
func (p *Provider) verifyOrgVisibility(ctx context.Context) error {
	url := fmt.Sprintf("%s/user/memberships/orgs/%s", p.baseURL, p.org)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	// Kept on the raw Do path rather than DoJSON: the diagnosis below needs the
	// response headers (X-OAuth-Scopes) and treats each status differently, so a
	// generic APIError would throw away the only signal that tells a
	// missing-scope token apart from a non-member one.
	resp, err := p.client.Do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("github: read response: %w", err)
	}

	// Only classic tokens advertise their scopes; fine-grained and app tokens
	// leave this header empty, so it can sharpen an error but never grant trust.
	scopes := resp.Header.Get("X-OAuth-Scopes")

	if resp.StatusCode == http.StatusOK {
		var membership struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal(body, &membership); err != nil {
			return fmt.Errorf("github: decode org membership: %w", err)
		}
		if membership.State != "active" {
			return fmt.Errorf("github: token owner's membership in org %s is %q, not active — /orgs/%s/members would return only public members", p.org, membership.State, p.org)
		}
		return nil
	}

	if scopes != "" && !hasOrgReadScope(scopes) {
		return fmt.Errorf("github: token scopes (%s) do not include read:org — /orgs/%s/members would silently return only the public members; grant admin:org (or at least read:org)", scopes, p.org)
	}
	return fmt.Errorf("github: cannot verify org membership for %s (status %d): %s — refusing to report a possibly public-only member list", p.org, resp.StatusCode, body)
}

// hasOrgReadScope reports whether a classic token's X-OAuth-Scopes list grants
// org read. GitHub lists granted scopes literally, without their implied
// children, so admin:org appears alone rather than expanded to read:org.
func hasOrgReadScope(scopes string) bool {
	for _, s := range strings.Split(scopes, ",") {
		switch strings.TrimSpace(s) {
		case "read:org", "write:org", "admin:org":
			return true
		}
	}
	return false
}

// fetchMembers lists org members, optionally filtered by role ("admin" or "member").
func (p *Provider) fetchMembers(ctx context.Context, role string) ([]orgMember, error) {
	var members []orgMember

	for page := 1; ; page++ {
		url := fmt.Sprintf("%s/orgs/%s/members?per_page=100&page=%d", p.baseURL, p.org, page)
		if role != "" {
			url += "&role=" + role
		}
		pageMembers, hasNext, err := p.fetchMemberPage(ctx, url)
		if err != nil {
			return nil, err
		}
		if len(pageMembers) == 0 {
			break
		}
		members = append(members, pageMembers...)
		if !hasNext {
			break
		}
	}

	return members, nil
}

func (p *Provider) fetchMemberPage(ctx context.Context, url string) (members []orgMember, hasNext bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	// Raw Do, not DoJSON: pagination is driven by the Link header, which only the
	// response object carries.
	resp, err := p.client.Do(ctx, req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("github: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, false, &httpclient.APIError{
			Provider:   "github",
			StatusCode: resp.StatusCode,
			Body:       string(body),
			URL:        req.URL.String(),
		}
	}

	if err := json.Unmarshal(body, &members); err != nil {
		return nil, false, fmt.Errorf("github: decode response: %w", err)
	}

	return members, hasNextPage(resp.Header.Get("Link")), nil
}

// fetchOrgActivity builds a best-effort map of login -> most recent activity from
// /orgs/{org}/events.
//
// That feed is PUBLIC events only and GitHub hard-caps it at 300 events over the
// last 30 days, hence 3 pages of 100. Anything private (private repos, most orgs'
// entire day-to-day) never appears, so a login missing from this map says nothing
// about whether the person works — see Capabilities, which declines to claim
// activity reporting for exactly this reason. Errors are swallowed on purpose:
// a missing timestamp degrades to "unknown", it does not invalidate the seat list.
func (p *Provider) fetchOrgActivity(ctx context.Context) map[string]time.Time {
	const maxEventPages = 3

	activity := make(map[string]time.Time)

	for page := 1; page <= maxEventPages; page++ {
		url := fmt.Sprintf("%s/orgs/%s/events?per_page=100&page=%d", p.baseURL, p.org, page)
		events, hasNext := p.fetchEventPage(ctx, url)
		if len(events) == 0 {
			break
		}
		for _, e := range events {
			key := strings.ToLower(e.Actor.Login)
			if _, exists := activity[key]; !exists {
				activity[key] = e.CreatedAt.UTC()
			}
		}
		if !hasNext {
			break
		}
	}

	return activity
}

type orgEvent struct {
	Actor struct {
		Login string `json:"login"`
	} `json:"actor"`
	CreatedAt time.Time `json:"created_at"`
}

func (p *Provider) fetchEventPage(ctx context.Context, url string) (events []orgEvent, hasNext bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	// Raw Do, not DoJSON: the event feed is paginated through the Link header.
	resp, err := p.client.Do(ctx, req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false
	}
	if json.Unmarshal(body, &events) != nil {
		return nil, false
	}

	return events, hasNextPage(resp.Header.Get("Link"))
}

// fetchUserEmail calls /users/{login} for the member's public email and name.
//
// resolved is false when GitHub exposes no email — the overwhelming majority of
// corporate accounts. The caller then keeps the bare login as Email; see
// ListUsers for why no address is invented.
func (p *Provider) fetchUserEmail(ctx context.Context, login string) (email, name string, resolved bool) {
	url := fmt.Sprintf("%s/users/%s", p.baseURL, login)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return login, "", false
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	var user struct {
		Email string `json:"email"`
		Name  string `json:"name"`
		Login string `json:"login"`
	}
	if p.client.DoJSON(ctx, "github", req, &user) != nil {
		return login, "", false
	}
	if user.Email == "" {
		return login, user.Name, false
	}
	return user.Email, user.Name, true
}

// fetchSAMLIdentities queries GitHub's GraphQL API for SAML SSO identity mappings.
// Returns a map of GitHub login -> SAML nameId (typically the corporate email).
// Returns an empty map if the org doesn't have SSO or the query fails.
func (p *Provider) fetchSAMLIdentities(ctx context.Context) map[string]string {
	result := make(map[string]string)
	cursor := ""

	for {
		afterClause := ""
		if cursor != "" {
			afterClause = fmt.Sprintf(`, after: "%s"`, cursor)
		}
		query := fmt.Sprintf(`{
			organization(login: "%s") {
				samlIdentityProvider {
					externalIdentities(first: 100%s) {
						pageInfo { hasNextPage endCursor }
						edges {
							node {
								samlIdentity { nameId }
								user { login }
							}
						}
					}
				}
			}
		}`, p.org, afterClause)

		body, err := json.Marshal(map[string]string{"query": query})
		if err != nil {
			return result
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.graphqlURL, bytes.NewReader(body))
		if err != nil {
			return result
		}
		req.Header.Set("Authorization", "Bearer "+p.token)
		req.Header.Set("Content-Type", "application/json")

		var gqlResp struct {
			Data struct {
				Organization struct {
					SAMLIdentityProvider *struct {
						ExternalIdentities struct {
							PageInfo struct {
								HasNextPage bool   `json:"hasNextPage"`
								EndCursor   string `json:"endCursor"`
							} `json:"pageInfo"`
							Edges []struct {
								Node struct {
									SAMLIdentity struct {
										NameID string `json:"nameId"`
									} `json:"samlIdentity"`
									User *struct {
										Login string `json:"login"`
									} `json:"user"`
								} `json:"node"`
							} `json:"edges"`
						} `json:"externalIdentities"`
					} `json:"samlIdentityProvider"`
				} `json:"organization"`
			} `json:"data"`
		}
		if p.client.DoJSON(ctx, "github", req, &gqlResp) != nil {
			return result
		}

		provider := gqlResp.Data.Organization.SAMLIdentityProvider
		if provider == nil {
			return result // org doesn't have SSO
		}

		for _, edge := range provider.ExternalIdentities.Edges {
			if edge.Node.User != nil && edge.Node.SAMLIdentity.NameID != "" {
				result[edge.Node.User.Login] = edge.Node.SAMLIdentity.NameID
			}
		}

		if !provider.ExternalIdentities.PageInfo.HasNextPage {
			break
		}
		cursor = provider.ExternalIdentities.PageInfo.EndCursor
	}

	return result
}

// hasNextPage checks if the Link header contains a rel="next" link.
func hasNextPage(link string) bool {
	if link == "" {
		return false
	}
	// Simple check: look for rel="next" in the Link header
	for _, part := range splitLinks(link) {
		if len(part) > 0 {
			for _, param := range splitLinks2(part) {
				if param == `rel="next"` {
					return true
				}
			}
		}
	}
	return false
}

func splitLinks(s string) []string {
	var parts []string
	current := ""
	for _, c := range s {
		if c == ',' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func splitLinks2(s string) []string {
	var parts []string
	current := ""
	for _, c := range s {
		if c == ';' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	// Trim spaces
	for i := range parts {
		trimmed := ""
		started := false
		end := len(parts[i])
		for j := len(parts[i]) - 1; j >= 0; j-- {
			if parts[i][j] != ' ' {
				end = j + 1
				break
			}
		}
		for j := 0; j < end; j++ {
			if parts[i][j] != ' ' {
				started = true
			}
			if started {
				trimmed += string(parts[i][j])
			}
		}
		parts[i] = trimmed
	}
	return parts
}

// Billing reads the organisation's plan so a seat report needs no config.
//
// GitHub states no price, but it does state how many seats were PURCHASED
// against how many are FILLED. That gap is the most expensive thing this
// connector can find and it is invisible from the member list: an org can
// carry dozens of paid-for, unoccupied seats for years because nobody looks at
// the plan and the member list alike.
func (p *Provider) Billing(ctx context.Context) (*core.Billing, error) {
	url := fmt.Sprintf("%s/orgs/%s", p.baseURL, p.org)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	var org struct {
		Plan *struct {
			Name        string `json:"name"`
			Seats       int    `json:"seats"`
			FilledSeats int    `json:"filled_seats"`
		} `json:"plan"`
	}
	if err := p.client.DoJSON(ctx, "github", req, &org); err != nil {
		return nil, err
	}
	if org.Plan == nil {
		// plan is only returned to org admins; a read:org member sees nothing.
		return nil, nil
	}

	// Seats is what is paid for. FilledSeats is what is used. Reporting the
	// purchased count is the point — using FilledSeats would hide the gap.
	// No price is inferred: GitHub plan names carry none, and an Enterprise
	// agreement is unknowable from here.
	return &core.Billing{
		Plan:        org.Plan.Name,
		BilledSeats: org.Plan.Seats,
		FilledSeats: org.Plan.FilledSeats,
	}, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("github: programmatic user invites not supported via API")
}

func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	users, err := p.ListUsers(ctx)
	if err != nil {
		return err
	}

	var login string
	for _, u := range users {
		if u.Email == email {
			login = u.Metadata["login"]
			break
		}
	}
	if login == "" {
		return fmt.Errorf("github: user %s not found", email)
	}

	url := fmt.Sprintf("%s/orgs/%s/members/%s", p.baseURL, p.org, login)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	return p.client.DoJSON(ctx, "github", req, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("github: role changes not supported via API")
}
