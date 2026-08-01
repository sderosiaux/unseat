package stripe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://access.stripe.com"

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

func (p *Provider) Name() string { return "stripe" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     true,
		CanRemove:  true,
		CanSuspend: false,
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
}

type scimListResponse struct {
	Resources    []scimUser `json:"Resources"`
	TotalResults int        `json:"totalResults"`
	ItemsPerPage int        `json:"itemsPerPage"`
	StartIndex   int        `json:"startIndex"`
}

type scimCreateRequest struct {
	Schemas  []string    `json:"schemas"`
	UserName string      `json:"userName"`
	Name     scimName    `json:"name"`
	Emails   []scimEmail `json:"emails"`
	Active   bool        `json:"active"`
}

func (p *Provider) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+p.token)
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	all, err := httpclient.ListSCIM[scimUser](ctx, p.client, httpclient.SCIMPageOptions{
		Provider: "stripe",
		URL:      p.baseURL + "/scim/v2/Users",
		PageSize: 100,
		Decorate: p.authorize,
	})
	if err != nil {
		return nil, err
	}

	users := make([]core.User, 0, len(all))
	for _, u := range all {
		email := ""
		if len(u.Emails) > 0 {
			email = u.Emails[0].Value
		}
		status := "active"
		if !u.Active {
			status = "suspended"
		}
		users = append(users, core.User{
			Email:       email,
			DisplayName: u.DisplayName,
			Role:        "member",
			Status:      status,
			ProviderID:  u.ID,
		})
	}
	return users, nil
}

func (p *Provider) AddUser(ctx context.Context, email string, _ string) error {
	givenName, familyName := splitName(email)

	payload := scimCreateRequest{
		Schemas:  []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
		UserName: email,
		Name:     scimName{GivenName: givenName, FamilyName: familyName},
		Emails:   []scimEmail{{Value: email, Primary: true}},
		Active:   true,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("stripe: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/scim/v2/Users", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	p.authorize(req)
	req.Header.Set("Content-Type", "application/scim+json")

	return p.client.DoJSON(ctx, "stripe", req, nil)
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
		return fmt.Errorf("stripe: user %s not found", email)
	}

	url := fmt.Sprintf("%s/scim/v2/Users/%s", p.baseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	p.authorize(req)

	return p.client.DoJSON(ctx, "stripe", req, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("stripe: role changes not supported (managed via SAML)")
}

// splitName splits a display name into given/family on the first space.
// Falls back to the email local part as given name if empty.
func splitName(email string) (string, string) {
	local := email
	if idx := strings.Index(email, "@"); idx > 0 {
		local = email[:idx]
	}
	return local, ""
}
