package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://gitlab.com"

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

func (p *Provider) Name() string { return "gitlab" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove:  true,
		CanSuspend: true,
	}
}

type apiUser struct {
	ID               int    `json:"id"`
	Username         string `json:"username"`
	Name             string `json:"name"`
	Email            string `json:"email"`
	State            string `json:"state"`
	IsAdmin          bool   `json:"is_admin"`
	AvatarURL        string `json:"avatar_url"`
	CurrentSignInAt  string `json:"current_sign_in_at,omitempty"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	page := 1

	for {
		url := fmt.Sprintf("%s/api/v4/users?active=true&per_page=100&page=%d", p.baseURL, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("PRIVATE-TOKEN", p.token)

		resp, err := p.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("gitlab: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("gitlab: API error (status %d): %s", resp.StatusCode, body)
		}

		var users []apiUser
		if err := json.Unmarshal(body, &users); err != nil {
			return nil, fmt.Errorf("gitlab: decode response: %w", err)
		}

		if len(users) == 0 {
			break
		}

		for _, u := range users {
			role := "member"
			if u.IsAdmin {
				role = "admin"
			}
			status := "active"
			if u.State == "blocked" {
				status = "suspended"
			}
			user := core.User{
				Email:       u.Email,
				DisplayName: u.Name,
				Role:        role,
				Status:      status,
				ProviderID:  strconv.Itoa(u.ID),
			}
			if u.CurrentSignInAt != "" {
				if t, err := time.Parse(time.RFC3339, u.CurrentSignInAt); err == nil {
					user.LastActivityAt = &t
				}
			}
			all = append(all, user)
		}

		// Check X-Total-Pages for pagination
		totalPagesStr := resp.Header.Get("X-Total-Pages")
		if totalPagesStr != "" {
			totalPages, err := strconv.Atoi(totalPagesStr)
			if err == nil && page >= totalPages {
				break
			}
		} else {
			break
		}
		page++
	}

	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("gitlab: programmatic user invites not supported via API")
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
		return fmt.Errorf("gitlab: user %s not found", email)
	}

	url := fmt.Sprintf("%s/api/v4/users/%s/block", p.baseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gitlab: block user failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("gitlab: role changes not supported via API")
}
