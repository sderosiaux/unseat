package trello

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.trello.com"

type Provider struct {
	apiKey  string
	token   string
	orgID   string
	baseURL string
	client  *http.Client
}

func New(apiKey, token, orgID string) *Provider {
	return &Provider{
		apiKey:  apiKey,
		token:   token,
		orgID:   orgID,
		baseURL: defaultBaseURL,
		client:  &http.Client{},
	}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "trello" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type trelloMember struct {
	ID       string `json:"id"`
	FullName string `json:"fullName"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

func (p *Provider) authParams() string {
	return fmt.Sprintf("key=%s&token=%s", p.apiKey, p.token)
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	endpoint := fmt.Sprintf("%s/1/organizations/%s/members?fields=fullName,username,email&%s",
		p.baseURL, p.orgID, p.authParams())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("trello: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trello: API error (status %d): %s", resp.StatusCode, body)
	}

	var members []trelloMember
	if err := json.Unmarshal(body, &members); err != nil {
		return nil, fmt.Errorf("trello: decode response: %w", err)
	}

	users := make([]core.User, 0, len(members))
	for _, m := range members {
		// Trello only returns email if member has it set as visible.
		// Fall back to username as identifier.
		email := m.Email
		if email == "" {
			email = m.Username
		}
		users = append(users, core.User{
			Email:       email,
			DisplayName: m.FullName,
			Role:        "member",
			Status:      "active",
			ProviderID:  m.ID,
			Metadata:    map[string]string{"username": m.Username},
		})
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("trello: programmatic user invites not supported via API")
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
		return fmt.Errorf("trello: user %s not found", email)
	}

	endpoint := fmt.Sprintf("%s/1/organizations/%s/members/%s?%s",
		p.baseURL, p.orgID, memberID, p.authParams())
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("trello: remove member failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("trello: role changes not supported via API")
}
