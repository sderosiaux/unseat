package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const (
	defaultBaseURL    = "https://api.github.com"
	defaultGraphQLURL = "https://api.github.com/graphql"
)

type Provider struct {
	token      string
	org        string
	baseURL    string
	graphqlURL string
	client     *http.Client
}

func New(token, org string) *Provider {
	return &Provider{token: token, org: org, baseURL: defaultBaseURL, graphqlURL: defaultGraphQLURL, client: &http.Client{}}
}

// WithBaseURL overrides the API base URL (useful for testing).
func (p *Provider) WithBaseURL(url string) *Provider {
	p.baseURL = url
	p.graphqlURL = url + "/graphql"
	return p
}

func (p *Provider) Name() string { return "github" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanRemove: true,
	}
}

type apiUser struct {
	ID    int    `json:"id"`
	Login string `json:"login"`
	Email string `json:"email"`
	Role  string `json:"role_name"`
}

type orgMember struct {
	Login   string `json:"login"`
	ID      int    `json:"id"`
	SiteURL string `json:"html_url"`
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var members []orgMember
	page := 1

	for {
		url := fmt.Sprintf("%s/orgs/%s/members?per_page=100&page=%d", p.baseURL, p.org, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+p.token)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := p.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("github: read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("github: API error (status %d): %s", resp.StatusCode, body)
		}

		var page_members []orgMember
		if err := json.Unmarshal(body, &page_members); err != nil {
			return nil, fmt.Errorf("github: decode response: %w", err)
		}

		if len(page_members) == 0 {
			break
		}
		members = append(members, page_members...)

		linkHeader := resp.Header.Get("Link")
		if !hasNextPage(linkHeader) {
			break
		}
		page++
	}

	// Try SAML identity mapping first (orgs with SSO).
	// Maps login -> corporate email (nameId).
	samlMap := p.fetchSAMLIdentities(ctx)

	// Fetch email for each member: SAML first, then /users/{login} fallback.
	all := make([]core.User, 0, len(members))
	for _, m := range members {
		var email, displayName string
		if samlEmail, ok := samlMap[m.Login]; ok {
			email = samlEmail
			displayName = m.Login
		} else {
			email, displayName = p.fetchUserEmail(ctx, m.Login)
			if displayName == "" {
				displayName = m.Login
			}
		}
		all = append(all, core.User{
			Email:       email,
			DisplayName: displayName,
			Role:        "member",
			Status:      "active",
			ProviderID:  fmt.Sprintf("%d", m.ID),
			Metadata:    map[string]string{"login": m.Login},
		})
	}

	return all, nil
}

// fetchUserEmail calls /users/{login} to get the user's public email and name.
// Falls back to {login}@users.noreply.github.com if no public email.
func (p *Provider) fetchUserEmail(ctx context.Context, login string) (email, name string) {
	url := fmt.Sprintf("%s/users/%s", p.baseURL, login)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return login, ""
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := p.client.Do(req)
	if err != nil {
		return login, ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return login, ""
	}

	var user struct {
		Email string `json:"email"`
		Name  string `json:"name"`
		Login string `json:"login"`
	}
	body, _ := io.ReadAll(resp.Body)
	if json.Unmarshal(body, &user) != nil {
		return login, ""
	}
	if user.Email == "" {
		return user.Login, user.Name
	}
	return user.Email, user.Name
}

// fetchSAMLIdentities queries GitHub's GraphQL API for SAML SSO identity mappings.
// Returns a map of GitHub login -> SAML nameId (typically the corporate email).
// Returns an empty map if the org doesn't have SSO or the query fails.
func (p *Provider) fetchSAMLIdentities(ctx context.Context) map[string]string {
	result := make(map[string]string)
	cursor := ""

	for {
		afterClause := ""
		if cursor != "" {
			afterClause = fmt.Sprintf(`, after: "%s"`, cursor)
		}
		query := fmt.Sprintf(`{
			organization(login: "%s") {
				samlIdentityProvider {
					externalIdentities(first: 100%s) {
						pageInfo { hasNextPage endCursor }
						edges {
							node {
								samlIdentity { nameId }
								user { login }
							}
						}
					}
				}
			}
		}`, p.org, afterClause)

		body, err := json.Marshal(map[string]string{"query": query})
		if err != nil {
			return result
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.graphqlURL, bytes.NewReader(body))
		if err != nil {
			return result
		}
		req.Header.Set("Authorization", "Bearer "+p.token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := p.client.Do(req)
		if err != nil {
			return result
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return result
		}

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return result
		}

		var gqlResp struct {
			Data struct {
				Organization struct {
					SAMLIdentityProvider *struct {
						ExternalIdentities struct {
							PageInfo struct {
								HasNextPage bool   `json:"hasNextPage"`
								EndCursor   string `json:"endCursor"`
							} `json:"pageInfo"`
							Edges []struct {
								Node struct {
									SAMLIdentity struct {
										NameID string `json:"nameId"`
									} `json:"samlIdentity"`
									User *struct {
										Login string `json:"login"`
									} `json:"user"`
								} `json:"node"`
							} `json:"edges"`
						} `json:"externalIdentities"`
					} `json:"samlIdentityProvider"`
				} `json:"organization"`
			} `json:"data"`
		}
		if json.Unmarshal(respBody, &gqlResp) != nil {
			return result
		}

		provider := gqlResp.Data.Organization.SAMLIdentityProvider
		if provider == nil {
			return result // org doesn't have SSO
		}

		for _, edge := range provider.ExternalIdentities.Edges {
			if edge.Node.User != nil && edge.Node.SAMLIdentity.NameID != "" {
				result[edge.Node.User.Login] = edge.Node.SAMLIdentity.NameID
			}
		}

		if !provider.ExternalIdentities.PageInfo.HasNextPage {
			break
		}
		cursor = provider.ExternalIdentities.PageInfo.EndCursor
	}

	return result
}

// hasNextPage checks if the Link header contains a rel="next" link.
func hasNextPage(link string) bool {
	if link == "" {
		return false
	}
	// Simple check: look for rel="next" in the Link header
	for _, part := range splitLinks(link) {
		if len(part) > 0 {
			for _, param := range splitLinks2(part) {
				if param == `rel="next"` {
					return true
				}
			}
		}
	}
	return false
}

func splitLinks(s string) []string {
	var parts []string
	current := ""
	for _, c := range s {
		if c == ',' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func splitLinks2(s string) []string {
	var parts []string
	current := ""
	for _, c := range s {
		if c == ';' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	// Trim spaces
	for i := range parts {
		trimmed := ""
		started := false
		end := len(parts[i])
		for j := len(parts[i]) - 1; j >= 0; j-- {
			if parts[i][j] != ' ' {
				end = j + 1
				break
			}
		}
		for j := 0; j < end; j++ {
			if parts[i][j] != ' ' {
				started = true
			}
			if started {
				trimmed += string(parts[i][j])
			}
		}
		parts[i] = trimmed
	}
	return parts
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("github: programmatic user invites not supported via API")
}

func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	users, err := p.ListUsers(ctx)
	if err != nil {
		return err
	}

	var login string
	for _, u := range users {
		if u.Email == email {
			login = u.Metadata["login"]
			break
		}
	}
	if login == "" {
		return fmt.Errorf("github: user %s not found", email)
	}

	url := fmt.Sprintf("%s/orgs/%s/members/%s", p.baseURL, p.org, login)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github: remove member failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("github: role changes not supported via API")
}
