package deel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://api.deel.com"

type Provider struct {
	token   string
	baseURL string
	client  *http.Client
}

func New(token string) *Provider {
	return &Provider{token: token, baseURL: defaultBaseURL, client: &http.Client{}}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "deel" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type deelPerson struct {
	ID        string `json:"id"`
	FullName  string `json:"full_name"`
	Email     string `json:"email"`
	WorkEmail string `json:"work_email"`
	State     string `json:"state"`
}

type deelListResponse struct {
	Data []deelPerson `json:"data"`
	Page deelPage     `json:"page"`
}

type deelPage struct {
	Offset     int `json:"offset"`
	Limit      int `json:"limit"`
	TotalRows  int `json:"total_rows"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []deelPerson
	offset := 0
	limit := 100

	for {
		url := fmt.Sprintf("%s/rest/v2/people?limit=%d&offset=%d", p.baseURL, limit, offset)
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
			return nil, fmt.Errorf("deel: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("deel: API error (status %d): %s", resp.StatusCode, body)
		}

		var result deelListResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("deel: decode response: %w", err)
		}

		all = append(all, result.Data...)

		if offset+len(result.Data) >= result.Page.TotalRows {
			break
		}
		offset += len(result.Data)
	}

	users := make([]core.User, 0, len(all))
	for _, person := range all {
		email := person.WorkEmail
		if email == "" {
			email = person.Email
		}
		status := normalizeDeelState(person.State)
		users = append(users, core.User{
			Email:       email,
			DisplayName: person.FullName,
			Role:        "member",
			Status:      status,
			ProviderID:  person.ID,
		})
	}
	return users, nil
}

func normalizeDeelState(state string) string {
	switch state {
	case "active":
		return "active"
	case "invited", "onboarding":
		return "invited"
	default:
		return "suspended"
	}
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("deel: programmatic user creation not supported")
}

func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	users, err := p.ListUsers(ctx)
	if err != nil {
		return err
	}

	var personID string
	for _, u := range users {
		if u.Email == email {
			personID = u.ProviderID
			break
		}
	}
	if personID == "" {
		return fmt.Errorf("deel: user %s not found", email)
	}

	url := fmt.Sprintf("%s/rest/v2/people/%s/terminate", p.baseURL, personID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("deel: terminate user failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("deel: role changes not supported")
}
