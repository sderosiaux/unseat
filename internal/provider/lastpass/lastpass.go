package lastpass

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://lastpass.com"

type Provider struct {
	cid             string
	provisioningHash string
	baseURL         string
	client          *http.Client
}

func New(cid, provisioningHash string) *Provider {
	return &Provider{
		cid:             cid,
		provisioningHash: provisioningHash,
		baseURL:         defaultBaseURL,
		client:          &http.Client{},
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
	UserName  string `json:"username"`
	FullName  string `json:"fullname"`
	Admin     bool   `json:"admin"`
	Disabled  bool   `json:"disabled"`
	Invited   bool   `json:"invited"`
}

type getUserDataResponse struct {
	Users map[string]userData `json:"Users"`
	Total int                 `json:"total"`
}

func (p *Provider) doRequest(ctx context.Context, cmd string, data any) ([]byte, error) {
	payload := apiRequest{
		CID:      p.cid,
		ProvHash: p.provisioningHash,
		Cmd:      cmd,
		Data:     data,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("lastpass: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/enterpriseapi.php", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("lastpass: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lastpass: API error (status %d): %s", resp.StatusCode, respBody)
	}

	return respBody, nil
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	respBody, err := p.doRequest(ctx, "getuserdata", nil)
	if err != nil {
		return nil, err
	}

	var result getUserDataResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("lastpass: decode response: %w", err)
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
	respBody, err := p.doRequest(ctx, "deluser", data)
	if err != nil {
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
