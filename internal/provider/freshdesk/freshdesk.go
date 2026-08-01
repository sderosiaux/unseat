package freshdesk

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
	apiKey  string
	baseURL string
	client  *httpclient.Client
}

func New(apiKey, subdomain string) *Provider {
	return &Provider{
		apiKey:  apiKey,
		baseURL: fmt.Sprintf("https://%s.freshdesk.com", subdomain),
		client:  httpclient.New(),
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
		// Agents carry last_login_at.
		ReportsActivity: true,
	}
}

// freshdeskContact holds the person behind the agent. Account state and login
// history live HERE, not on the agent: `active` and `last_login_at` are
// documented as contact attributes. They were previously declared on the agent
// struct, where they silently decoded to the zero value on every response.
type freshdeskContact struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	// Active is a pointer so an absent field is distinguishable from an
	// explicit false. Defaulting a missing field to "deactivated" would mark
	// an entire helpdesk as suspended.
	Active      *bool  `json:"active"`
	LastLoginAt string `json:"last_login_at,omitempty"`
}

type freshdeskAgent struct {
	ID         int64            `json:"id"`
	Occasional bool             `json:"occasional"`
	RoleIDs    []int64          `json:"role_ids"`
	Available  bool             `json:"available"`
	Contact    freshdeskContact `json:"contact"`
}

func (p *Provider) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(p.apiKey, "X")
	req.Header.Set("Accept", "application/json")
	return p.client.DoJSON(ctx, "freshdesk", req, out)
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	page := 1

	for {
		url := fmt.Sprintf("%s/api/v2/agents?per_page=100&page=%d", p.baseURL, page)

		var agents []freshdeskAgent
		if err := p.getJSON(ctx, url, &agents); err != nil {
			return nil, err
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
			// `available` is a ticket-routing toggle, not account state — an
			// agent who stepped away is still a paid, active seat. Only
			// `active` means the account is deactivated, and reconciliation
			// depends on that distinction: it reads StatusSuspended as
			// "already deactivated, do not act again".
			status := core.StatusActive
			if a.Contact.Active != nil && !*a.Contact.Active {
				status = core.StatusSuspended
			}
			user := core.User{
				Email:       a.Contact.Email,
				DisplayName: displayName,
				Role:        role,
				Status:      status,
				ProviderID:  strconv.FormatInt(a.ID, 10),
				Metadata:    map[string]string{"available": strconv.FormatBool(a.Available)},
			}
			if a.Contact.LastLoginAt != "" {
				if t, err := time.Parse(time.RFC3339, a.Contact.LastLoginAt); err == nil {
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

	return p.client.DoJSON(ctx, "freshdesk", req, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("freshdesk: setting roles not supported via API")
}
