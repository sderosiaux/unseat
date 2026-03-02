package sentry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://sentry.io"

type Provider struct {
	token   string
	orgSlug string
	baseURL string
	client  *http.Client
}

func New(token, orgSlug string) *Provider {
	return &Provider{token: token, orgSlug: orgSlug, baseURL: defaultBaseURL, client: &http.Client{}}
}

func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "sentry" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type sentryUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"orgRole"`
	User  *struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	} `json:"user"`
	Pending bool `json:"pending"`
}

func (p *Provider) doGet(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	return p.client.Do(req)
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	cursor := ""

	for {
		url := fmt.Sprintf("%s/api/0/organizations/%s/members/", p.baseURL, p.orgSlug)
		if cursor != "" {
			url += "?cursor=" + cursor
		}

		resp, err := p.doGet(ctx, url)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("sentry: read response: %w", err)
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("sentry: API error %d: %s", resp.StatusCode, body)
		}

		var members []sentryUser
		if err := json.Unmarshal(body, &members); err != nil {
			return nil, fmt.Errorf("sentry: decode response: %w", err)
		}

		for _, m := range members {
			email := m.Email
			displayName := m.Name
			if m.User != nil {
				if m.User.Email != "" {
					email = m.User.Email
				}
				if m.User.Name != "" {
					displayName = m.User.Name
				}
			}
			if displayName == "" {
				displayName = email
			}
			status := "active"
			if m.Pending {
				status = "invited"
			}
			all = append(all, core.User{
				Email:       email,
				DisplayName: displayName,
				Role:        m.Role,
				Status:      status,
				ProviderID:  m.ID,
			})
		}

		nextCursor := parseLinkCursor(resp.Header.Get("Link"))
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return all, nil
}

// parseLinkCursor extracts the next cursor from a Sentry Link header.
// Format: <url>; rel="previous"; results="false"; cursor="...", <url>; rel="next"; results="true"; cursor="..."
func parseLinkCursor(link string) string {
	if link == "" {
		return ""
	}
	// Parse the Link header manually for the next cursor with results="true"
	parts := splitLink(link)
	for _, part := range parts {
		if containsAll(part, `rel="next"`, `results="true"`) {
			return extractParam(part, "cursor")
		}
	}
	return ""
}

func splitLink(link string) []string {
	var parts []string
	depth := 0
	start := 0
	for i, c := range link {
		if c == '<' {
			depth++
		} else if c == '>' {
			depth--
		} else if c == ',' && depth == 0 {
			parts = append(parts, link[start:i])
			start = i + 1
		}
	}
	parts = append(parts, link[start:])
	return parts
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func extractParam(s, key string) string {
	target := key + `="`
	idx := -1
	for i := 0; i <= len(s)-len(target); i++ {
		if s[i:i+len(target)] == target {
			idx = i + len(target)
			break
		}
	}
	if idx < 0 {
		return ""
	}
	end := idx
	for end < len(s) && s[end] != '"' {
		end++
	}
	return s[idx:end]
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("sentry: adding users not supported via API")
}

func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	users, err := p.ListUsers(ctx)
	if err != nil {
		return err
	}
	var memberID string
	for _, u := range users {
		if u.Email == email {
			memberID = u.ProviderID
			break
		}
	}
	if memberID == "" {
		return fmt.Errorf("sentry: user %s not found", email)
	}

	url := fmt.Sprintf("%s/api/0/organizations/%s/members/%s/", p.baseURL, p.orgSlug, memberID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sentry: delete member failed %d: %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("sentry: setting roles not supported via API")
}

