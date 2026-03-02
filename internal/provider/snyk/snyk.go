package snyk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.snyk.io"

type Provider struct {
	token   string
	orgID   string
	baseURL string
	client  *http.Client
}

func New(token, orgID string) *Provider {
	return &Provider{
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

func (p *Provider) Name() string { return "snyk" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     false,
		CanRemove:  true,
		CanSuspend: false,
		CanSetRole: false,
	}
}

type snykMember struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	url := fmt.Sprintf("%s/v1/org/%s/members", p.baseURL, p.orgID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+p.token)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("snyk: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("snyk: API error (status %d): %s", resp.StatusCode, body)
	}

	var members []snykMember
	if err := json.Unmarshal(body, &members); err != nil {
		return nil, fmt.Errorf("snyk: decode response: %w", err)
	}

	users := make([]core.User, 0, len(members))
	for _, m := range members {
		users = append(users, core.User{
			Email:       m.Email,
			DisplayName: m.Username,
			Role:        m.Role,
			Status:      "active",
			ProviderID:  m.ID,
		})
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("snyk: programmatic user invites not supported")
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
		return fmt.Errorf("snyk: user %s not found", email)
	}

	url := fmt.Sprintf("%s/v1/org/%s/members/update/%s", p.baseURL, p.orgID, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("snyk: remove user failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("snyk: role changes not supported")
}
