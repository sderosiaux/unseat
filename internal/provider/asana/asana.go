package asana

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://app.asana.com"

type Provider struct {
	token       string
	workspaceID string
	baseURL     string
	client      *http.Client
}

func New(token, workspaceID string) *Provider {
	return &Provider{
		token:       token,
		workspaceID: workspaceID,
		baseURL:     defaultBaseURL,
		client:      &http.Client{},
	}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "asana" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type asanaUser struct {
	GID          string `json:"gid"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	ResourceType string `json:"resource_type"`
}

type listUsersResponse struct {
	Data     []asanaUser `json:"data"`
	NextPage *nextPage   `json:"next_page"`
}

type nextPage struct {
	Offset string `json:"offset"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	offset := ""

	for {
		endpoint := fmt.Sprintf("%s/api/1.0/workspaces/%s/users?opt_fields=email,name,resource_type&limit=100",
			p.baseURL, p.workspaceID)
		if offset != "" {
			endpoint += "&offset=" + offset
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+p.token)

		resp, err := p.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("asana: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("asana: API error (status %d): %s", resp.StatusCode, body)
		}

		var result listUsersResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("asana: decode response: %w", err)
		}

		for _, u := range result.Data {
			all = append(all, core.User{
				Email:       u.Email,
				DisplayName: u.Name,
				Role:        "member",
				Status:      "active",
				ProviderID:  u.GID,
			})
		}

		if result.NextPage == nil || result.NextPage.Offset == "" {
			break
		}
		offset = result.NextPage.Offset
	}

	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("asana: programmatic user invites not supported via API")
}

func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	users, err := p.ListUsers(ctx)
	if err != nil {
		return err
	}

	var userGID string
	for _, u := range users {
		if u.Email == email {
			userGID = u.ProviderID
			break
		}
	}
	if userGID == "" {
		return fmt.Errorf("asana: user %s not found", email)
	}

	endpoint := fmt.Sprintf("%s/api/1.0/workspaces/%s/removeUser", p.baseURL, p.workspaceID)
	payload := fmt.Sprintf(`{"data":{"user":"%s"}}`, userGID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("asana: remove user failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("asana: role changes not supported via API")
}
