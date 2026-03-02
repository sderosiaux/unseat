package docusign

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.docusign.com/management"

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

func (p *Provider) Name() string { return "docusign" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type dsUser struct {
	ID         string `json:"id"`
	UserName   string `json:"user_name"`
	Email      string `json:"email"`
	UserStatus string `json:"user_status"`
}

type usersResponse struct {
	Users []dsUser `json:"users"`
	Paging struct {
		ResultSetSize          int `json:"result_set_size"`
		ResultSetStartPosition int `json:"result_set_start_position"`
		TotalSetSize           int `json:"total_set_size"`
	} `json:"paging"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	start := 0
	take := 100

	for {
		endpoint := fmt.Sprintf("%s/v2/organizations/%s/users?start=%d&take=%d", p.baseURL, p.orgID, start, take)

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
			return nil, fmt.Errorf("docusign: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("docusign: API returned %d: %s", resp.StatusCode, body)
		}

		var result usersResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("docusign: decode response: %w", err)
		}

		for _, u := range result.Users {
			status := "active"
			if u.UserStatus != "active" {
				status = u.UserStatus
			}
			all = append(all, core.User{
				Email:       u.Email,
				DisplayName: u.UserName,
				Role:        "member",
				Status:      status,
				ProviderID:  u.ID,
			})
		}

		if start+take >= result.Paging.TotalSetSize {
			break
		}
		start += take
	}

	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("docusign: add user not supported")
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
		return fmt.Errorf("docusign: user %s not found", email)
	}

	endpoint := fmt.Sprintf("%s/v2/organizations/%s/users/%s/profiles", p.baseURL, p.orgID, userID)
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
		return fmt.Errorf("docusign: delete user returned %d: %s", resp.StatusCode, body)
	}

	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("docusign: set role not supported")
}
