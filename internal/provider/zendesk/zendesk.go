package zendesk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/sderosiaux/unseat/internal/core"
)

type Provider struct {
	token   string
	baseURL string
	client  *http.Client
}

func New(token, subdomain string) *Provider {
	return &Provider{
		token:   token,
		baseURL: fmt.Sprintf("https://%s.zendesk.com", subdomain),
		client:  &http.Client{},
	}
}

func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "zendesk" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type zendeskUser struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	Active bool   `json:"active"`
}

type usersResponse struct {
	Users []zendeskUser `json:"users"`
	Meta  struct {
		HasMore      bool   `json:"has_more"`
		AfterCursor  string `json:"after_cursor"`
	} `json:"meta"`
	Links struct {
		Next string `json:"next"`
	} `json:"links"`
}

func (p *Provider) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
		return nil, fmt.Errorf("zendesk: read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("zendesk: API error %d: %s", resp.StatusCode, body)
	}
	return body, nil
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	url := fmt.Sprintf("%s/api/v2/users?role[]=admin&role[]=agent&page[size]=100", p.baseURL)

	for {
		body, err := p.doGet(ctx, url)
		if err != nil {
			return nil, err
		}

		var resp usersResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("zendesk: decode response: %w", err)
		}

		for _, u := range resp.Users {
			status := "active"
			if !u.Active {
				status = "suspended"
			}
			all = append(all, core.User{
				Email:       u.Email,
				DisplayName: u.Name,
				Role:        u.Role,
				Status:      status,
				ProviderID:  strconv.FormatInt(u.ID, 10),
			})
		}

		if !resp.Meta.HasMore || resp.Links.Next == "" {
			break
		}
		url = resp.Links.Next
	}

	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("zendesk: adding users not supported via API")
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
		return fmt.Errorf("zendesk: user %s not found", email)
	}

	url := fmt.Sprintf("%s/api/v2/users/%s", p.baseURL, userID)
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

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("zendesk: delete user failed %d: %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("zendesk: setting roles not supported via API")
}
