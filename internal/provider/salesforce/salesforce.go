package salesforce

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
)

const apiVersion = "v59.0"

type Provider struct {
	token   string
	baseURL string
	client  *http.Client
}

// New creates a Salesforce provider. instanceURL is e.g. "https://mycompany.my.salesforce.com".
func New(token, instanceURL string) *Provider {
	return &Provider{token: token, baseURL: instanceURL, client: &http.Client{}}
}

func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "salesforce" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true, // deactivation
	}
}

type sfUser struct {
	ID            string `json:"Id"`
	Name          string `json:"Name"`
	Email         string `json:"Email"`
	IsActive      bool   `json:"IsActive"`
	LastLoginDate string `json:"LastLoginDate,omitempty"`
	Profile       *struct {
		Name string `json:"Name"`
	} `json:"Profile"`
}

type queryResponse struct {
	Done           bool     `json:"done"`
	TotalSize      int      `json:"totalSize"`
	Records        []sfUser `json:"records"`
	NextRecordsURL string   `json:"nextRecordsUrl"`
}

func (p *Provider) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
		return nil, fmt.Errorf("salesforce: read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("salesforce: API error %d: %s", resp.StatusCode, body)
	}
	return body, nil
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	url := fmt.Sprintf("%s/services/data/%s/query?q=SELECT+Id,Name,Email,IsActive,LastLoginDate,Profile.Name+FROM+User", p.baseURL, apiVersion)

	for {
		body, err := p.doGet(ctx, url)
		if err != nil {
			return nil, err
		}

		var resp queryResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("salesforce: decode response: %w", err)
		}

		for _, u := range resp.Records {
			role := "user"
			if u.Profile != nil && u.Profile.Name != "" {
				role = u.Profile.Name
			}
			status := "active"
			if !u.IsActive {
				status = "suspended"
			}
			user := core.User{
				Email:       u.Email,
				DisplayName: u.Name,
				Role:        role,
				Status:      status,
				ProviderID:  u.ID,
			}
			if u.LastLoginDate != "" {
				if t, err := time.Parse(time.RFC3339, u.LastLoginDate); err == nil {
					user.LastActivityAt = &t
				}
			}
			all = append(all, user)
		}

		if resp.Done || resp.NextRecordsURL == "" {
			break
		}
		url = p.baseURL + resp.NextRecordsURL
	}

	return all, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("salesforce: adding users not supported via API")
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
		return fmt.Errorf("salesforce: user %s not found", email)
	}

	payload, _ := json.Marshal(map[string]bool{"IsActive": false})
	url := fmt.Sprintf("%s/services/data/%s/sobjects/User/%s", p.baseURL, apiVersion, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("salesforce: deactivate user failed %d: %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("salesforce: setting roles not supported via API")
}
