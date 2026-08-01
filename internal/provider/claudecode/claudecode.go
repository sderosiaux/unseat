package claudecode

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://api.anthropic.com"

type Provider struct {
	apiKey  string
	baseURL string
	client  *httpclient.Client
}

func New(apiKey string) *Provider {
	return &Provider{apiKey: apiKey, baseURL: defaultBaseURL, client: httpclient.New()}
}

// WithBaseURL overrides the API endpoint (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "claude-code" }

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

type apiListResponse struct {
	Data    []apiUser `json:"data"`
	HasMore bool      `json:"has_more"`
	FirstID string    `json:"first_id,omitempty"`
	LastID  string    `json:"last_id,omitempty"`
}

func (p *Provider) setAuthHeaders(req *http.Request) {
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
}

// fetchAllUsers retrieves all organization users via cursor pagination.
func (p *Provider) fetchAllUsers(ctx context.Context) ([]apiUser, error) {
	var all []apiUser
	var cursor string

	for {
		url := p.baseURL + "/v1/organizations/users?limit=100"
		if cursor != "" {
			url += "&after_id=" + cursor
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		p.setAuthHeaders(req)

		var listResp apiListResponse
		if err := p.client.DoJSON(ctx, "claude-code", req, &listResp); err != nil {
			return nil, err
		}

		all = append(all, listResp.Data...)

		if !listResp.HasMore {
			break
		}
		cursor = listResp.LastID
	}

	return all, nil
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	apiUsers, err := p.fetchAllUsers(ctx)
	if err != nil {
		return nil, err
	}

	var users []core.User
	for _, u := range apiUsers {
		if u.Role != "claude_code_user" {
			continue
		}
		users = append(users, core.User{
			Email:       u.Email,
			DisplayName: u.Name,
			Role:        "claude_code_user",
			Status:      "active",
			ProviderID:  u.ID,
		})
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("claude-code: adding users not supported")
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
		return fmt.Errorf("claude-code: user %s not found among claude_code_user accounts", email)
	}

	url := fmt.Sprintf("%s/v1/organizations/users/%s", p.baseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	p.setAuthHeaders(req)

	return p.client.DoJSON(ctx, "claude-code", req, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("claude-code: role changes not supported")
}
