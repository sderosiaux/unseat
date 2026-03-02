package gcpiam

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://cloudidentity.googleapis.com"

type Provider struct {
	token      string
	customerID string
	baseURL    string
	client     *http.Client
}

func New(token, customerID string) *Provider {
	return &Provider{
		token:      token,
		customerID: customerID,
		baseURL:    defaultBaseURL,
		client:     &http.Client{},
	}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(u string) *Provider {
	p.baseURL = u
	return p
}

func (p *Provider) Name() string { return "gcp-iam" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type userName struct {
	GivenName  string `json:"givenName"`
	FamilyName string `json:"familyName"`
	FullName   string `json:"fullName"`
}

type userEmail struct {
	Address string `json:"address"`
	Primary bool   `json:"primary"`
}

type gcpUser struct {
	ID               string      `json:"id"`
	PrimaryEmail     string      `json:"primaryEmail"`
	Name             userName    `json:"name"`
	Emails           []userEmail `json:"emails"`
	Suspended        bool        `json:"suspended"`
	IsAdmin          bool        `json:"isAdmin"`
	IsEnrolledIn2Sv  bool        `json:"isEnrolledIn2Sv"`
	OrgUnitPath      string      `json:"orgUnitPath"`
	CreationTime     string      `json:"creationTime"`
	LastLoginTime    string      `json:"lastLoginTime"`
}

type listUsersResponse struct {
	Users         []gcpUser `json:"users"`
	NextPageToken string    `json:"nextPageToken"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []gcpUser
	pageToken := ""

	for {
		params := url.Values{}
		params.Set("customer", p.customerID)
		params.Set("maxResults", "100")
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}

		reqURL := fmt.Sprintf("%s/admin/directory/v1/users?%s", p.baseURL, params.Encode())
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
			return nil, fmt.Errorf("gcp-iam: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("gcp-iam: API error (status %d): %s", resp.StatusCode, body)
		}

		var result listUsersResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("gcp-iam: decode response: %w", err)
		}

		all = append(all, result.Users...)

		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}

	users := make([]core.User, 0, len(all))
	for _, u := range all {
		status := "active"
		if u.Suspended {
			status = "suspended"
		}
		role := "member"
		if u.IsAdmin {
			role = "admin"
		}
		users = append(users, core.User{
			Email:       u.PrimaryEmail,
			DisplayName: u.Name.FullName,
			Role:        role,
			Status:      status,
			ProviderID:  u.ID,
		})
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("gcp-iam: add user not supported")
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
		return fmt.Errorf("gcp-iam: user %s not found", email)
	}

	reqURL := fmt.Sprintf("%s/admin/directory/v1/users/%s", p.baseURL, userID)
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
		return fmt.Errorf("gcp-iam: delete user failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("gcp-iam: set role not supported")
}
