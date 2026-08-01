package notion

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://api.notion.com"

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

func (p *Provider) Name() string { return "notion" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type scimEmail struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary"`
}

type scimName struct {
	Formatted  string `json:"formatted"`
	GivenName  string `json:"givenName"`
	FamilyName string `json:"familyName"`
}

// scimNotionExtension carries the workspace role, which Notion exposes through
// its own schema extension rather than the SCIM core attributes.
type scimNotionExtension struct {
	Role string `json:"role"`
}

type scimUser struct {
	ID              string               `json:"id"`
	UserName        string               `json:"userName"`
	DisplayName     string               `json:"displayName"`
	Name            scimName             `json:"name"`
	Emails          []scimEmail          `json:"emails"`
	Active          bool                 `json:"active"`
	Title           string               `json:"title,omitempty"`
	NotionExtension *scimNotionExtension `json:"urn:ietf:params:scim:schemas:extension:notion:2.0:User,omitempty"`
}

// displayName follows Notion's documented preference order; name.formatted is
// the field they recommend IdPs populate, but many only send the parts.
func (u scimUser) displayName() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	if u.Name.Formatted != "" {
		return u.Name.Formatted
	}
	if joined := strings.TrimSpace(u.Name.GivenName + " " + u.Name.FamilyName); joined != "" {
		return joined
	}
	return u.UserName
}

// role reports the Notion workspace role ("owner", "membership_admin",
// "member", "restricted_member"), defaulting to member when the workspace does
// not return the extension.
func (u scimUser) role() string {
	if u.NotionExtension != nil && u.NotionExtension.Role != "" {
		return u.NotionExtension.Role
	}
	return "member"
}

func (p *Provider) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+p.token)
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	all, err := httpclient.ListSCIM[scimUser](ctx, p.client, httpclient.SCIMPageOptions{
		Provider: "notion",
		URL:      p.baseURL + "/scim/v2/Users",
		Decorate: p.authorize,
	})
	if err != nil {
		return nil, err
	}

	users := make([]core.User, 0, len(all))
	for _, u := range all {
		email := u.UserName
		for _, e := range u.Emails {
			if e.Primary {
				email = e.Value
				break
			}
		}
		status := "active"
		if !u.Active {
			status = "suspended"
		}
		users = append(users, core.User{
			Email:       email,
			DisplayName: u.displayName(),
			Role:        u.role(),
			Status:      status,
			ProviderID:  u.ID,
		})
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("notion: programmatic user invites not supported via SCIM API")
}

func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	users, err := p.ListUsers(ctx)
	if err != nil {
		return err
	}

	var userID string
	for _, u := range users {
		if u.Email == email {
			userID = u.ProviderID
			break
		}
	}
	if userID == "" {
		return fmt.Errorf("notion: user %s not found", email)
	}

	url := fmt.Sprintf("%s/scim/v2/Users/%s", p.baseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	p.authorize(req)

	return p.client.DoJSON(ctx, "notion", req, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("notion: role changes not supported via SCIM API")
}
