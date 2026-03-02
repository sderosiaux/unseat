package miro

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.miro.com"

type Provider struct {
	token   string
	orgID   string
	baseURL string
	client  *http.Client
}

func New(token, orgID string) *Provider {
	return &Provider{token: token, orgID: orgID, baseURL: defaultBaseURL, client: &http.Client{}}
}

func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "miro" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type member struct {
	ID             string `json:"id"`
	Email          string `json:"email"`
	Role           string `json:"role"`
	Active         bool   `json:"active"`
	License        string `json:"license"`
	LastActivityAt string `json:"lastActivityAt,omitempty"`
}

type membersResponse struct {
	Data   []member `json:"data"`
	Cursor string   `json:"cursor"`
	Limit  int      `json:"limit"`
	Size   int      `json:"size"`
	Total  int      `json:"total"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	cursor := ""

	for {
		endpoint := fmt.Sprintf("%s/v2/orgs/%s/members?active=true", p.baseURL, p.orgID)
		if cursor != "" {
			endpoint += "&cursor=" + cursor
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
			return nil, fmt.Errorf("miro: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("miro: API returned %d: %s", resp.StatusCode, body)
		}

		var result membersResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("miro: decode response: %w", err)
		}

		for _, m := range result.Data {
			status := "active"
			if !m.Active {
				status = "suspended"
			}
			user := core.User{
				Email:       m.Email,
				DisplayName: m.Email,
				Role:        m.Role,
				Status:      status,
				ProviderID:  m.ID,
			}
			if m.LastActivityAt != "" {
				if t, err := time.Parse(time.RFC3339, m.LastActivityAt); err == nil {
					user.LastActivityAt = &t
				}
			}
			all = append(all, user)
		}

		if result.Cursor == "" {
			break
		}
		cursor = result.Cursor
	}

	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("miro: programmatic user invites not supported via API")
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
		return fmt.Errorf("miro: user %s not found", email)
	}

	endpoint := fmt.Sprintf("%s/v2/orgs/%s/members/%s", p.baseURL, p.orgID, memberID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("miro: delete member returned %d: %s", resp.StatusCode, body)
	}

	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("miro: role changes not supported via API")
}
