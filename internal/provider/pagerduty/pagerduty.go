package pagerduty

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.pagerduty.com"

type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func New(apiKey string) *Provider {
	return &Provider{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		client:  &http.Client{},
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
		req.Header.Set("Authorization", "Token token="+p.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := p.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("pagerduty: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("pagerduty: API error (status %d): %s", resp.StatusCode, body)
		}

		var result listUsersResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("pagerduty: decode response: %w", err)
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
	req.Header.Set("Authorization", "Token token="+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pagerduty: remove user failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("pagerduty: set role not supported via API")
}
