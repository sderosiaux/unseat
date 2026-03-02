package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRandomState(t *testing.T) {
	state1, err := randomState()
	require.NoError(t, err)
	state2, err := randomState()
	require.NoError(t, err)
	assert.NotEqual(t, state1, state2)
	assert.Len(t, state1, 32) // 16 bytes = 32 hex chars
}

func TestListKnownProviders(t *testing.T) {
	providers := ListKnownProviders()
	assert.Contains(t, providers, "figma")
	assert.Contains(t, providers, "linear")
	assert.Contains(t, providers, "hubspot")
	assert.Contains(t, providers, "miro")
	assert.Contains(t, providers, "framer")
	assert.Contains(t, providers, "google-directory")
	// Verify sorted
	for i := 1; i < len(providers); i++ {
		assert.True(t, providers[i-1] < providers[i], "providers should be sorted: %s < %s", providers[i-1], providers[i])
	}
}

func TestKnownProviderConfigs(t *testing.T) {
	for name, p := range KnownProviders {
		t.Run(name, func(t *testing.T) {
			assert.NotEmpty(t, p.Name, "missing name")
			assert.Equal(t, name, p.Name, "name mismatch with map key")
			assert.NotEmpty(t, p.AuthMethod, "missing auth method")
			assert.NotEmpty(t, p.Instructions, "missing instructions")

			if p.AuthMethod == "oauth2" {
				assert.NotEmpty(t, p.AuthURL, "oauth2 provider missing auth URL")
				assert.NotEmpty(t, p.TokenURL, "oauth2 provider missing token URL")
				assert.NotEmpty(t, p.Scopes, "oauth2 provider missing scopes")
			}
		})
	}
}

func TestRunOAuthFlowTimeout(t *testing.T) {
	// Test that the flow respects context cancellation when no callback arrives.
	cfg := ProviderAuth{
		Name:       "test-provider",
		AuthMethod: "oauth2",
		AuthURL:    "https://example.com/auth", // Never actually called
		TokenURL:   "https://example.com/token",
		Scopes:     []string{"read"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := RunOAuthFlow(ctx, cfg, "test-client-id", "test-client-secret")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

func TestOAuthCallbackHandler(t *testing.T) {
	// Test the callback handler logic directly by simulating what RunOAuthFlow sets up internally.
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "test-access-token",
			"refresh_token": "test-refresh-token",
			"token_type":    "bearer",
			"expires_in":    3600,
		})
	}))
	defer tokenServer.Close()

	cfg := ProviderAuth{
		Name:       "test",
		AuthMethod: "oauth2",
		AuthURL:    "https://example.com/auth",
		TokenURL:   tokenServer.URL,
		Scopes:     []string{"read"},
	}

	ctx := context.Background()
	state := "test-state-value"

	// Reproduce the callback handler from RunOAuthFlow
	oauthCfg := buildOAuthConfig(cfg, "test-client", "test-secret", "http://localhost:0/callback")

	resultCh := make(chan *OAuthResult, 1)
	errCh := make(chan error, 1)

	handler := makeCallbackHandler(ctx, oauthCfg, state, resultCh, errCh)

	t.Run("valid callback", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/callback?code=test-code&state=test-state-value", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		select {
		case result := <-resultCh:
			assert.Equal(t, "test-access-token", result.AccessToken)
			assert.Equal(t, "test-refresh-token", result.RefreshToken)
		case err := <-errCh:
			t.Fatalf("unexpected error: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for result")
		}
	})

	t.Run("invalid state", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/callback?code=test-code&state=wrong-state", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		select {
		case err := <-errCh:
			assert.Contains(t, err.Error(), "invalid state")
		case <-time.After(time.Second):
			t.Fatal("expected error for invalid state")
		}
	})

	t.Run("missing code", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/callback?state=test-state-value", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		select {
		case err := <-errCh:
			assert.Contains(t, err.Error(), "no code")
		case <-time.After(time.Second):
			t.Fatal("expected error for missing code")
		}
	})
}

func TestBuildAuthURL(t *testing.T) {
	cfg := ProviderAuth{
		AuthURL:  "https://example.com/oauth/authorize",
		TokenURL: "https://example.com/oauth/token",
		Scopes:   []string{"read", "write"},
	}
	oauthCfg := buildOAuthConfig(cfg, "my-client-id", "my-secret", "http://localhost:12345/callback")

	authURL := oauthCfg.AuthCodeURL("test-state")
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)

	assert.Equal(t, "https", parsed.Scheme)
	assert.Equal(t, "example.com", parsed.Host)
	assert.Equal(t, "/oauth/authorize", parsed.Path)
	assert.Equal(t, "my-client-id", parsed.Query().Get("client_id"))
	assert.Equal(t, "http://localhost:12345/callback", parsed.Query().Get("redirect_uri"))
	assert.Equal(t, "test-state", parsed.Query().Get("state"))
	assert.Contains(t, parsed.Query().Get("scope"), "read")
}
