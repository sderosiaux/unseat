package hubspot

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://api.hubapi.com"

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

func (p *Provider) Name() string { return "hubspot" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     false,
		CanRemove:  true,
		CanSuspend: false,
		CanSetRole: false,
		// Users carry lastActiveTime.
		ReportsActivity: true,
	}
}

type hubspotUser struct {
	ID             string `json:"id"`
	Email          string `json:"email"`
	RoleID         string `json:"roleId"`
	SuperAdmin     bool   `json:"superAdmin"`
	PrimaryTeamID  string `json:"primaryTeamId"`
	LastActiveTime string `json:"lastActiveTime,omitempty"`
}

type pagingNext struct {
	After string `json:"after"`
	Link  string `json:"link"`
}

type paging struct {
	Next *pagingNext `json:"next"`
}

type usersResponse struct {
	Results []hubspotUser `json:"results"`
	Paging  *paging       `json:"paging,omitempty"`
}

func (p *Provider) doGet(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)

	return p.client.DoJSON(ctx, "hubspot", req, out)
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	var after string

	for {
		path := "/settings/v3/users/"
		if after != "" {
			path += "?after=" + after
		}

		var resp usersResponse
		if err := p.doGet(ctx, path, &resp); err != nil {
			return nil, err
		}

		for _, u := range resp.Results {
			role := "member"
			if u.SuperAdmin {
				role = "super_admin"
			}
			user := core.User{
				Email:       u.Email,
				DisplayName: u.Email,
				Role:        role,
				Status:      "active",
				ProviderID:  u.ID,
			}
			if u.LastActiveTime != "" {
				if t, err := time.Parse(time.RFC3339, u.LastActiveTime); err == nil {
					user.LastActivityAt = &t
				}
			}
			all = append(all, user)
		}

		if resp.Paging == nil || resp.Paging.Next == nil || resp.Paging.Next.After == "" {
			break
		}
		after = resp.Paging.Next.After
	}

	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("hubspot: adding users not supported via API")
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
		return fmt.Errorf("hubspot: user %s not found", email)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, p.baseURL+"/settings/v3/users/"+userID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)

	return p.client.DoJSON(ctx, "hubspot", req, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("hubspot: setting roles not supported via API")
}
