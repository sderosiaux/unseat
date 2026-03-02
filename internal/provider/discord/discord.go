package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const defaultBaseURL = "https://discord.com"

type Provider struct {
	token   string
	guildID string
	baseURL string
	client  *http.Client
}

func New(token, guildID string) *Provider {
	return &Provider{token: token, guildID: guildID, baseURL: defaultBaseURL, client: &http.Client{}}
}

func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	return p
}

func (p *Provider) Name() string { return "discord" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     false,
		CanRemove:  true,
		CanSuspend: false,
		CanSetRole: false,
	}
}

type discordUser struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Discriminator string `json:"discriminator"`
	GlobalName    string `json:"global_name"`
	Email         string `json:"email"`
}

type discordMember struct {
	User   discordUser `json:"user"`
	Nick   string      `json:"nick"`
	Roles  []string    `json:"roles"`
	Joined string      `json:"joined_at"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var all []discordMember
	after := "0"

	for {
		url := fmt.Sprintf("%s/api/v10/guilds/%s/members?limit=1000&after=%s", p.baseURL, p.guildID, after)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bot "+p.token)

		resp, err := p.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("discord: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("discord: API error (status %d): %s", resp.StatusCode, body)
		}

		var members []discordMember
		if err := json.Unmarshal(body, &members); err != nil {
			return nil, fmt.Errorf("discord: decode response: %w", err)
		}

		all = append(all, members...)

		if len(members) < 1000 {
			break
		}
		after = members[len(members)-1].User.ID
	}

	users := make([]core.User, 0, len(all))
	for _, m := range all {
		displayName := m.Nick
		if displayName == "" {
			displayName = m.User.GlobalName
		}
		if displayName == "" {
			displayName = m.User.Username
		}
		users = append(users, core.User{
			Email:       m.User.Email,
			DisplayName: displayName,
			Role:        "member",
			Status:      "active",
			ProviderID:  m.User.ID,
		})
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("discord: programmatic user invites not supported")
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
		return fmt.Errorf("discord: user %s not found", email)
	}

	url := fmt.Sprintf("%s/api/v10/guilds/%s/members/%s", p.baseURL, p.guildID, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord: kick member failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("discord: role changes not supported")
}
