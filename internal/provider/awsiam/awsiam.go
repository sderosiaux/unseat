package awsiam

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

type Provider struct {
	token   string
	baseURL string
	client  *http.Client
}

// New creates a provider for AWS IAM Identity Center (SSO) SCIM endpoint.
// scimEndpoint is the full SCIM base URL (varies per AWS Identity Center instance).
func New(token, scimEndpoint string) *Provider {
	return &Provider{
		token:   token,
		baseURL: scimEndpoint,
		client:  &http.Client{},
	}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "aws-iam" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type scimEmail struct {
	Value   string `json:"value"`
	Type    string `json:"type"`
	Primary bool   `json:"primary"`
}

type scimName struct {
	GivenName  string `json:"givenName"`
	FamilyName string `json:"familyName"`
}

type scimUser struct {
	ID          string      `json:"id"`
	UserName    string      `json:"userName"`
	DisplayName string      `json:"displayName"`
	Name        scimName    `json:"name"`
	Emails      []scimEmail `json:"emails"`
	Active      bool        `json:"active"`
}

type scimListResponse struct {
	Schemas      []string   `json:"schemas"`
	Resources    []scimUser `json:"Resources"`
	TotalResults int        `json:"totalResults"`
	ItemsPerPage int        `json:"itemsPerPage"`
	StartIndex   int        `json:"startIndex"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []scimUser
	startIndex := 1
	count := 100

	for {
		url := fmt.Sprintf("%s/Users?startIndex=%d&count=%d", p.baseURL, startIndex, count)
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
			return nil, fmt.Errorf("aws-iam: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("aws-iam: API error (status %d): %s", resp.StatusCode, body)
		}

		var result scimListResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("aws-iam: decode response: %w", err)
		}

		all = append(all, result.Resources...)

		if startIndex+len(result.Resources) > result.TotalResults {
			break
		}
		startIndex += len(result.Resources)
	}

	users := make([]core.User, 0, len(all))
	for _, u := range all {
		email := u.UserName
		for _, e := range u.Emails {
			if e.Primary {
				email = e.Value
				break
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
	return fmt.Errorf("aws-iam: add user not supported via SCIM API")
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
		return fmt.Errorf("aws-iam: user %s not found", email)
	}

	url := fmt.Sprintf("%s/Users/%s", p.baseURL, userID)
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

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("aws-iam: delete user failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("aws-iam: set role not supported via SCIM API")
}
