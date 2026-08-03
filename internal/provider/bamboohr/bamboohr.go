package bamboohr

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://api.bamboohr.com"

type Provider struct {
	apiKey    string
	subdomain string
	baseURL   string
	client    *httpclient.Client
}

func New(apiKey, subdomain string) *Provider {
	return &Provider{apiKey: apiKey, subdomain: subdomain, baseURL: defaultBaseURL, client: httpclient.New()}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "bamboohr" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{}
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

	var result directoryResponse
	if err := p.client.DoJSON(ctx, "bamboohr", req, &result); err != nil {
		return nil, err
	}

	users := make([]core.User, 0, len(result.Employees))
	for _, e := range result.Employees {
		users = append(users, core.User{
			Email:       e.WorkEmail,
			DisplayName: e.DisplayName,
			Role:        "member",
			Status:      normalizeEmployeeStatus(e.Status),
			ProviderID:  e.ID,
		})
	}
	return users, nil
}

func normalizeEmployeeStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "inactive", "terminated":
		return core.StatusSuspended
	case "", "active", "current", "employed":
		return core.StatusActive
	default:
		return core.StatusActive
	}
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("bamboohr: programmatic user creation not supported")
}

func (p *Provider) RemoveUser(_ context.Context, _ string) error {
	return fmt.Errorf("bamboohr: removal is not supported; treat BambooHR as a read-only HR identity source, not a SaaS seat target")
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("bamboohr: role changes not supported")
}
