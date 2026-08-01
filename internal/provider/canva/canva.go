package canva

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://www.canva.com"

// scimPageSize is the maximum page Canva's SCIM implementation serves.
const scimPageSize = 10

type Provider struct {
	token   string
	baseURL string
	client  *httpclient.Client
}

func New(token string) *Provider {
	return &Provider{token: token, baseURL: defaultBaseURL, client: httpclient.New()}
}

func (p *Provider) WithBaseURL(u string) *Provider {
	p.baseURL = u
	return p
}

func (p *Provider) Name() string { return "canva" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type scimName struct {
	GivenName  string `json:"givenName"`
	FamilyName string `json:"familyName"`
}

type scimEmail struct {
	Value   string `json:"value"`
	Type    string `json:"type"`
	Primary bool   `json:"primary"`
}

type scimUser struct {
	ID       string      `json:"id"`
	UserName string      `json:"userName"`
	Name     scimName    `json:"name"`
	Emails   []scimEmail `json:"emails"`
	Active   bool        `json:"active"`
	Role     string      `json:"role,omitempty"`
}

func (u scimUser) toCore() core.User {
	email := u.UserName
	if len(u.Emails) > 0 {
		email = u.Emails[0].Value
	}
	displayName := u.Name.GivenName
	if u.Name.FamilyName != "" {
		if displayName != "" {
			displayName += " "
		}
		displayName += u.Name.FamilyName
	}
	if displayName == "" {
		displayName = email
	}

	status := "active"
	if !u.Active {
		status = "suspended"
	}

	role := u.Role
	if role == "" {
		role = "member"
	}

	return core.User{
		Email:       email,
		DisplayName: displayName,
		Role:        role,
		Status:      status,
		ProviderID:  u.ID,
	}
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	resources, err := httpclient.ListSCIM[scimUser](ctx, p.client, httpclient.SCIMPageOptions{
		Provider: "canva",
		URL:      p.baseURL + "/_scim/v2/Users",
		PageSize: scimPageSize,
		Decorate: func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer "+p.token)
		},
	})
	if err != nil {
		return nil, err
	}

	all := make([]core.User, 0, len(resources))
	for _, u := range resources {
		all = append(all, u.toCore())
	}
	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("canva: add user not supported")
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
		return fmt.Errorf("canva: user %s not found", email)
	}

	endpoint := fmt.Sprintf("%s/_scim/v2/Users/%s", p.baseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)

	return p.client.DoJSON(ctx, "canva", req, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("canva: set role not supported")
}
