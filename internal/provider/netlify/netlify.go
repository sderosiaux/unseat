package netlify

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://api.netlify.com"

type Provider struct {
	token       string
	accountSlug string
	baseURL     string
	client      *httpclient.Client
}

func New(token, accountSlug string) *Provider {
	return &Provider{
		token:       token,
		accountSlug: accountSlug,
		baseURL:     defaultBaseURL,
		client:      httpclient.New(),
	}
}

func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "netlify" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type netlifyMember struct {
	ID       string `json:"id"`
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	Avatar   string `json:"avatar"`
	Role     string `json:"role"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	page := 1

	for {
		endpoint := fmt.Sprintf("%s/api/v1/%s/members?page=%s&per_page=100",
			p.baseURL, p.accountSlug, strconv.Itoa(page))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+p.token)

		var members []netlifyMember
		if err := p.client.DoJSON(ctx, "netlify", req, &members); err != nil {
			return nil, err
		}

		for _, m := range members {
			all = append(all, core.User{
				Email:       m.Email,
				DisplayName: m.FullName,
				Role:        m.Role,
				Status:      "active",
				ProviderID:  m.ID,
			})
		}

		if len(members) < 100 {
			break
		}
		page++
	}

	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("netlify: add user not supported via API")
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
		return fmt.Errorf("netlify: user %s not found", email)
	}

	endpoint := fmt.Sprintf("%s/api/v1/%s/members/%s", p.baseURL, p.accountSlug, memberID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)

	return p.client.DoJSON(ctx, "netlify", req, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("netlify: set role not supported via API")
}
