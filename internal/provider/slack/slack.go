package slack

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://api.slack.com"

const pageSize = 100

type Provider struct {
	token   string
	baseURL string
	client  *httpclient.Client
}

func New(token string) *Provider {
	return &Provider{token: token, baseURL: defaultBaseURL, client: httpclient.New()}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "slack" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     false,
		CanRemove:  true,
		CanSuspend: true,
		CanSetRole: false,
	}
}

type scimEmail struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary"`
}

type scimName struct {
	GivenName  string `json:"givenName"`
	FamilyName string `json:"familyName"`
}

type scimUser struct {
	ID          string      `json:"id"`
	UserName    string      `json:"userName"`
	DisplayName string      `json:"displayName"`
	Name        scimName    `json:"name"`
	Emails      []scimEmail `json:"emails"`
	Active      bool        `json:"active"`
	Title       string      `json:"title"`
}

type scimListResponse struct {
	Resources    []scimUser `json:"Resources"`
	TotalResults int        `json:"totalResults"`
	ItemsPerPage int        `json:"itemsPerPage"`
	StartIndex   int        `json:"startIndex"`
}

func (u scimUser) email() string {
	if len(u.Emails) > 0 {
		return u.Emails[0].Value
	}
	return ""
}

// name is empty for users provisioned with only userName/name, so degrade
// through the fields Slack does populate rather than surfacing a blank label.
func (u scimUser) name() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	if full := strings.TrimSpace(u.Name.GivenName + " " + u.Name.FamilyName); full != "" {
		return full
	}
	return u.UserName
}

func (u scimUser) toCore() core.User {
	status := "active"
	if !u.Active {
		status = "suspended"
	}
	return core.User{
		Email:       u.email(),
		DisplayName: u.name(),
		Role:        "member",
		Status:      status,
		ProviderID:  u.ID,
	}
}

func (p *Provider) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+p.token)
}

// planGate annotates auth-shaped failures. SCIM is gated behind the Business+
// and Enterprise Grid plans, so a 401/403/404 is far more often a plan/scope
// problem than a malformed token.
func planGate(err error) error {
	if err == nil {
		return nil
	}
	if httpclient.IsUnauthorized(err) || httpclient.IsNotFound(err) {
		return fmt.Errorf("%w — the SCIM API requires a Business+ or Enterprise Grid plan and an admin-scoped token (admin scope, org/workspace owner)", err)
	}
	return err
}

func (p *Provider) getUsers(ctx context.Context, query url.Values) (*scimListResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/scim/v2/Users?%s", p.baseURL, query.Encode()), nil)
	if err != nil {
		return nil, err
	}
	p.authorize(req)

	var result scimListResponse
	if err := p.client.DoJSON(ctx, "slack", req, &result); err != nil {
		return nil, planGate(err)
	}
	return &result, nil
}

func (p *Provider) listAll(ctx context.Context) ([]scimUser, error) {
	all, err := httpclient.ListSCIM[scimUser](ctx, p.client, httpclient.SCIMPageOptions{
		Provider: "slack",
		URL:      p.baseURL + "/scim/v2/Users",
		PageSize: pageSize,
		Decorate: p.authorize,
	})
	if err != nil {
		return nil, planGate(err)
	}
	return all, nil
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	all, err := p.listAll(ctx)
	if err != nil {
		return nil, err
	}
	users := make([]core.User, 0, len(all))
	for _, u := range all {
		users = append(users, u.toCore())
	}
	return users, nil
}

// resolveUserID trades a full org crawl for a single filtered lookup — a bulk
// offboard would otherwise re-list every member once per removal.
func (p *Provider) resolveUserID(ctx context.Context, email string) (string, error) {
	q := url.Values{}
	q.Set("filter", fmt.Sprintf("email eq %q", email))
	q.Set("count", "1")

	if result, err := p.getUsers(ctx, q); err == nil {
		for _, u := range result.Resources {
			if u.email() == email {
				return u.ID, nil
			}
		}
	}

	// Filtering is rejected or unmatched on some tenants; only then pay for the crawl.
	all, err := p.listAll(ctx)
	if err != nil {
		return "", err
	}
	for _, u := range all {
		if u.email() == email {
			return u.ID, nil
		}
	}
	return "", fmt.Errorf("slack: user %s not found", email)
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("slack: programmatic user invites not supported via SCIM API")
}

func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	userID, err := p.resolveUserID(ctx, email)
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("%s/scim/v2/Users/%s", p.baseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	p.authorize(req)

	return p.client.DoJSON(ctx, "slack", req, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("slack: role changes not supported via SCIM API")
}
