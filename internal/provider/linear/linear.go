package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.linear.app/graphql"

type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func New(apiKey string) *Provider {
	return &Provider{apiKey: apiKey, baseURL: defaultBaseURL, client: &http.Client{}}
}

// WithBaseURL overrides the GraphQL endpoint (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "linear" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     false,
		CanRemove:  true,
		CanSuspend: true,
		CanSetRole: false,
	}
}

type gqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (p *Provider) graphql(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	body, _ := json.Marshal(gqlRequest{Query: query, Variables: variables})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var gqlResp gqlResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", gqlResp.Errors[0].Message)
	}
	return gqlResp.Data, nil
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	query := `query { users { nodes { id name email active admin guest lastSeen } } }`
	data, err := p.graphql(ctx, query, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Users struct {
			Nodes []struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				Email    string `json:"email"`
				Active   bool   `json:"active"`
				Admin    bool   `json:"admin"`
				Guest    bool   `json:"guest"`
				LastSeen string `json:"lastSeen"`
			} `json:"nodes"`
		} `json:"users"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	users := make([]core.User, 0, len(result.Users.Nodes))
	for _, u := range result.Users.Nodes {
		status := "active"
		if !u.Active {
			status = "suspended"
		}
		role := "member"
		if u.Admin {
			role = "admin"
		} else if u.Guest {
			role = "guest"
		}
		user := core.User{
			Email:       u.Email,
			DisplayName: u.Name,
			Role:        role,
			Status:      status,
			ProviderID:  u.ID,
		}
		if u.LastSeen != "" {
			if t, err := time.Parse(time.RFC3339, u.LastSeen); err == nil {
				user.LastActivityAt = &t
			}
		}
		users = append(users, user)
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("linear: programmatic user invites not supported via API")
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
		return fmt.Errorf("linear: user %s not found", email)
	}

	query := `mutation($id: String!) { userSuspend(id: $id) { success } }`
	_, err = p.graphql(ctx, query, map[string]any{"id": userID})
	return err
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("linear: role changes not supported via API")
}
