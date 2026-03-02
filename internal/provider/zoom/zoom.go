package zoom

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.zoom.us"

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

func (p *Provider) Name() string { return "zoom" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     false,
		CanRemove:  true,
		CanSuspend: false,
		CanSetRole: false,
	}
}

type zoomUser struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	DisplayName string `json:"display_name"`
	Type        int    `json:"type"`
	Status      string `json:"status"`
	RoleName    string `json:"role_name"`
}

type zoomListResponse struct {
	PageSize      int        `json:"page_size"`
	NextPageToken string     `json:"next_page_token"`
	Users         []zoomUser `json:"users"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []zoomUser
	nextPageToken := ""

	for {
		url := fmt.Sprintf("%s/v2/users?status=active&page_size=300", p.baseURL)
		if nextPageToken != "" {
			url += "&next_page_token=" + nextPageToken
		}

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
			return nil, fmt.Errorf("zoom: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("zoom: API error (status %d): %s", resp.StatusCode, body)
		}

		var result zoomListResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("zoom: decode response: %w", err)
		}

		all = append(all, result.Users...)

		if result.NextPageToken == "" {
			break
		}
		nextPageToken = result.NextPageToken
	}

	users := make([]core.User, 0, len(all))
	for _, u := range all {
		displayName := u.DisplayName
		if displayName == "" {
			displayName = fmt.Sprintf("%s %s", u.FirstName, u.LastName)
		}
		role := "member"
		if u.RoleName != "" {
			role = u.RoleName
		}
		users = append(users, core.User{
			Email:       u.Email,
			DisplayName: displayName,
			Role:        role,
			Status:      "active",
			ProviderID:  u.ID,
		})
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("zoom: programmatic user invites not supported")
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
		return fmt.Errorf("zoom: user %s not found", email)
	}

	url := fmt.Sprintf("%s/v2/users/%s?action=disassociate", p.baseURL, userID)
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
		return fmt.Errorf("zoom: remove user failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("zoom: role changes not supported")
}
