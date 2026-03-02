package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.github.com"

type Provider struct {
	token   string
	org     string
	baseURL string
	client  *http.Client
}

func New(token, org string) *Provider {
	return &Provider{token: token, org: org, baseURL: defaultBaseURL, client: &http.Client{}}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "github" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type apiUser struct {
	ID    int    `json:"id"`
	Login string `json:"login"`
	Email string `json:"email"`
	Role  string `json:"role_name"`
}

type orgMember struct {
	Login   string `json:"login"`
	ID      int    `json:"id"`
	SiteURL string `json:"html_url"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	page := 1

	for {
		url := fmt.Sprintf("%s/orgs/%s/members?per_page=100&page=%d", p.baseURL, p.org, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+p.token)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := p.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("github: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("github: API error (status %d): %s", resp.StatusCode, body)
		}

		var members []orgMember
		if err := json.Unmarshal(body, &members); err != nil {
			return nil, fmt.Errorf("github: decode response: %w", err)
		}

		if len(members) == 0 {
			break
		}

		for _, m := range members {
			all = append(all, core.User{
				Email:       m.Login,
				DisplayName: m.Login,
				Role:        "member",
				Status:      "active",
				ProviderID:  fmt.Sprintf("%d", m.ID),
			})
		}

		// Check Link header for next page
		linkHeader := resp.Header.Get("Link")
		if !hasNextPage(linkHeader) {
			break
		}
		page++
	}

	return all, nil
}

// hasNextPage checks if the Link header contains a rel="next" link.
func hasNextPage(link string) bool {
	if link == "" {
		return false
	}
	// Simple check: look for rel="next" in the Link header
	for _, part := range splitLinks(link) {
		if len(part) > 0 {
			for _, param := range splitLinks2(part) {
				if param == `rel="next"` {
					return true
				}
			}
		}
	}
	return false
}

func splitLinks(s string) []string {
	var parts []string
	current := ""
	for _, c := range s {
		if c == ',' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func splitLinks2(s string) []string {
	var parts []string
	current := ""
	for _, c := range s {
		if c == ';' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	// Trim spaces
	for i := range parts {
		trimmed := ""
		started := false
		end := len(parts[i])
		for j := len(parts[i]) - 1; j >= 0; j-- {
			if parts[i][j] != ' ' {
				end = j + 1
				break
			}
		}
		for j := 0; j < end; j++ {
			if parts[i][j] != ' ' {
				started = true
			}
			if started {
				trimmed += string(parts[i][j])
			}
		}
		parts[i] = trimmed
	}
	return parts
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("github: programmatic user invites not supported via API")
}

func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	users, err := p.ListUsers(ctx)
	if err != nil {
		return err
	}

	var username string
	for _, u := range users {
		if u.Email == email {
			username = u.DisplayName
			break
		}
	}
	if username == "" {
		return fmt.Errorf("github: user %s not found", email)
	}

	url := fmt.Sprintf("%s/orgs/%s/members/%s", p.baseURL, p.org, username)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github: remove member failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("github: role changes not supported via API")
}
