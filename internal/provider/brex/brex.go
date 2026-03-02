package brex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://platform.brexapis.com"

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

func (p *Provider) Name() string { return "brex" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove:  true,
		CanSuspend: true,
	}
}

type brexUser struct {
	ID        string `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Status    string `json:"status"`
}

type usersResponse struct {
	Items      []brexUser `json:"items"`
	NextCursor *string    `json:"next_cursor"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	cursor := ""

	for {
		endpoint := fmt.Sprintf("%s/v2/users?limit=100", p.baseURL)
		if cursor != "" {
			endpoint += "&cursor=" + cursor
		}

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
			return nil, fmt.Errorf("brex: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("brex: API returned %d: %s", resp.StatusCode, body)
		}

		var result usersResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("brex: decode response: %w", err)
		}

		for _, u := range result.Items {
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

			status := "active"
			switch u.Status {
			case "ACTIVE":
				status = "active"
			case "DISABLED":
				status = "suspended"
			case "INVITED":
				status = "invited"
			default:
				status = u.Status
			}

			all = append(all, core.User{
				Email:       u.Email,
				DisplayName: displayName,
				Role:        "member",
				Status:      status,
				ProviderID:  u.ID,
			})
		}

		if result.NextCursor == nil || *result.NextCursor == "" {
			break
		}
		cursor = *result.NextCursor
	}

	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("brex: add user not supported")
}

type statusUpdate struct {
	Status string `json:"status"`
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
		return fmt.Errorf("brex: user %s not found", email)
	}

	payload, err := json.Marshal(statusUpdate{Status: "DISABLED"})
	if err != nil {
		return fmt.Errorf("brex: marshal status: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v2/users/%s/status", p.baseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(payload))
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
		return fmt.Errorf("brex: update status returned %d: %s", resp.StatusCode, body)
	}

	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("brex: set role not supported")
}
