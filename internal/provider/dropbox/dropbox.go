package dropbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://api.dropboxapi.com"

type Provider struct {
	token   string
	baseURL string
	client  *httpclient.Client
}

func New(token string) *Provider {
	return &Provider{token: token, baseURL: defaultBaseURL, client: httpclient.New()}
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

// doPost issues a Dropbox RPC-style POST and decodes the JSON reply into out.
// Pass a nil out when the reply body is irrelevant.
func (p *Provider) doPost(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("dropbox: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")

	return p.client.DoJSON(ctx, "dropbox", req, out)
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var result dropboxListResponse
	if err := p.doPost(ctx, "/2/team/members/list_v2", dropboxListRequest{Limit: 100}, &result); err != nil {
		return nil, err
	}

	all := result.Members

	for result.HasMore {
		cursor := result.Cursor
		result = dropboxListResponse{}
		if err := p.doPost(ctx, "/2/team/members/list/continue_v2", dropboxContinueRequest{Cursor: cursor}, &result); err != nil {
			return nil, err
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

	return p.doPost(ctx, "/2/team/members/remove", reqBody, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("dropbox: role changes not supported")
}
