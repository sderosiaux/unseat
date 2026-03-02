package intercom

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.intercom.io"

type Provider struct {
	token   string
	baseURL string
	client  *http.Client
}

func New(token string) *Provider {
	return &Provider{token: token, baseURL: defaultBaseURL, client: &http.Client{}}
}

func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "intercom" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true, // set away
	}
}

type intercomAdmin struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	Role          string `json:"role"`
	Away          bool   `json:"away_mode_enabled"`
	LastRequestAt int64  `json:"last_request_at,omitempty"`
}

type adminsResponse struct {
	Type   string          `json:"type"`
	Admins []intercomAdmin `json:"admins"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/admins", nil)
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
		return nil, fmt.Errorf("intercom: read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("intercom: API error %d: %s", resp.StatusCode, body)
	}

	var result adminsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("intercom: decode response: %w", err)
	}

	users := make([]core.User, 0, len(result.Admins))
	for _, a := range result.Admins {
		displayName := a.Name
		if displayName == "" {
			displayName = a.Email
		}
		status := "active"
		if a.Away {
			status = "away"
		}
		user := core.User{
			Email:       a.Email,
			DisplayName: displayName,
			Role:        a.Role,
			Status:      status,
			ProviderID:  a.ID,
		}
		if a.LastRequestAt != 0 {
			t := time.Unix(a.LastRequestAt, 0).UTC()
			user.LastActivityAt = &t
		}
		users = append(users, user)
	}

	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("intercom: adding users not supported via API")
}

func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	users, err := p.ListUsers(ctx)
	if err != nil {
		return err
	}
	var adminID string
	for _, u := range users {
		if u.Email == email {
			adminID = u.ProviderID
			break
		}
	}
	if adminID == "" {
		return fmt.Errorf("intercom: user %s not found", email)
	}

	url := fmt.Sprintf("%s/admins/%s/away", p.baseURL, adminID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("intercom: set away failed %d: %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("intercom: setting roles not supported via API")
}

