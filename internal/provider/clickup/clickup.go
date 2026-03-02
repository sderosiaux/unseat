package clickup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.clickup.com"

type Provider struct {
	token   string
	teamID  string
	baseURL string
	client  *http.Client
}

func New(token, teamID string) *Provider {
	return &Provider{
		token:   token,
		teamID:  teamID,
		baseURL: defaultBaseURL,
		client:  &http.Client{},
	}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "clickup" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type teamMember struct {
	User memberUser `json:"user"`
}

type memberUser struct {
	ID             int    `json:"id"`
	Username       string `json:"username"`
	Email          string `json:"email"`
	ProfilePicture string `json:"profilePicture"`
	Role           int    `json:"role"`
}

type teamDetail struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Members []teamMember `json:"members"`
}

type getTeamResponse struct {
	Team teamDetail `json:"team"`
}

// roleString maps ClickUp role integers to human-readable roles.
func roleString(role int) string {
	switch role {
	case 1:
		return "owner"
	case 2:
		return "admin"
	case 3:
		return "member"
	case 4:
		return "guest"
	default:
		return "member"
	}
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	endpoint := fmt.Sprintf("%s/api/v2/team/%s", p.baseURL, p.teamID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("clickup: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clickup: API error (status %d): %s", resp.StatusCode, body)
	}

	var result getTeamResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("clickup: decode response: %w", err)
	}

	users := make([]core.User, 0, len(result.Team.Members))
	for _, m := range result.Team.Members {
		users = append(users, core.User{
			Email:       m.User.Email,
			DisplayName: m.User.Username,
			Role:        roleString(m.User.Role),
			Status:      "active",
			ProviderID:  strconv.Itoa(m.User.ID),
		})
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("clickup: programmatic user invites not supported via API")
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
		return fmt.Errorf("clickup: user %s not found", email)
	}

	endpoint := fmt.Sprintf("%s/api/v2/team/%s/user/%s", p.baseURL, p.teamID, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("clickup: remove user failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("clickup: role changes not supported via API")
}
