package grafana

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://grafana.com"

type Provider struct {
	token   string
	baseURL string
	client  *http.Client
}

func New(token string) *Provider {
	return &Provider{
		token:   token,
		baseURL: defaultBaseURL,
		client:  &http.Client{},
	}
}

func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "grafana" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type grafanaOrgUser struct {
	OrgID         int    `json:"orgId"`
	UserID        int    `json:"userId"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	Login         string `json:"login"`
	Role          string `json:"role"`
	AvatarURL     string `json:"avatarUrl"`
	LastSeenAt    string `json:"lastSeenAt"`
	LastSeenAtAge string `json:"lastSeenAtAge"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	endpoint := fmt.Sprintf("%s/api/org/users", p.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("grafana: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("grafana: API error (status %d): %s", resp.StatusCode, body)
	}

	var members []grafanaOrgUser
	if err := json.Unmarshal(body, &members); err != nil {
		return nil, fmt.Errorf("grafana: decode response: %w", err)
	}

	users := make([]core.User, 0, len(members))
	for _, m := range members {
		displayName := m.Name
		if displayName == "" {
			displayName = m.Login
		}
		users = append(users, core.User{
			Email:       m.Email,
			DisplayName: displayName,
			Role:        m.Role,
			Status:      "active",
			ProviderID:  fmt.Sprintf("%d", m.UserID),
		})
	}

	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("grafana: add user not supported via API")
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
		return fmt.Errorf("grafana: user %s not found", email)
	}

	endpoint := fmt.Sprintf("%s/api/org/users/%s", p.baseURL, userID)
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

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("grafana: remove user failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("grafana: set role not supported via API")
}
