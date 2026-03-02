package canva

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://www.canva.com"

type Provider struct {
	token   string
	baseURL string
	client  *http.Client
}

func New(token string) *Provider {
	return &Provider{token: token, baseURL: defaultBaseURL, client: &http.Client{}}
}

func (p *Provider) WithBaseURL(u string) *Provider {
	p.baseURL = u
	return p
}

func (p *Provider) Name() string { return "canva" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type scimName struct {
	GivenName  string `json:"givenName"`
	FamilyName string `json:"familyName"`
}

type scimEmail struct {
	Value   string `json:"value"`
	Type    string `json:"type"`
	Primary bool   `json:"primary"`
}

type scimUser struct {
	ID       string      `json:"id"`
	UserName string      `json:"userName"`
	Name     scimName    `json:"name"`
	Emails   []scimEmail `json:"emails"`
	Active   bool        `json:"active"`
	Role     string      `json:"role,omitempty"`
}

type scimListResponse struct {
	TotalResults int        `json:"totalResults"`
	StartIndex   int        `json:"startIndex"`
	ItemsPerPage int        `json:"itemsPerPage"`
	Resources    []scimUser `json:"Resources"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	startIndex := 1
	count := 10 // Canva SCIM max per page

	for {
		endpoint := fmt.Sprintf("%s/_scim/v2/Users?startIndex=%s&count=%s",
			p.baseURL,
			url.QueryEscape(strconv.Itoa(startIndex)),
			url.QueryEscape(strconv.Itoa(count)),
		)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+p.token)

		resp, err := p.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("canva: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("canva: API returned %d: %s", resp.StatusCode, body)
		}

		var result scimListResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("canva: decode response: %w", err)
		}

		for _, u := range result.Resources {
			email := u.UserName
			if len(u.Emails) > 0 {
				email = u.Emails[0].Value
			}
			displayName := u.Name.GivenName
			if u.Name.FamilyName != "" {
				if displayName != "" {
					displayName += " "
				}
				displayName += u.Name.FamilyName
			}
			if displayName == "" {
				displayName = email
			}

			status := "active"
			if !u.Active {
				status = "suspended"
			}

			role := u.Role
			if role == "" {
				role = "member"
			}

			all = append(all, core.User{
				Email:       email,
				DisplayName: displayName,
				Role:        role,
				Status:      status,
				ProviderID:  u.ID,
			})
		}

		if startIndex+count > result.TotalResults {
			break
		}
		startIndex += count
	}

	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("canva: add user not supported")
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
		return fmt.Errorf("canva: user %s not found", email)
	}

	endpoint := fmt.Sprintf("%s/_scim/v2/Users/%s", p.baseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("canva: delete user returned %d: %s", resp.StatusCode, body)
	}

	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("canva: set role not supported")
}
