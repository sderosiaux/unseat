package lastpass

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider/httpclient"
)

const defaultBaseURL = "https://lastpass.com"

type Provider struct {
	cid              string
	provisioningHash string
	baseURL          string
	client           *httpclient.Client
}

func New(cid, provisioningHash string) *Provider {
	return &Provider{
		cid:              cid,
		provisioningHash: provisioningHash,
		baseURL:          defaultBaseURL,
		client:           httpclient.New(),
	}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "lastpass" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     false,
		CanRemove:  true,
		CanSuspend: false,
		CanSetRole: false,
	}
}

type apiRequest struct {
	CID      string `json:"cid"`
	ProvHash string `json:"provhash"`
	Cmd      string `json:"cmd"`
	Data     any    `json:"data,omitempty"`
}

type userData struct {
	UserName string `json:"username"`
	FullName string `json:"fullname"`
	Admin    bool   `json:"admin"`
	Disabled bool   `json:"disabled"`
	Invited  bool   `json:"invited"`
}

type getUserDataResponse struct {
	Users map[string]userData `json:"Users"`
	Total int                 `json:"total"`
}

// doRequest posts an Enterprise API command and decodes the JSON reply into out.
// Pass nil for out to discard the body.
func (p *Provider) doRequest(ctx context.Context, cmd string, data any, out any) error {
	payload := apiRequest{
		CID:      p.cid,
		ProvHash: p.provisioningHash,
		Cmd:      cmd,
		Data:     data,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("lastpass: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/enterpriseapi.php", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	return p.client.DoJSON(ctx, "lastpass", req, out)
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var result getUserDataResponse
	if err := p.doRequest(ctx, "getuserdata", nil, &result); err != nil {
		return nil, err
	}

	users := make([]core.User, 0, len(result.Users))
	for id, u := range result.Users {
		status := "active"
		if u.Disabled {
			status = "suspended"
		} else if u.Invited {
			status = "invited"
		}
		role := "member"
		if u.Admin {
			role = "admin"
		}
		users = append(users, core.User{
			Email:       u.UserName,
			DisplayName: u.FullName,
			Role:        role,
			Status:      status,
			ProviderID:  id,
		})
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("lastpass: programmatic user invites not supported via Enterprise API")
}

func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	data := map[string]string{"username": email}
	// LastPass reports command failures in a 200 body, so the raw reply is kept
	// verbatim for the error message.
	var respBody json.RawMessage
	if err := p.doRequest(ctx, "deluser", data, &respBody); err != nil {
		return err
	}

	// LastPass returns a JSON status response.
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("lastpass: decode response: %w", err)
	}

	if status, ok := result["status"].(string); ok && status != "OK" {
		return fmt.Errorf("lastpass: remove user failed: %s", respBody)
	}

	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("lastpass: role changes not supported via Enterprise API")
}
