package salesforce

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const apiVersion = "v59.0"

type Provider struct {
	token   string
	baseURL string
	client  *httpclient.Client
}

// New creates a Salesforce provider. instanceURL is e.g. "https://mycompany.my.salesforce.com".
func New(token, instanceURL string) *Provider {
	return &Provider{token: token, baseURL: instanceURL, client: httpclient.New()}
}

func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "salesforce" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true, // deactivation
		// Users carry LastLoginDate.
		ReportsActivity: true,
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

func (p *Provider) doGet(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/json")

	return p.client.DoJSON(ctx, "salesforce", req, out)
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []core.User
	url := fmt.Sprintf("%s/services/data/%s/query?q=SELECT+Id,Name,Email,IsActive,LastLoginDate,Profile.Name+FROM+User", p.baseURL, apiVersion)

	for {
		var resp queryResponse
		if err := p.doGet(ctx, url, &resp); err != nil {
			return nil, err
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
			if t, ok := parseSalesforceTime(u.LastLoginDate); ok {
				user.LastActivityAt = &t
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

// salesforceTimeFormats covers what SOQL actually returns for datetime fields.
//
// The primary form uses a numeric offset with no colon ("+0000"), which
// time.RFC3339 rejects. Parsing with RFC3339 alone silently left every
// LastActivityAt nil, and since this connector declares ReportsActivity, a nil
// value reads as "never active" — turning the whole org into high-severity
// inactive seats with a euro figure attached.
var salesforceTimeFormats = []string{
	"2006-01-02T15:04:05.000-0700",
	time.RFC3339,
	"2006-01-02T15:04:05-0700",
}

func parseSalesforceTime(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range salesforceTimeFormats {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
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

	return p.client.DoJSON(ctx, "salesforce", req, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("salesforce: setting roles not supported via API")
}
