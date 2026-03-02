package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.loom.com"

type Provider struct {
	token   string
	spaceID string
	baseURL string
	client  *http.Client
}

func New(token, spaceID string) *Provider {
	return &Provider{token: token, spaceID: spaceID, baseURL: defaultBaseURL, client: &http.Client{}}
}

func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "loom" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     false,
		CanRemove:  true,
		CanSuspend: false,
		CanSetRole: false,
	}
}

type loomMember struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Status      string `json:"status"`
}

type loomListResponse struct {
	Members       []loomMember `json:"members"`
	NextPageToken string       `json:"next_page_token"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []loomMember
	nextPageToken := ""

	for {
		url := fmt.Sprintf("%s/v1/spaces/%s/members", p.baseURL, p.spaceID)
		if nextPageToken != "" {
			url += "?page_token=" + nextPageToken
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
			return nil, fmt.Errorf("loom: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("loom: API error (status %d): %s", resp.StatusCode, body)
		}

		var result loomListResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("loom: decode response: %w", err)
		}

		all = append(all, result.Members...)

		if result.NextPageToken == "" {
			break
		}
		nextPageToken = result.NextPageToken
	}

	users := make([]core.User, 0, len(all))
	for _, m := range all {
		status := m.Status
		if status == "" {
			status = "active"
		}
		role := m.Role
		if role == "" {
			role = "member"
		}
		users = append(users, core.User{
			Email:       m.Email,
			DisplayName: m.DisplayName,
			Role:        role,
			Status:      status,
			ProviderID:  m.ID,
		})
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("loom: programmatic user invites not supported")
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
		return fmt.Errorf("loom: user %s not found", email)
	}

	url := fmt.Sprintf("%s/v1/spaces/%s/members/%s", p.baseURL, p.spaceID, memberID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("loom: remove member failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("loom: role changes not supported")
}
