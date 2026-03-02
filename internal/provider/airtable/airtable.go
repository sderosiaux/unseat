package airtable

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.airtable.com"

type Provider struct {
	token               string
	enterpriseAccountID string
	baseURL             string
	client              *http.Client
}

func New(token, enterpriseAccountID string) *Provider {
	return &Provider{
		token:               token,
		enterpriseAccountID: enterpriseAccountID,
		baseURL:             defaultBaseURL,
		client:              &http.Client{},
	}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "airtable" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type airtableUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	State string `json:"state"`
}

type airtableListResponse struct {
	Users  []airtableUser `json:"users"`
	Offset string         `json:"offset,omitempty"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []airtableUser
	offset := ""

	for {
		url := fmt.Sprintf("%s/v0/meta/enterpriseAccount/%s/users?pageSize=100", p.baseURL, p.enterpriseAccountID)
		if offset != "" {
			url += "&offset=" + offset
		}

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
			return nil, fmt.Errorf("airtable: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("airtable: API error (status %d): %s", resp.StatusCode, body)
		}

		var result airtableListResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("airtable: decode response: %w", err)
		}

		all = append(all, result.Users...)

		if result.Offset == "" {
			break
		}
		offset = result.Offset
	}

	users := make([]core.User, 0, len(all))
	for _, u := range all {
		status := normalizeAirtableState(u.State)
		users = append(users, core.User{
			Email:       u.Email,
			DisplayName: u.Name,
			Role:        "member",
			Status:      status,
			ProviderID:  u.ID,
		})
	}
	return users, nil
}

func normalizeAirtableState(state string) string {
	switch state {
	case "active":
		return "active"
	case "invited", "pending":
		return "invited"
	case "deactivated", "suspended":
		return "suspended"
	default:
		return state
	}
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("airtable: programmatic user creation not supported")
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
		return fmt.Errorf("airtable: user %s not found", email)
	}

	url := fmt.Sprintf("%s/v0/meta/enterpriseAccount/%s/users/%s", p.baseURL, p.enterpriseAccountID, userID)
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

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("airtable: delete user failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("airtable: role changes not supported")
}
