package intercom

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://api.intercom.io"

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

func (p *Provider) Name() string { return "intercom" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true, // set away
		// Deliberately false: last_request_at is not a field on the Admin
		// object. Per Intercom's published OpenAPI spec, the admin schema
		// (github.com/intercom/Intercom-OpenAPI, descriptions/2.10/api.intercom.io.yaml)
		// exposes only type, id, name, email, job_title, away_mode_enabled,
		// away_mode_reassign, has_inbox_seat, team_ids, avatar, team_priority_level.
		// last_request_at exists only on Contact and Company objects. Parsing
		// it off /admins always yields the zero value, and with this flag set,
		// a nil LastActivityAt reads as "never active" for every admin.
		ReportsActivity: false,
	}
}

// intercomAdmin mirrors the Admin object, verified field by field against
// Intercom's own OpenAPI schema (intercom/Intercom-OpenAPI, Admin).
//
// It used to declare `role` and `last_request_at` as well. Neither exists on
// this object: role has no equivalent at all, and last_request_at lives on
// Company and Contact. Both decoded to their zero value on every response, so
// Role was always empty — silently disabling admin-sprawl detection here — and
// the activity claim rested on a field the API never sends.
type intercomAdmin struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Away  bool   `json:"away_mode_enabled"`
	// HasInboxSeat is what Intercom actually bills: "identifies if this admin
	// has a paid inbox seat". A teammate without one occupies no paid seat.
	HasInboxSeat bool `json:"has_inbox_seat"`
}

type adminsResponse struct {
	Type   string          `json:"type"`
	Admins []intercomAdmin `json:"admins"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/admins", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/json")

	var result adminsResponse
	if err := p.client.DoJSON(ctx, "intercom", req, &result); err != nil {
		return nil, err
	}

	users := make([]core.User, 0, len(result.Admins))
	for _, a := range result.Admins {
		displayName := a.Name
		if displayName == "" {
			displayName = a.Email
		}
		user := core.User{
			Email:       a.Email,
			DisplayName: displayName,
			// The Admin object carries no permission or role field, so none is
			// invented. Admin-sprawl detection cannot work for this provider.
			Role: "",
			// away_mode_enabled is a presence toggle, not account state: a
			// teammate who stepped away still holds their seat. Reporting it as
			// a status would have reconciliation read them as deactivated.
			Status:     core.StatusActive,
			ProviderID: a.ID,
			Metadata: map[string]string{
				"away":       strconv.FormatBool(a.Away),
				"inbox_seat": strconv.FormatBool(a.HasInboxSeat),
			},
		}
		users = append(users, user)
	}

	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("intercom: adding users not supported via API")
}

func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	users, err := p.ListUsers(ctx)
	if err != nil {
		return err
	}
	var adminID string
	for _, u := range users {
		if u.Email == email {
			adminID = u.ProviderID
			break
		}
	}
	if adminID == "" {
		return fmt.Errorf("intercom: user %s not found", email)
	}

	url := fmt.Sprintf("%s/admins/%s/away", p.baseURL, adminID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/json")

	return p.client.DoJSON(ctx, "intercom", req, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("intercom: setting roles not supported via API")
}
