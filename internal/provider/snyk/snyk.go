package snyk

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://api.snyk.io"

type Provider struct {
	token   string
	orgID   string
	baseURL string
	client  *httpclient.Client
}

func New(token, orgID string) *Provider {
	return &Provider{
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

func (p *Provider) Name() string { return "snyk" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     false,
		CanRemove:  true,
		CanSuspend: false,
		CanSetRole: false,
	}
}

type snykMember struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	url := fmt.Sprintf("%s/v1/org/%s/members", p.baseURL, p.orgID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+p.token)
	req.Header.Set("Accept", "application/json")

	var members []snykMember
	if err := p.client.DoJSON(ctx, "snyk", req, &members); err != nil {
		return nil, err
	}

	users := make([]core.User, 0, len(members))
	for _, m := range members {
		users = append(users, core.User{
			Email:       m.Email,
			DisplayName: m.Username,
			Role:        m.Role,
			Status:      "active",
			ProviderID:  m.ID,
		})
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("snyk: programmatic user invites not supported")
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
		return fmt.Errorf("snyk: user %s not found", email)
	}

	url := fmt.Sprintf("%s/v1/org/%s/members/update/%s", p.baseURL, p.orgID, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+p.token)

	return p.client.DoJSON(ctx, "snyk", req, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("snyk: role changes not supported")
}
