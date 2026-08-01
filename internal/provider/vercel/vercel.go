package vercel

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://api.vercel.com"

type Provider struct {
	token   string
	teamID  string
	baseURL string
	client  *httpclient.Client
}

func New(token, teamID string) *Provider {
	return &Provider{
		token:   token,
		teamID:  teamID,
		baseURL: defaultBaseURL,
		client:  httpclient.New(),
	}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "vercel" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type vercelMember struct {
	UID       string `json:"uid"`
	Email     string `json:"email"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Confirmed bool   `json:"confirmed"`
	CreatedAt int64  `json:"createdAt"`
}

type pagination struct {
	HasNext bool   `json:"hasNext"`
	Count   int    `json:"count"`
	Next    *int64 `json:"next"`
}

type listMembersResponse struct {
	Members    []vercelMember `json:"members"`
	Pagination pagination     `json:"pagination"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	var until string

	for {
		endpoint := fmt.Sprintf("%s/v3/teams/%s/members?limit=100", p.baseURL, p.teamID)
		if until != "" {
			endpoint += "&until=" + until
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+p.token)

		var result listMembersResponse
		if err := p.client.DoJSON(ctx, "vercel", req, &result); err != nil {
			return nil, err
		}

		for _, m := range result.Members {
			displayName := m.Name
			if displayName == "" {
				displayName = m.Username
			}
			status := "active"
			if !m.Confirmed {
				status = "invited"
			}
			all = append(all, core.User{
				Email:       m.Email,
				DisplayName: displayName,
				Role:        m.Role,
				Status:      status,
				ProviderID:  m.UID,
			})
		}

		if !result.Pagination.HasNext || result.Pagination.Next == nil {
			break
		}
		until = strconv.FormatInt(*result.Pagination.Next, 10)
	}

	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("vercel: programmatic user invites not supported via API")
}

func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	users, err := p.ListUsers(ctx)
	if err != nil {
		return err
	}

	var uid string
	for _, u := range users {
		if u.Email == email {
			uid = u.ProviderID
			break
		}
	}
	if uid == "" {
		return fmt.Errorf("vercel: user %s not found", email)
	}

	endpoint := fmt.Sprintf("%s/v1/teams/%s/members/%s", p.baseURL, p.teamID, uid)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)

	return p.client.DoJSON(ctx, "vercel", req, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("vercel: role changes not supported via API")
}
