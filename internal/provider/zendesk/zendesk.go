package zendesk

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

type Provider struct {
	token   string
	baseURL string
	client  *httpclient.Client
}

func New(token, subdomain string) *Provider {
	return &Provider{
		token:   token,
		baseURL: fmt.Sprintf("https://%s.zendesk.com", subdomain),
		client:  httpclient.New(),
	}
}

func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "zendesk" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove:       true,
		ReportsActivity: true,
	}
}

type zendeskUser struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	Active      bool   `json:"active"`
	LastLoginAt string `json:"last_login_at,omitempty"`
}

type usersResponse struct {
	Users []zendeskUser `json:"users"`
	Meta  struct {
		HasMore     bool   `json:"has_more"`
		AfterCursor string `json:"after_cursor"`
	} `json:"meta"`
	Links struct {
		Next string `json:"next"`
	} `json:"links"`
}

func (p *Provider) doGet(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/json")

	return p.client.DoJSON(ctx, "zendesk", req, out)
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	url := fmt.Sprintf("%s/api/v2/users?role[]=admin&role[]=agent&page[size]=100", p.baseURL)

	for {
		var resp usersResponse
		if err := p.doGet(ctx, url, &resp); err != nil {
			return nil, err
		}

		for _, u := range resp.Users {
			status := "active"
			if !u.Active {
				status = "suspended"
			}
			user := core.User{
				Email:       u.Email,
				DisplayName: u.Name,
				Role:        u.Role,
				Status:      status,
				ProviderID:  strconv.FormatInt(u.ID, 10),
			}
			if u.LastLoginAt != "" {
				if t, err := time.Parse(time.RFC3339, u.LastLoginAt); err == nil {
					user.LastActivityAt = &t
				}
			}
			all = append(all, user)
		}

		if !resp.Meta.HasMore || resp.Links.Next == "" {
			break
		}
		url = resp.Links.Next
	}

	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("zendesk: adding users not supported via API")
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
		return fmt.Errorf("zendesk: user %s not found", email)
	}

	url := fmt.Sprintf("%s/api/v2/users/%s", p.baseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)

	return p.client.DoJSON(ctx, "zendesk", req, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("zendesk: setting roles not supported via API")
}
