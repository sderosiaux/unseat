package hubspot

import (
	"context"
	"fmt"
	"net/http"
	"strings"

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
		// Deliberately false. The struct used to declare a lastActiveTime
		// field and the code parsed it, but /settings/v3/users returns no such
		// field — verified against a live portal, where the entire payload is
		// id, email, firstName, lastName, roleIds, seatNames, superAdmin.
		// Claiming activity reporting turned every nil into "never active" and
		// flagged an entire portal as inactive at high severity.
		ReportsActivity: false,
	}
}

type hubspotUser struct {
	ID            string   `json:"id"`
	Email         string   `json:"email"`
	FirstName     string   `json:"firstName"`
	LastName      string   `json:"lastName"`
	RoleIDs       []string `json:"roleIds"`
	SuperAdmin    bool     `json:"superAdmin"`
	PrimaryTeamID string   `json:"primaryTeamId"`
	// SeatNames is what HubSpot actually bills for: "core", "sales-enterprise",
	// "service-enterprise", "view-only". Seat types differ in price by an order
	// of magnitude, so the mix matters more than the head count.
	SeatNames []string `json:"seatNames"`
}

// displayName prefers the real name; the API always returns an email but not
// always a name.
func (u hubspotUser) displayName() string {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name == "" {
		return u.Email
	}
	return name
}

// seat returns the billable seat type, which is the field that carries cost.
func (u hubspotUser) seat() string {
	if len(u.SeatNames) == 0 {
		return ""
	}
	return strings.Join(u.SeatNames, "+")
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
			// A private app that simply lacks one scope returns the same
			// opaque 403 as a revoked token. Name the scope: it is a two-click
			// fix once you know which one.
			if httpclient.IsUnauthorized(err) {
				return nil, fmt.Errorf("%w\n  Grant the `settings.users.read` scope to the private app "+
					"(Settings > Integrations > Private Apps > your app > Scopes)", err)
			}
			return nil, err
		}

		for _, u := range resp.Results {
			role := "member"
			if u.SuperAdmin {
				role = "super_admin"
			}
			user := core.User{
				Email:       u.Email,
				DisplayName: u.displayName(),
				Role:        role,
				// Every user this endpoint returns holds a seat. HubSpot has
				// no deactivated-but-listed state: removing someone removes
				// them from this list, so being here means being billed.
				Status:     core.StatusActive,
				ProviderID: u.ID,
			}
			if seat := u.seat(); seat != "" {
				user.Metadata = map[string]string{"seat": seat}
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
