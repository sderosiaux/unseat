package box

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.box.com"

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

func (p *Provider) Name() string { return "box" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     false,
		CanRemove:  true,
		CanSuspend: false,
		CanSetRole: false,
	}
}

type boxUser struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Name   string `json:"name"`
	Login  string `json:"login"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

type boxListResponse struct {
	TotalCount int       `json:"total_count"`
	Limit      int       `json:"limit"`
	Offset     int       `json:"offset"`
	Entries    []boxUser `json:"entries"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []boxUser
	offset := 0
	limit := 100

	for {
		url := fmt.Sprintf("%s/2.0/users?limit=%d&offset=%d", p.baseURL, limit, offset)
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
			return nil, fmt.Errorf("box: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("box: API error (status %d): %s", resp.StatusCode, body)
		}

		var result boxListResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("box: decode response: %w", err)
		}

		all = append(all, result.Entries...)

		if offset+limit >= result.TotalCount {
			break
		}
		offset += limit
	}

	users := make([]core.User, 0, len(all))
	for _, u := range all {
		status := u.Status
		if status == "" {
			status = "active"
		}
		role := u.Role
		if role == "" {
			role = "user"
		}
		users = append(users, core.User{
			Email:       u.Login,
			DisplayName: u.Name,
			Role:        role,
			Status:      status,
			ProviderID:  u.ID,
		})
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("box: programmatic user invites not supported")
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
		return fmt.Errorf("box: user %s not found", email)
	}

	url := fmt.Sprintf("%s/2.0/users/%s", p.baseURL, userID)
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
		return fmt.Errorf("box: remove user failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("box: role changes not supported")
}
