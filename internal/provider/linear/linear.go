package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://api.linear.app/graphql"

type Provider struct {
	apiKey  string
	baseURL string
	client  *httpclient.Client
}

func New(apiKey string) *Provider {
	return &Provider{apiKey: apiKey, baseURL: defaultBaseURL, client: httpclient.New()}
}

// WithBaseURL overrides the GraphQL endpoint (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "linear" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:          false,
		CanRemove:       true,
		CanSuspend:      true,
		CanSetRole:      false,
		ReportsActivity: true,
		// "Customers are billed for the number of unsuspended users" — a
		// suspended seat leaves the next billing cycle, so it is not waste.
		SuspendedBilling: core.SuspendedBillingReleased,
	}
}

type gqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (p *Provider) graphql(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	body, _ := json.Marshal(gqlRequest{Query: query, Variables: variables})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", p.apiKey)

	var gqlResp gqlResponse
	if err := p.client.DoJSON(ctx, "linear", req, &gqlResp); err != nil {
		return nil, err
	}
	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", gqlResp.Errors[0].Message)
	}
	return gqlResp.Data, nil
}

// Linear caps connections at 50 nodes without an explicit `first`, and 250 is the
// per-page ceiling it accepts. includeDisabled surfaces suspended-but-billed seats,
// which are hidden by default.
const usersPageSize = 250

const listUsersQuery = `query($first: Int!, $after: String) {
  users(first: $first, after: $after, includeDisabled: true) {
    nodes { id name email active admin guest app lastSeen }
    pageInfo { hasNextPage endCursor }
  }
}`

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	users := make([]core.User, 0)
	var after *string

	for {
		data, err := p.graphql(ctx, listUsersQuery, map[string]any{"first": usersPageSize, "after": after})
		if err != nil {
			return nil, err
		}

		var result struct {
			Users struct {
				Nodes []struct {
					ID       string `json:"id"`
					Name     string `json:"name"`
					Email    string `json:"email"`
					Active   bool   `json:"active"`
					Admin    bool   `json:"admin"`
					Guest    bool   `json:"guest"`
					App      bool   `json:"app"`
					LastSeen string `json:"lastSeen"`
				} `json:"nodes"`
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"users"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, err
		}

		for _, u := range result.Users.Nodes {
			// OAuth integrations and the Linear agent are users too, with synthetic
			// emails that can never match a directory identity.
			if u.App {
				continue
			}
			status := "active"
			if !u.Active {
				status = "suspended"
			}
			role := "member"
			if u.Admin {
				role = "admin"
			} else if u.Guest {
				role = "guest"
			}
			user := core.User{
				Email:       u.Email,
				DisplayName: u.Name,
				Role:        role,
				Status:      status,
				ProviderID:  u.ID,
			}
			if u.LastSeen != "" {
				if t, err := time.Parse(time.RFC3339, u.LastSeen); err == nil {
					user.LastActivityAt = &t
				}
			}
			users = append(users, user)
		}

		// A missing cursor with hasNextPage set would loop forever on the same page.
		if !result.Users.PageInfo.HasNextPage || result.Users.PageInfo.EndCursor == "" {
			return users, nil
		}
		cursor := result.Users.PageInfo.EndCursor
		after = &cursor
	}
}

const billingQuery = `query {
  organization {
    subscription {
      type
      seats
      nextBillingAt
    }
  }
}`

// Billing reads the workspace subscription so the operator does not have to
// type a price unseat can read.
//
// Linear does not expose an amount, but encodes it in the plan identifier —
// "business_yearly_14" is 14 per seat per month. That is an inference about a
// vendor's naming, not a documented contract, so the rate is reported as
// derived from the plan rather than as a figure Linear stated. `seats` is
// authoritative: it is what Linear actually charges for.
func (p *Provider) Billing(ctx context.Context) (*core.Billing, error) {
	data, err := p.graphql(ctx, billingQuery, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Organization struct {
			Subscription *struct {
				Type          string `json:"type"`
				Seats         float64
				NextBillingAt string `json:"nextBillingAt"`
			} `json:"subscription"`
		} `json:"organization"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("linear: decode subscription: %w", err)
	}

	sub := result.Organization.Subscription
	if sub == nil {
		// A free workspace has no subscription. Not an error: there is simply
		// nothing to bill.
		return nil, nil
	}

	b := &core.Billing{
		Plan:        sub.Type,
		BilledSeats: int(sub.Seats),
	}
	if rate, ok := core.PriceFromPlanIdentifier(sub.Type); ok {
		b.CostPerSeat = rate
		b.Source = core.BillingSourcePlan
	}
	if sub.NextBillingAt != "" {
		if t, err := time.Parse(time.RFC3339, sub.NextBillingAt); err == nil {
			b.NextBillingAt = &t
		}
	}
	return b, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("linear: programmatic user invites not supported via API")
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
		return fmt.Errorf("linear: user %s not found", email)
	}

	query := `mutation($id: String!) { userSuspend(id: $id) { success } }`
	_, err = p.graphql(ctx, query, map[string]any{"id": userID})
	return err
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("linear: role changes not supported via API")
}
