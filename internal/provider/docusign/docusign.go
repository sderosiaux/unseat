package docusign

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://api.docusign.com/management"

type Provider struct {
	token   string
	orgID   string
	baseURL string
	client  *httpclient.Client
}

func New(token, orgID string) *Provider {
	return &Provider{token: token, orgID: orgID, baseURL: defaultBaseURL, client: httpclient.New()}
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
	Users  []dsUser `json:"users"`
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

		var result usersResponse
		if err := p.client.DoJSON(ctx, "docusign", req, &result); err != nil {
			return nil, err
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

	return p.client.DoJSON(ctx, "docusign", req, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("docusign: set role not supported")
}
