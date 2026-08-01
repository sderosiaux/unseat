package trello

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://api.trello.com"

type Provider struct {
	apiKey  string
	token   string
	orgID   string
	baseURL string
	client  *httpclient.Client
}

func New(apiKey, token, orgID string) *Provider {
	return &Provider{
		apiKey:  apiKey,
		token:   token,
		orgID:   orgID,
		baseURL: defaultBaseURL,
		client:  httpclient.New(),
	}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "trello" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type trelloMember struct {
	ID       string `json:"id"`
	FullName string `json:"fullName"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

func (p *Provider) authParams() string {
	return fmt.Sprintf("key=%s&token=%s", p.apiKey, p.token)
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	endpoint := fmt.Sprintf("%s/1/organizations/%s/members?fields=fullName,username,email&%s",
		p.baseURL, p.orgID, p.authParams())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	var members []trelloMember
	if err := p.client.DoJSON(ctx, "trello", req, &members); err != nil {
		return nil, err
	}

	users := make([]core.User, 0, len(members))
	for _, m := range members {
		// Trello only returns email if member has it set as visible.
		// Fall back to username as identifier.
		email := m.Email
		if email == "" {
			email = m.Username
		}
		users = append(users, core.User{
			Email:       email,
			DisplayName: m.FullName,
			Role:        "member",
			Status:      "active",
			ProviderID:  m.ID,
			Metadata:    map[string]string{"username": m.Username},
		})
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("trello: programmatic user invites not supported via API")
}

func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	users, err := p.ListUsers(ctx)
	if err != nil {
		return err
	}

	var memberID string
	for _, u := range users {
		if u.Email == email {
			memberID = u.ProviderID
			break
		}
	}
	if memberID == "" {
		return fmt.Errorf("trello: user %s not found", email)
	}

	endpoint := fmt.Sprintf("%s/1/organizations/%s/members/%s?%s",
		p.baseURL, p.orgID, memberID, p.authParams())
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}

	return p.client.DoJSON(ctx, "trello", req, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("trello: role changes not supported via API")
}
