package auth0

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/sderosiaux/unseat/internal/core"
)

type Provider struct {
	token   string
	baseURL string
	client  *http.Client
}

func New(token, domain string) *Provider {
	return &Provider{
		token:   token,
		baseURL: fmt.Sprintf("https://%s", domain),
		client:  &http.Client{},
	}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(u string) *Provider {
	p.baseURL = u
	return p
}

func (p *Provider) Name() string { return "auth0" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     false,
		CanRemove:  true,
		CanSuspend: false,
		CanSetRole: false,
	}
}

type auth0User struct {
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Blocked   bool   `json:"blocked"`
	LastLogin string `json:"last_login,omitempty"`
}

type listUsersResponse struct {
	Users []auth0User `json:"users"`
	Start int         `json:"start"`
	Limit int         `json:"limit"`
	Total int         `json:"total"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []auth0User
	page := 0
	perPage := 100

	for {
		params := url.Values{
			"per_page":       {strconv.Itoa(perPage)},
			"page":           {strconv.Itoa(page)},
			"include_totals": {"true"},
		}
		reqURL := fmt.Sprintf("%s/api/v2/users?%s", p.baseURL, params.Encode())

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
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
			return nil, fmt.Errorf("auth0: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("auth0: API error (status %d): %s", resp.StatusCode, body)
		}

		var result listUsersResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("auth0: decode response: %w", err)
		}

		all = append(all, result.Users...)

		if len(all) >= result.Total || len(result.Users) == 0 {
			break
		}
		page++
	}

	users := make([]core.User, 0, len(all))
	for _, u := range all {
		status := "active"
		if u.Blocked {
			status = "suspended"
		}
		users = append(users, core.User{
			Email:       u.Email,
			DisplayName: u.Name,
			Role:        "member",
			Status:      status,
			ProviderID:  u.UserID,
		})
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("auth0: programmatic user creation not supported")
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
		return fmt.Errorf("auth0: user %s not found", email)
	}

	reqURL := fmt.Sprintf("%s/api/v2/users/%s", p.baseURL, url.PathEscape(userID))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
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
		return fmt.Errorf("auth0: delete user failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("auth0: role changes not supported")
}
