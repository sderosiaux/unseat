package dropbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.dropboxapi.com"

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

func (p *Provider) Name() string { return "dropbox" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     false,
		CanRemove:  true,
		CanSuspend: false,
		CanSetRole: false,
	}
}

type dropboxName struct {
	GivenName   string `json:"given_name"`
	Surname     string `json:"surname"`
	DisplayName string `json:"display_name"`
}

type dropboxProfile struct {
	TeamMemberID string      `json:"team_member_id"`
	Email        string      `json:"email"`
	Name         dropboxName `json:"name"`
	Status       dropboxTag  `json:"status"`
	Role         dropboxTag  `json:"role"`
}

type dropboxTag struct {
	Tag string `json:".tag"`
}

type dropboxMember struct {
	Profile dropboxProfile `json:"profile"`
}

type dropboxListRequest struct {
	Limit int `json:"limit"`
}

type dropboxContinueRequest struct {
	Cursor string `json:"cursor"`
}

type dropboxListResponse struct {
	Members []dropboxMember `json:"members"`
	Cursor  string          `json:"cursor"`
	HasMore bool            `json:"has_more"`
}

type dropboxRemoveRequest struct {
	User dropboxUserSelector `json:"user"`
}

type dropboxUserSelector struct {
	Tag   string `json:".tag"`
	Email string `json:"email"`
}

func (p *Provider) doPost(ctx context.Context, path string, body any) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("dropbox: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(b))
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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("dropbox: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dropbox: API error (status %d): %s", resp.StatusCode, respBody)
	}

	return respBody, nil
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	body, err := p.doPost(ctx, "/2/team/members/list_v2", dropboxListRequest{Limit: 100})
	if err != nil {
		return nil, err
	}

	var result dropboxListResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("dropbox: decode response: %w", err)
	}

	all := result.Members

	for result.HasMore {
		body, err = p.doPost(ctx, "/2/team/members/list/continue_v2", dropboxContinueRequest{Cursor: result.Cursor})
		if err != nil {
			return nil, err
		}
		result = dropboxListResponse{}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("dropbox: decode continue response: %w", err)
		}
		all = append(all, result.Members...)
	}

	users := make([]core.User, 0, len(all))
	for _, m := range all {
		status := m.Profile.Status.Tag
		if status == "" {
			status = "active"
		}
		role := m.Profile.Role.Tag
		if role == "" {
			role = "member"
		}
		users = append(users, core.User{
			Email:       m.Profile.Email,
			DisplayName: m.Profile.Name.DisplayName,
			Role:        role,
			Status:      status,
			ProviderID:  m.Profile.TeamMemberID,
		})
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("dropbox: programmatic user invites not supported")
}

func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	reqBody := dropboxRemoveRequest{
		User: dropboxUserSelector{
			Tag:   "email",
			Email: email,
		},
	}

	_, err := p.doPost(ctx, "/2/team/members/remove", reqBody)
	return err
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("dropbox: role changes not supported")
}
