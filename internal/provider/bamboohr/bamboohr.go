package bamboohr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.bamboohr.com"

type Provider struct {
	apiKey    string
	subdomain string
	baseURL   string
	client    *http.Client
}

func New(apiKey, subdomain string) *Provider {
	return &Provider{apiKey: apiKey, subdomain: subdomain, baseURL: defaultBaseURL, client: &http.Client{}}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "bamboohr" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type directoryEmployee struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	WorkEmail   string `json:"workEmail"`
	JobTitle    string `json:"jobTitle"`
	Department  string `json:"department"`
	Status      string `json:"status,omitempty"`
}

type directoryResponse struct {
	Employees []directoryEmployee `json:"employees"`
}

func (p *Provider) setBasicAuth(req *http.Request) {
	req.SetBasicAuth(p.apiKey, "x")
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	url := fmt.Sprintf("%s/api/gateway.php/%s/v1/employees/directory", p.baseURL, p.subdomain)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	p.setBasicAuth(req)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("bamboohr: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bamboohr: API error (status %d): %s", resp.StatusCode, body)
	}

	var result directoryResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("bamboohr: decode response: %w", err)
	}

	users := make([]core.User, 0, len(result.Employees))
	for _, e := range result.Employees {
		users = append(users, core.User{
			Email:       e.WorkEmail,
			DisplayName: e.DisplayName,
			Role:        "member",
			Status:      "active",
			ProviderID:  e.ID,
		})
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("bamboohr: programmatic user creation not supported")
}

func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	users, err := p.ListUsers(ctx)
	if err != nil {
		return err
	}

	var employeeID string
	for _, u := range users {
		if u.Email == email {
			employeeID = u.ProviderID
			break
		}
	}
	if employeeID == "" {
		return fmt.Errorf("bamboohr: user %s not found", email)
	}

	url := fmt.Sprintf("%s/api/gateway.php/%s/v1/employees/%s", p.baseURL, p.subdomain, employeeID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	p.setBasicAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("bamboohr: terminate employee failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("bamboohr: role changes not supported")
}
