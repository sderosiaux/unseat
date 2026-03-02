package okta

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
)

type Provider struct {
	token   string
	baseURL string
	client  *http.Client
}

func New(token, domain string) *Provider {
	return &Provider{
		token:   token,
		baseURL: fmt.Sprintf("https://%s.okta.com", domain),
		client:  &http.Client{},
	}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "okta" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     false,
		CanRemove:  true,
		CanSuspend: true,
		CanSetRole: false,
	}
}

type oktaUserProfile struct {
	Login       string `json:"login"`
	Email       string `json:"email"`
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	DisplayName string `json:"displayName,omitempty"`
}

type oktaUser struct {
	ID        string          `json:"id"`
	Status    string          `json:"status"`
	LastLogin string          `json:"lastLogin,omitempty"`
	Profile   oktaUserProfile `json:"profile"`
}

// linkRegexp parses RFC 5988 Link headers: <url>; rel="next"
var linkRegexp = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []oktaUser
	nextURL := fmt.Sprintf("%s/api/v1/users?limit=200", p.baseURL)

	for nextURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "SSWS "+p.token)
		req.Header.Set("Accept", "application/json")

		resp, err := p.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("okta: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("okta: API error (status %d): %s", resp.StatusCode, body)
		}

		var page []oktaUser
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("okta: decode response: %w", err)
		}

		all = append(all, page...)

		// Parse Link header for next page.
		nextURL = ""
		for _, link := range resp.Header.Values("Link") {
			if matches := linkRegexp.FindStringSubmatch(link); len(matches) == 2 {
				nextURL = matches[1]
				break
			}
		}
	}

	users := make([]core.User, 0, len(all))
	for _, u := range all {
		displayName := u.Profile.DisplayName
		if displayName == "" {
			displayName = strings.TrimSpace(u.Profile.FirstName + " " + u.Profile.LastName)
		}
		email := u.Profile.Email
		if email == "" {
			email = u.Profile.Login
		}
		user := core.User{
			Email:       email,
			DisplayName: displayName,
			Role:        "member",
			Status:      normalizeOktaStatus(u.Status),
			ProviderID:  u.ID,
		}
		if u.LastLogin != "" {
			if t, err := time.Parse(time.RFC3339, u.LastLogin); err == nil {
				user.LastActivityAt = &t
			}
		}
		users = append(users, user)
	}
	return users, nil
}

func normalizeOktaStatus(status string) string {
	switch strings.ToUpper(status) {
	case "ACTIVE", "RECOVERY", "PASSWORD_EXPIRED", "LOCKED_OUT":
		return "active"
	case "STAGED", "PROVISIONED":
		return "invited"
	case "SUSPENDED", "DEPROVISIONED":
		return "suspended"
	default:
		return strings.ToLower(status)
	}
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("okta: programmatic user creation not supported")
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
		return fmt.Errorf("okta: user %s not found", email)
	}

	url := fmt.Sprintf("%s/api/v1/users/%s/lifecycle/deactivate", p.baseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "SSWS "+p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("okta: deactivate user failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("okta: role changes not supported")
}
