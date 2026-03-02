package adobe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://usermanagement.adobe.io"

type Provider struct {
	token   string
	orgID   string
	baseURL string
	client  *http.Client
}

func New(token, orgID string) *Provider {
	return &Provider{token: token, orgID: orgID, baseURL: defaultBaseURL, client: &http.Client{}}
}

func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "adobe" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type adobeUser struct {
	ID        string   `json:"id"`
	Email     string   `json:"email"`
	FirstName string   `json:"firstname"`
	LastName  string   `json:"lastname"`
	Username  string   `json:"username"`
	Status    string   `json:"status"`
	Type      string   `json:"type"`
	Groups    []string `json:"groups"`
}

type usersResponse struct {
	LastPage bool        `json:"lastPage"`
	Result   string      `json:"result"`
	Users    []adobeUser `json:"users"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	page := 0

	for {
		endpoint := fmt.Sprintf("%s/v2/usermanagement/users/%s/%d", p.baseURL, p.orgID, page)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+p.token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := p.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("adobe: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("adobe: API returned %d: %s", resp.StatusCode, body)
		}

		var result usersResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("adobe: decode response: %w", err)
		}

		for _, u := range result.Users {
			displayName := u.FirstName
			if u.LastName != "" {
				if displayName != "" {
					displayName += " "
				}
				displayName += u.LastName
			}
			if displayName == "" {
				displayName = u.Email
			}

			all = append(all, core.User{
				Email:       u.Email,
				DisplayName: displayName,
				Role:        u.Type,
				Status:      u.Status,
				ProviderID:  u.ID,
			})
		}

		if result.LastPage {
			break
		}
		page++
	}

	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("adobe: add user not supported")
}

type actionCommand struct {
	User string             `json:"user"`
	Do   []actionStep       `json:"do"`
}

type actionStep struct {
	RemoveFromOrg map[string]any `json:"removeFromOrg"`
}

func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	users, err := p.ListUsers(ctx)
	if err != nil {
		return err
	}

	var found bool
	for _, u := range users {
		if u.Email == email {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("adobe: user %s not found", email)
	}

	commands := []actionCommand{
		{
			User: email,
			Do: []actionStep{
				{RemoveFromOrg: map[string]any{}},
			},
		},
	}

	payload, err := json.Marshal(commands)
	if err != nil {
		return fmt.Errorf("adobe: marshal action: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v2/usermanagement/action/%s", p.baseURL, p.orgID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("adobe: action returned %d: %s", resp.StatusCode, body)
	}

	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("adobe: set role not supported")
}
