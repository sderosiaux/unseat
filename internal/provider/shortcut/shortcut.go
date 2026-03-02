package shortcut

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.app.shortcut.com"

type Provider struct {
	token   string
	baseURL string
	client  *http.Client
}

func New(token string) *Provider {
	return &Provider{token: token, baseURL: defaultBaseURL, client: &http.Client{}}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "shortcut" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type apiMember struct {
	ID       string      `json:"id"`
	Profile  apiProfile  `json:"profile"`
	Role     string      `json:"role"`
	Disabled bool        `json:"disabled"`
	State    string      `json:"state"`
}

type apiProfile struct {
	Name              string `json:"name"`
	EmailAddress      string `json:"email_address"`
	MentionName       string `json:"mention_name"`
	Deactivated       bool   `json:"deactivated"`
	DisplayIcon       string `json:"display_icon"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	url := fmt.Sprintf("%s/api/v3/members", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Shortcut-Token", p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("shortcut: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("shortcut: API error (status %d): %s", resp.StatusCode, body)
	}

	var members []apiMember
	if err := json.Unmarshal(body, &members); err != nil {
		return nil, fmt.Errorf("shortcut: decode response: %w", err)
	}

	users := make([]core.User, 0, len(members))
	for _, m := range members {
		status := "active"
		if m.Disabled || m.Profile.Deactivated {
			status = "suspended"
		}
		users = append(users, core.User{
			Email:       m.Profile.EmailAddress,
			DisplayName: m.Profile.Name,
			Role:        m.Role,
			Status:      status,
			ProviderID:  m.ID,
		})
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("shortcut: programmatic user invites not supported via API")
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
		return fmt.Errorf("shortcut: user %s not found", email)
	}

	url := fmt.Sprintf("%s/api/v3/members/%s", p.baseURL, memberID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Shortcut-Token", p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("shortcut: remove member failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("shortcut: role changes not supported via API")
}
