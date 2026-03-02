package datadog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.datadoghq.com"

type Provider struct {
	apiKey  string
	appKey  string
	baseURL string
	client  *http.Client
}

func New(apiKey, appKey string) *Provider {
	return &Provider{
		apiKey:  apiKey,
		appKey:  appKey,
		baseURL: defaultBaseURL,
		client:  &http.Client{},
	}
}

func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "datadog" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type datadogUserAttributes struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Status   string `json:"status"`
	Disabled bool   `json:"disabled"`
	Title    string `json:"title"`
	Role     string `json:"role"`
}

type datadogUser struct {
	ID         string                `json:"id"`
	Type       string                `json:"type"`
	Attributes datadogUserAttributes `json:"attributes"`
}

type datadogMeta struct {
	Page struct {
		TotalCount    int `json:"total_count"`
		TotalFiltered int `json:"total_filtered_count"`
	} `json:"page"`
}

type listUsersResponse struct {
	Data []datadogUser `json:"data"`
	Meta datadogMeta   `json:"meta"`
}

func (p *Provider) setAuthHeaders(req *http.Request) {
	req.Header.Set("DD-API-KEY", p.apiKey)
	req.Header.Set("DD-APPLICATION-KEY", p.appKey)
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	pageNumber := 0
	pageSize := 100

	for {
		endpoint := fmt.Sprintf("%s/api/v2/users?page[size]=%d&page[number]=%s",
			p.baseURL, pageSize, strconv.Itoa(pageNumber))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		p.setAuthHeaders(req)

		resp, err := p.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("datadog: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("datadog: API error (status %d): %s", resp.StatusCode, body)
		}

		var result listUsersResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("datadog: decode response: %w", err)
		}

		for _, u := range result.Data {
			status := "active"
			if u.Attributes.Disabled {
				status = "suspended"
			} else if u.Attributes.Status == "Pending" {
				status = "invited"
			}
			all = append(all, core.User{
				Email:       u.Attributes.Email,
				DisplayName: u.Attributes.Name,
				Role:        u.Attributes.Role,
				Status:      status,
				ProviderID:  u.ID,
			})
		}

		if len(result.Data) < pageSize {
			break
		}
		pageNumber++
	}

	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("datadog: add user not supported via API")
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
		return fmt.Errorf("datadog: user %s not found", email)
	}

	endpoint := fmt.Sprintf("%s/api/v2/users/%s", p.baseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	p.setAuthHeaders(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("datadog: remove user failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("datadog: set role not supported via API")
}
