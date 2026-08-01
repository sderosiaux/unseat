package pagerduty

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://api.pagerduty.com"

type Provider struct {
	apiKey  string
	baseURL string
	client  *httpclient.Client
}

func New(apiKey string) *Provider {
	return &Provider{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		client:  httpclient.New(),
	}
}

func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "pagerduty" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type pagerdutyUser struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

type listUsersResponse struct {
	Users  []pagerdutyUser `json:"users"`
	More   bool            `json:"more"`
	Offset int             `json:"offset"`
	Limit  int             `json:"limit"`
	Total  int             `json:"total"`
}

func (p *Provider) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Token token="+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	offset := 0
	limit := 100

	for {
		endpoint := fmt.Sprintf("%s/users?limit=%d&offset=%s",
			p.baseURL, limit, strconv.Itoa(offset))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		p.authorize(req)

		var result listUsersResponse
		if err := p.client.DoJSON(ctx, "pagerduty", req, &result); err != nil {
			return nil, err
		}

		for _, u := range result.Users {
			all = append(all, core.User{
				Email:       u.Email,
				DisplayName: u.Name,
				Role:        u.Role,
				Status:      "active",
				ProviderID:  u.ID,
			})
		}

		if !result.More {
			break
		}
		offset += limit
	}

	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("pagerduty: add user not supported via API")
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
		return fmt.Errorf("pagerduty: user %s not found", email)
	}

	endpoint := fmt.Sprintf("%s/users/%s", p.baseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	p.authorize(req)

	return p.client.DoJSON(ctx, "pagerduty", req, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("pagerduty: set role not supported via API")
}
