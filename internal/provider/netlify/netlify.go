package netlify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.netlify.com"

type Provider struct {
	token       string
	accountSlug string
	baseURL     string
	client      *http.Client
}

func New(token, accountSlug string) *Provider {
	return &Provider{
		token:       token,
		accountSlug: accountSlug,
		baseURL:     defaultBaseURL,
		client:      &http.Client{},
	}
}

func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "netlify" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type netlifyMember struct {
	ID       string `json:"id"`
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	Avatar   string `json:"avatar"`
	Role     string `json:"role"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	page := 1

	for {
		endpoint := fmt.Sprintf("%s/api/v1/%s/members?page=%s&per_page=100",
			p.baseURL, p.accountSlug, strconv.Itoa(page))

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
			return nil, fmt.Errorf("netlify: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("netlify: API error (status %d): %s", resp.StatusCode, body)
		}

		var members []netlifyMember
		if err := json.Unmarshal(body, &members); err != nil {
			return nil, fmt.Errorf("netlify: decode response: %w", err)
		}

		for _, m := range members {
			all = append(all, core.User{
				Email:       m.Email,
				DisplayName: m.FullName,
				Role:        m.Role,
				Status:      "active",
				ProviderID:  m.ID,
			})
		}

		if len(members) < 100 {
			break
		}
		page++
	}

	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("netlify: add user not supported via API")
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
		return fmt.Errorf("netlify: user %s not found", email)
	}

	endpoint := fmt.Sprintf("%s/api/v1/%s/members/%s", p.baseURL, p.accountSlug, memberID)
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
		return fmt.Errorf("netlify: remove member failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("netlify: set role not supported via API")
}
