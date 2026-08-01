package figma

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://www.figma.com/scim/v2"

// pageSize is the number of users to request per SCIM page.
const pageSize = 100

type Provider struct {
	token    string
	tenantID string
	baseURL  string
	client   *httpclient.Client
}

func New(token, tenantID string) *Provider {
	return &Provider{
		token:    token,
		tenantID: tenantID,
		baseURL:  defaultBaseURL + "/" + tenantID,
		client:   httpclient.New(),
	}
}

// WithBaseURL overrides the SCIM endpoint (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "figma" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     false,
		CanRemove:  true,
		CanSuspend: true,
		CanSetRole: false,
	}
}

// SCIM types

type scimEmail struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary"`
}

type scimUser struct {
	ID          string      `json:"id"`
	UserName    string      `json:"userName"`
	DisplayName string      `json:"displayName"`
	Active      bool        `json:"active"`
	Emails      []scimEmail `json:"emails"`
	Title       string      `json:"title,omitempty"`
}

type scimListResponse struct {
	Schemas      []string   `json:"schemas,omitempty"`
	TotalResults int        `json:"totalResults"`
	Resources    []scimUser `json:"Resources"`
}

type scimPatchOperation struct {
	Op    string         `json:"op"`
	Value map[string]any `json:"value"`
}

type scimPatchOp struct {
	Schemas    []string             `json:"schemas"`
	Operations []scimPatchOperation `json:"Operations"`
}

// doRequest issues a request with the SCIM auth headers and decodes the JSON
// response into out. Pass a nil out when the body is not needed.
func (p *Provider) doRequest(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("figma: marshal request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, reqBody)
	if err != nil {
		return err
	}
	p.decorate(req)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return p.client.DoJSON(ctx, "figma", req, out)
}

func (p *Provider) decorate(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+p.token)
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	resources, err := httpclient.ListSCIM[scimUser](ctx, p.client, httpclient.SCIMPageOptions{
		Provider: "figma",
		URL:      p.baseURL + "/Users",
		PageSize: pageSize,
		Decorate: p.decorate,
	})
	if err != nil {
		return nil, err
	}

	allUsers := make([]core.User, 0, len(resources))
	for _, u := range resources {
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
		allUsers = append(allUsers, core.User{
			Email:       email,
			DisplayName: u.DisplayName,
			Role:        "member",
			Status:      status,
			ProviderID:  u.ID,
		})
	}

	return allUsers, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("figma: programmatic user invites not supported via SCIM API")
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
		return fmt.Errorf("figma: user %s not found", email)
	}

	patch := scimPatchOp{
		Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
		Operations: []scimPatchOperation{
			{Op: "replace", Value: map[string]any{"active": false}},
		},
	}

	return p.doRequest(ctx, http.MethodPatch, "/Users/"+userID, patch, nil)
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("figma: role changes not supported via SCIM API")
}
