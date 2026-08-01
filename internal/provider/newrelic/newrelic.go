package newrelic

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://api.newrelic.com"

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

func (p *Provider) Name() string { return "newrelic" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type newrelicUser struct {
	ID    int    `json:"id"`
	Email string `json:"email"`
	Name  string `json:"first_name"`
	Last  string `json:"last_name"`
	Role  string `json:"role"`
}

type listUsersResponse struct {
	Users []newrelicUser `json:"users"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	page := 1

	for {
		endpoint := fmt.Sprintf("%s/v2/users.json?page=%s", p.baseURL, strconv.Itoa(page))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Api-Key", p.apiKey)

		var result listUsersResponse
		if err := p.client.DoJSON(ctx, "newrelic", req, &result); err != nil {
			return nil, err
		}

		for _, u := range result.Users {
			displayName := u.Name
			if u.Last != "" {
				displayName = u.Name + " " + u.Last
			}
			all = append(all, core.User{
				Email:       u.Email,
				DisplayName: displayName,
				Role:        u.Role,
				Status:      "active",
				ProviderID:  strconv.Itoa(u.ID),
			})
		}

		if len(result.Users) == 0 {
			break
		}
		page++
	}

	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("newrelic: add user not supported via API")
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
		return fmt.Errorf("newrelic: user %s not found", email)
	}

	endpoint := fmt.Sprintf("%s/v2/users/%s.json", p.baseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Api-Key", p.apiKey)

	return p.client.DoJSON(ctx, "newrelic", req, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("newrelic: set role not supported via API")
}
