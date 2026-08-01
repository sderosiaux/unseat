package linear

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
)

// Linear caps connections at 50 nodes without an explicit `first`. Integrations
// and webhooks are counted in dozens, not thousands, so one generous page is
// the whole set on any real workspace — but the walk still follows pageInfo
// rather than assuming it.
const credentialsPageSize = 250

// Integration and Webhook both expose a creator. That is the field this whole
// connector exists for: it is the only place a SaaS says which person's
// authorisation an automated connection runs under, and it is precisely what
// offboarding cannot see.
//
// Neither type exposes anything resembling usage. updatedAt moves when the
// integration is reconfigured, which is a record edit, not a signal that the
// connection did anything — so it is not read as one.
const listIntegrationsQuery = `query($first: Int!, $after: String) {
  integrations(first: $first, after: $after) {
    nodes { id service createdAt archivedAt creator { email } team { name } }
    pageInfo { hasNextPage endCursor }
  }
}`

const listWebhooksQuery = `query($first: Int!, $after: String) {
  webhooks(first: $first, after: $after) {
    nodes { id label url enabled createdAt creator { email } }
    pageInfo { hasNextPage endCursor }
  }
}`

type gqlCreator struct {
	Email string `json:"email"`
}

type integrationNode struct {
	ID         string     `json:"id"`
	Service    string     `json:"service"`
	CreatedAt  string     `json:"createdAt"`
	ArchivedAt string     `json:"archivedAt"`
	Creator    gqlCreator `json:"creator"`
	Team       *struct {
		Name string `json:"name"`
	} `json:"team"`
}

type webhookNode struct {
	ID        string     `json:"id"`
	Label     string     `json:"label"`
	URL       string     `json:"url"`
	Enabled   bool       `json:"enabled"`
	CreatedAt string     `json:"createdAt"`
	Creator   gqlCreator `json:"creator"`
}

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

// parseLinearTime reads Linear's ISO-8601 timestamps, returning nil rather
// than a zero time so an absent date never renders as 1970.
func parseLinearTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}

// ListCredentials reports the integrations and webhooks connecting this
// workspace to other systems.
//
// Each one carries the email of the person who authorised it. An integration
// authorised by someone who has left keeps running on their grant: it breaks
// the day the token is finally revoked, and until then it holds a former
// employee's access. Suspending their Google account does neither.
//
// No usage is reported and Capabilities says so — Linear exposes createdAt and
// updatedAt on these objects, and updatedAt tracks reconfiguration, not use.
func (p *Provider) ListCredentials(ctx context.Context) ([]core.Credential, error) {
	integrations, err := p.fetchIntegrations(ctx)
	if err != nil {
		return nil, err
	}
	webhooks, err := p.fetchWebhooks(ctx)
	if err != nil {
		return nil, err
	}

	creds := make([]core.Credential, 0, len(integrations)+len(webhooks))

	for _, in := range integrations {
		c := core.Credential{
			Provider:  "linear",
			Kind:      core.CredentialIntegration,
			ID:        in.ID,
			Label:     in.Service,
			CreatedAt: parseLinearTime(in.CreatedAt),
			CreatedBy: strings.ToLower(strings.TrimSpace(in.Creator.Email)),
			// Linear states no scopes for an integration, and inventing a
			// severity from the service name would be a guess. Reach stays
			// unknown, which is not the same as narrow.
			Reach: core.ReachUnknown,
		}
		if archived := parseLinearTime(in.ArchivedAt); archived != nil {
			c.Disabled = true
			c.DisabledAt = archived
		}
		if in.Team != nil && in.Team.Name != "" {
			c.Metadata = map[string]string{"team": in.Team.Name}
		}
		creds = append(creds, c)
	}

	for _, w := range webhooks {
		c := core.Credential{
			Provider:  "linear",
			Kind:      core.CredentialWebhook,
			ID:        w.ID,
			Label:     webhookLabel(w),
			CreatedAt: parseLinearTime(w.CreatedAt),
			CreatedBy: strings.ToLower(strings.TrimSpace(w.Creator.Email)),
			Reach:     core.ReachUnknown,
			// A webhook has no suspension date to report: Linear exposes only
			// the boolean, so the class is known and the "since" is not.
			Disabled: !w.Enabled,
		}
		if w.URL != "" {
			c.Metadata = map[string]string{"url": w.URL}
		}
		creds = append(creds, c)
	}

	return creds, nil
}

// webhookLabel prefers the operator's own label and falls back to the
// destination, because an unlabelled webhook is identified by where it sends.
func webhookLabel(w webhookNode) string {
	if label := strings.TrimSpace(w.Label); label != "" {
		return label
	}
	if w.URL != "" {
		return w.URL
	}
	return "webhook " + w.ID
}

func (p *Provider) fetchIntegrations(ctx context.Context) ([]integrationNode, error) {
	var all []integrationNode
	var after *string

	for {
		data, err := p.graphql(ctx, listIntegrationsQuery, map[string]any{"first": credentialsPageSize, "after": after})
		if err != nil {
			return nil, err
		}
		var result struct {
			Integrations struct {
				Nodes    []integrationNode `json:"nodes"`
				PageInfo pageInfo          `json:"pageInfo"`
			} `json:"integrations"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, err
		}

		all = append(all, result.Integrations.Nodes...)
		if !result.Integrations.PageInfo.HasNextPage || result.Integrations.PageInfo.EndCursor == "" {
			return all, nil
		}
		cursor := result.Integrations.PageInfo.EndCursor
		after = &cursor
	}
}

func (p *Provider) fetchWebhooks(ctx context.Context) ([]webhookNode, error) {
	var all []webhookNode
	var after *string

	for {
		data, err := p.graphql(ctx, listWebhooksQuery, map[string]any{"first": credentialsPageSize, "after": after})
		if err != nil {
			return nil, err
		}
		var result struct {
			Webhooks struct {
				Nodes    []webhookNode `json:"nodes"`
				PageInfo pageInfo      `json:"pageInfo"`
			} `json:"webhooks"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, err
		}

		all = append(all, result.Webhooks.Nodes...)
		if !result.Webhooks.PageInfo.HasNextPage || result.Webhooks.PageInfo.EndCursor == "" {
			return all, nil
		}
		cursor := result.Webhooks.PageInfo.EndCursor
		after = &cursor
	}
}
