package azuread

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://graph.microsoft.com"

type Provider struct {
	token   string
	baseURL string
	client  *http.Client
}

func New(token string) *Provider {
	return &Provider{
		token:   token,
		baseURL: defaultBaseURL,
		client:  &http.Client{},
	}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(u string) *Provider {
	p.baseURL = u
	return p
}

func (p *Provider) Name() string { return "azure-ad" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type graphUser struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	Mail              string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
	AccountEnabled    bool   `json:"accountEnabled"`
}

type graphListResponse struct {
	Value    []graphUser `json:"value"`
	NextLink string      `json:"@odata.nextLink"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []graphUser

	params := url.Values{}
	params.Set("$top", "100")
	params.Set("$select", "id,displayName,mail,userPrincipalName,accountEnabled")
	reqURL := fmt.Sprintf("%s/v1.0/users?%s", p.baseURL, params.Encode())

	for reqURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+p.token)
		req.Header.Set("Accept", "application/json")

		resp, err := p.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("azure-ad: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("azure-ad: API error (status %d): %s", resp.StatusCode, body)
		}

		var result graphListResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("azure-ad: decode response: %w", err)
		}

		all = append(all, result.Value...)
		reqURL = result.NextLink
	}

	users := make([]core.User, 0, len(all))
	for _, u := range all {
		email := u.Mail
		if email == "" {
			email = u.UserPrincipalName
		}
		status := "active"
		if !u.AccountEnabled {
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
	return fmt.Errorf("azure-ad: add user not supported")
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
		return fmt.Errorf("azure-ad: user %s not found", email)
	}

	reqURL := fmt.Sprintf("%s/v1.0/users/%s", p.baseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
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
		return fmt.Errorf("azure-ad: delete user failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("azure-ad: set role not supported")
}
