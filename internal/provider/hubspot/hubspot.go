package hubspot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.hubapi.com"

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

func (p *Provider) Name() string { return "hubspot" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     false,
		CanRemove:  true,
		CanSuspend: false,
		CanSetRole: false,
	}
}

type hubspotUser struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	RoleID        string `json:"roleId"`
	SuperAdmin    bool   `json:"superAdmin"`
	PrimaryTeamID string `json:"primaryTeamId"`
}

type pagingNext struct {
	After string `json:"after"`
	Link  string `json:"link"`
}

type paging struct {
	Next *pagingNext `json:"next"`
}

type usersResponse struct {
	Results []hubspotUser `json:"results"`
	Paging  *paging       `json:"paging,omitempty"`
}

func (p *Provider) doGet(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+path, nil)
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
		return nil, fmt.Errorf("hubspot: read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("hubspot: API error %d: %s", resp.StatusCode, body)
	}
	return body, nil
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	var after string

	for {
		path := "/settings/v3/users/"
		if after != "" {
			path += "?after=" + after
		}

		body, err := p.doGet(ctx, path)
		if err != nil {
			return nil, err
		}

		var resp usersResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("hubspot: decode response: %w", err)
		}

		for _, u := range resp.Results {
			role := "member"
			if u.SuperAdmin {
				role = "super_admin"
			}
			all = append(all, core.User{
				Email:       u.Email,
				DisplayName: u.Email,
				Role:        role,
				Status:      "active",
				ProviderID:  u.ID,
			})
		}

		if resp.Paging == nil || resp.Paging.Next == nil || resp.Paging.Next.After == "" {
			break
		}
		after = resp.Paging.Next.After
	}

	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("hubspot: adding users not supported via API")
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
		return fmt.Errorf("hubspot: user %s not found", email)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, p.baseURL+"/settings/v3/users/"+userID, nil)
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
		return fmt.Errorf("hubspot: delete user failed %d: %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("hubspot: setting roles not supported via API")
}
