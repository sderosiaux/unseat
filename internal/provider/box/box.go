package box

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://api.box.com"

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

func (p *Provider) Name() string { return "box" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     false,
		CanRemove:  true,
		CanSuspend: false,
		CanSetRole: false,
		// Deliberately false: LastActivityAt below is filled from the user
		// record's modified_at, which moves when an admin edits the account,
		// not when the person uses Box. Treating it as usage would report
		// active people as reclaimable.
		ReportsActivity: false,
	}
}

type boxUser struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Name       string `json:"name"`
	Login      string `json:"login"`
	Role       string `json:"role"`
	Status     string `json:"status"`
	ModifiedAt string `json:"modified_at,omitempty"`
}

type boxListResponse struct {
	TotalCount int       `json:"total_count"`
	Limit      int       `json:"limit"`
	Offset     int       `json:"offset"`
	Entries    []boxUser `json:"entries"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []boxUser
	offset := 0
	limit := 100

	for {
		url := fmt.Sprintf("%s/2.0/users?limit=%d&offset=%d", p.baseURL, limit, offset)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+p.token)

		var result boxListResponse
		if err := p.client.DoJSON(ctx, "box", req, &result); err != nil {
			return nil, err
		}

		all = append(all, result.Entries...)

		if offset+limit >= result.TotalCount {
			break
		}
		offset += limit
	}

	users := make([]core.User, 0, len(all))
	for _, u := range all {
		status := u.Status
		if status == "" {
			status = "active"
		}
		role := u.Role
		if role == "" {
			role = "user"
		}
		user := core.User{
			Email:       u.Login,
			DisplayName: u.Name,
			Role:        role,
			Status:      status,
			ProviderID:  u.ID,
		}
		if u.ModifiedAt != "" {
			if t, err := time.Parse(time.RFC3339, u.ModifiedAt); err == nil {
				user.LastActivityAt = &t
			}
		}
		users = append(users, user)
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("box: programmatic user invites not supported")
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
		return fmt.Errorf("box: user %s not found", email)
	}

	url := fmt.Sprintf("%s/2.0/users/%s", p.baseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)

	return p.client.DoJSON(ctx, "box", req, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("box: role changes not supported")
}
