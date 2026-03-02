package freshdesk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
)

type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func New(apiKey, subdomain string) *Provider {
	return &Provider{
		apiKey:  apiKey,
		baseURL: fmt.Sprintf("https://%s.freshdesk.com", subdomain),
		client:  &http.Client{},
	}
}

func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "freshdesk" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type freshdeskContact struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

type freshdeskAgent struct {
	ID         int64            `json:"id"`
	Occasional bool             `json:"occasional"`
	RoleIDs    []int64          `json:"role_ids"`
	Available  bool             `json:"available"`
	Contact    freshdeskContact `json:"contact"`
	Active     bool             `json:"active"`
	LastLoginAt string          `json:"last_login_at,omitempty"`
}

func (p *Provider) doGet(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(p.apiKey, "X")
	req.Header.Set("Accept", "application/json")
	return p.client.Do(req)
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	page := 1

	for {
		url := fmt.Sprintf("%s/api/v2/agents?per_page=100&page=%d", p.baseURL, page)

		resp, err := p.doGet(ctx, url)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("freshdesk: read response: %w", err)
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("freshdesk: API error %d: %s", resp.StatusCode, body)
		}

		var agents []freshdeskAgent
		if err := json.Unmarshal(body, &agents); err != nil {
			return nil, fmt.Errorf("freshdesk: decode response: %w", err)
		}

		if len(agents) == 0 {
			break
		}

		for _, a := range agents {
			displayName := a.Contact.Name
			if displayName == "" {
				displayName = a.Contact.Email
			}
			role := "agent"
			if a.Occasional {
				role = "occasional"
			}
			status := "active"
			if !a.Available {
				status = "unavailable"
			}
			user := core.User{
				Email:       a.Contact.Email,
				DisplayName: displayName,
				Role:        role,
				Status:      status,
				ProviderID:  strconv.FormatInt(a.ID, 10),
			}
			if a.LastLoginAt != "" {
				if t, err := time.Parse(time.RFC3339, a.LastLoginAt); err == nil {
					user.LastActivityAt = &t
				}
			}
			all = append(all, user)
		}

		if len(agents) < 100 {
			break
		}
		page++
	}

	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("freshdesk: adding users not supported via API")
}

func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	users, err := p.ListUsers(ctx)
	if err != nil {
		return err
	}
	var agentID string
	for _, u := range users {
		if u.Email == email {
			agentID = u.ProviderID
			break
		}
	}
	if agentID == "" {
		return fmt.Errorf("freshdesk: user %s not found", email)
	}

	url := fmt.Sprintf("%s/api/v2/agents/%s", p.baseURL, agentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(p.apiKey, "X")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("freshdesk: delete agent failed %d: %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("freshdesk: setting roles not supported via API")
}
