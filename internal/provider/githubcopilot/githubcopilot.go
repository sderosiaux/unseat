// Package githubcopilot reads GitHub Copilot seat assignments.
//
// Copilot is billed as its own per-seat pool, separate from the organisation's
// member seats, and it is one of the very few products where the vendor reports
// genuine per-seat usage: every assignment carries the timestamp of its last
// editor activity. That makes an idle Copilot seat provable rather than
// inferred — the opposite of the org member list, where absence of a public
// event says nothing.
package githubcopilot

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://api.github.com"

// seatsPageSize is the API maximum.
const seatsPageSize = 100

// maxSeatPages bounds the walk. At 100 seats a page this covers 10k seats,
// far beyond any real Copilot deployment.
const maxSeatPages = 100

type Provider struct {
	token   string
	org     string
	baseURL string
	client  *httpclient.Client
}

func New(token, org string) *Provider {
	return &Provider{token: token, org: org, baseURL: defaultBaseURL, client: httpclient.New()}
}

func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "github-copilot" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		// Deliberately read-only. Copilot seats can be unassigned through the
		// API, but this connector exists to measure, and a seat removed here is
		// removed for a working developer mid-task.
		CanAdd:     false,
		CanRemove:  false,
		CanSuspend: false,
		CanSetRole: false,
		// The one flag in this codebase that is not a judgement call: every
		// seat carries last_activity_at, straight from the vendor.
		ReportsActivity: true,
	}
}

type copilotAssignee struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

type copilotSeat struct {
	CreatedAt               string          `json:"created_at"`
	LastActivityAt          string          `json:"last_activity_at"`
	LastActivityEditor      string          `json:"last_activity_editor"`
	PendingCancellationDate string          `json:"pending_cancellation_date"`
	PlanType                string          `json:"plan_type"`
	Assignee                copilotAssignee `json:"assignee"`
}

type seatsResponse struct {
	TotalSeats int           `json:"total_seats"`
	Seats      []copilotSeat `json:"seats"`
}

func (p *Provider) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	return p.client.DoJSON(ctx, "github-copilot", req, out)
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var users []core.User

	for page := 1; page <= maxSeatPages; page++ {
		var resp seatsResponse
		path := fmt.Sprintf("/orgs/%s/copilot/billing/seats?per_page=%d&page=%d", p.org, seatsPageSize, page)
		if err := p.get(ctx, path, &resp); err != nil {
			if httpclient.IsUnauthorized(err) {
				return nil, fmt.Errorf("%w\n  Copilot seat data needs a token with the `manage_billing:copilot` "+
					"scope and an org with Copilot Business or Enterprise", err)
			}
			return nil, err
		}
		if len(resp.Seats) == 0 {
			break
		}

		for _, s := range resp.Seats {
			// The login stays bare, exactly as the github connector does: core
			// classifies an identifier with no "@" as unresolved, which is the
			// honest verdict until an alias maps it to a person.
			u := core.User{
				Email:       s.Assignee.Login,
				DisplayName: s.Assignee.Login,
				Role:        s.PlanType,
				Status:      core.StatusActive,
				ProviderID:  s.Assignee.Login,
			}
			// A seat already scheduled to end is not a live seat to reclaim —
			// somebody has dealt with it.
			if s.PendingCancellationDate != "" {
				u.Status = core.StatusSuspended
			}
			if t, ok := parseSeatTime(s.LastActivityAt); ok {
				u.LastActivityAt = &t
			}
			if s.LastActivityEditor != "" {
				u.Metadata = map[string]string{"editor": s.LastActivityEditor}
			}
			users = append(users, u)
		}

		if len(resp.Seats) < seatsPageSize {
			break
		}
	}

	return users, nil
}

// Billing reports the size of the Copilot seat pool.
//
// No price is inferred. The org billing endpoint does expose a Copilot unit
// price, but it is a usage report rather than a statement of this org's seat
// rate, and Enterprise agreements differ — see principle 3.
//
// The seat_breakdown counters on /copilot/billing are deliberately NOT used.
// active_this_cycle and inactive_this_cycle are relative to the billing cycle,
// so minutes after a cycle rolls over they report nearly every seat as
// inactive. Observed live: a pool whose ten seats had all been used within
// forty-eight hours reported nine inactive, because the cycle had just reset.
// Per-seat last_activity_at is the only trustworthy signal here.
func (p *Provider) Billing(ctx context.Context) (*core.Billing, error) {
	var resp struct {
		SeatBreakdown struct {
			Total int `json:"total"`
		} `json:"seat_breakdown"`
		PlanType string `json:"plan_type"`
	}
	if err := p.get(ctx, "/orgs/"+p.org+"/copilot/billing", &resp); err != nil {
		return nil, err
	}
	if resp.SeatBreakdown.Total == 0 && resp.PlanType == "" {
		return nil, nil
	}
	return &core.Billing{
		Plan:        resp.PlanType,
		BilledSeats: resp.SeatBreakdown.Total,
		FilledSeats: resp.SeatBreakdown.Total,
	}, nil
}

// parseSeatTime reads the offset-bearing RFC3339 the seats endpoint returns
// ("2026-07-31T14:26:52-04:00"). Unlike the org audit log, this one is not
// milliseconds since epoch.
func parseSeatTime(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("github-copilot: assigning seats not supported — this connector is read-only")
}

func (p *Provider) RemoveUser(_ context.Context, _ string) error {
	return fmt.Errorf("github-copilot: unassigning seats not supported — this connector is read-only")
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("github-copilot: role changes not supported")
}
