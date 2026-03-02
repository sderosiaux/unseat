package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/saas-watcher/internal/core"
)

const defaultBaseURL = "https://api.anthropic.com"

type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func New(apiKey string) *Provider {
	return &Provider{apiKey: apiKey, baseURL: defaultBaseURL, client: &http.Client{}}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "anthropic" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type apiUser struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	AddedAt string `json:"added_at"`
	Type    string `json:"type"`
}

type listUsersResponse struct {
	Data    []apiUser `json:"data"`
	HasMore bool      `json:"has_more"`
	FirstID string    `json:"first_id"`
	LastID  string    `json:"last_id"`
}

type apiError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (p *Provider) doRequest(ctx context.Context, method, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var apiErr apiError
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Message != "" {
			return nil, fmt.Errorf("anthropic: API error: %s", apiErr.Message)
		}
		return nil, fmt.Errorf("anthropic: HTTP %d", resp.StatusCode)
	}

	return body, nil
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	var afterID string

	for {
		path := "/v1/organizations/users?limit=100"
		if afterID != "" {
			path += "&after_id=" + afterID
		}

		body, err := p.doRequest(ctx, http.MethodGet, path)
		if err != nil {
			return nil, err
		}

		var resp listUsersResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("anthropic: decode response: %w", err)
		}

		for _, u := range resp.Data {
			all = append(all, core.User{
				Email:       u.Email,
				DisplayName: u.Name,
				Role:        u.Role,
				Status:      "active",
				ProviderID:  u.ID,
			})
		}

		if !resp.HasMore {
			break
		}
		afterID = resp.LastID
	}

	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("anthropic: programmatic user invites not supported, use workspace invites")
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
		return fmt.Errorf("anthropic: user %s not found", email)
	}

	_, err = p.doRequest(ctx, http.MethodDelete, "/v1/organizations/users/"+userID)
	return err
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("anthropic: role changes not supported via Admin API")
}
