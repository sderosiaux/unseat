package onepassword

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://scim.1password.com"

type Provider struct {
	token   string
	baseURL string
	client  *http.Client
}

func New(token string) *Provider {
	return &Provider{token: token, baseURL: defaultBaseURL, client: &http.Client{}}
}

// WithBaseURL overrides the SCIM bridge URL (useful for testing or self-hosted bridges).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "1password" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     false,
		CanRemove:  true,
		CanSuspend: true,
		CanSetRole: false,
	}
}

type scimEmail struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary"`
}

type scimUser struct {
	ID          string      `json:"id"`
	UserName    string      `json:"userName"`
	DisplayName string      `json:"displayName"`
	Emails      []scimEmail `json:"emails"`
	Active      bool        `json:"active"`
}

type scimListResponse struct {
	Resources    []scimUser `json:"Resources"`
	TotalResults int        `json:"totalResults"`
	ItemsPerPage int        `json:"itemsPerPage"`
	StartIndex   int        `json:"startIndex"`
}

type scimPatchOperation struct {
	Op    string         `json:"op"`
	Value map[string]any `json:"value"`
}

type scimPatchOp struct {
	Schemas    []string             `json:"schemas"`
	Operations []scimPatchOperation `json:"Operations"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []scimUser
	startIndex := 1
	count := 100

	for {
		url := fmt.Sprintf("%s/scim/v2/Users?startIndex=%d&count=%d", p.baseURL, startIndex, count)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+p.token)
		req.Header.Set("Accept", "application/scim+json")

		resp, err := p.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("1password: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("1password: API error (status %d): %s", resp.StatusCode, body)
		}

		var result scimListResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("1password: decode response: %w", err)
		}

		all = append(all, result.Resources...)

		if startIndex+result.ItemsPerPage > result.TotalResults {
			break
		}
		startIndex += result.ItemsPerPage
	}

	users := make([]core.User, 0, len(all))
	for _, u := range all {
		email := u.UserName
		if len(u.Emails) > 0 {
			for _, e := range u.Emails {
				if e.Primary {
					email = e.Value
					break
				}
			}
		}
		status := "active"
		if !u.Active {
			status = "suspended"
		}
		users = append(users, core.User{
			Email:       email,
			DisplayName: u.DisplayName,
			Role:        "member",
			Status:      status,
			ProviderID:  u.ID,
		})
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("1password: programmatic user invites not supported via SCIM API")
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
		return fmt.Errorf("1password: user %s not found", email)
	}

	patch := scimPatchOp{
		Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
		Operations: []scimPatchOperation{
			{Op: "replace", Value: map[string]any{"active": false}},
		},
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("1password: marshal patch: %w", err)
	}

	url := fmt.Sprintf("%s/scim/v2/Users/%s", p.baseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/scim+json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("1password: deactivate user failed (status %d): %s", resp.StatusCode, respBody)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("1password: role changes not supported via SCIM API")
}
