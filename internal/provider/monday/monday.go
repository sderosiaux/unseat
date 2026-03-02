package monday

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.monday.com/v2"

type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func New(apiKey string) *Provider {
	return &Provider{apiKey: apiKey, baseURL: defaultBaseURL, client: &http.Client{}}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "monday" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type gqlRequest struct {
	Query string `json:"query"`
}

type mondayUser struct {
	ID      json.Number `json:"id"`
	Name    string      `json:"name"`
	Email   string      `json:"email"`
	IsAdmin bool        `json:"is_admin"`
	IsGuest bool        `json:"is_guest"`
	Enabled bool        `json:"enabled"`
}

type listUsersData struct {
	Users []mondayUser `json:"users"`
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []gqlError      `json:"errors,omitempty"`
}

type gqlError struct {
	Message string `json:"message"`
}

func (p *Provider) doGraphQL(ctx context.Context, query string, result any) error {
	payload, err := json.Marshal(gqlRequest{Query: query})
	if err != nil {
		return fmt.Errorf("monday: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("monday: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("monday: API error (status %d): %s", resp.StatusCode, body)
	}

	var gqlResp gqlResponse
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return fmt.Errorf("monday: decode response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("monday: GraphQL error: %s", gqlResp.Errors[0].Message)
	}

	if result != nil {
		if err := json.Unmarshal(gqlResp.Data, result); err != nil {
			return fmt.Errorf("monday: decode data: %w", err)
		}
	}
	return nil
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	query := `{ users { id name email is_admin is_guest enabled } }`

	var data listUsersData
	if err := p.doGraphQL(ctx, query, &data); err != nil {
		return nil, err
	}

	users := make([]core.User, 0, len(data.Users))
	for _, u := range data.Users {
		role := "member"
		if u.IsAdmin {
			role = "admin"
		} else if u.IsGuest {
			role = "guest"
		}
		status := "active"
		if !u.Enabled {
			status = "suspended"
		}
		users = append(users, core.User{
			Email:       u.Email,
			DisplayName: u.Name,
			Role:        role,
			Status:      status,
			ProviderID:  u.ID.String(),
		})
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("monday: programmatic user invites not supported via API")
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
		return fmt.Errorf("monday: user %s not found", email)
	}

	mutation := fmt.Sprintf(`mutation { deactivate_users (user_ids: [%s]) { deactivated_users { id } } }`, userID)
	return p.doGraphQL(ctx, mutation, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("monday: role changes not supported via API")
}
