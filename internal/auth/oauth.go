package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"golang.org/x/oauth2"
)

// OAuthResult holds the tokens returned from a successful OAuth2 flow.
type OAuthResult struct {
	AccessToken  string
	RefreshToken string
	TokenExpiry  time.Time
}

// RunOAuthFlow starts a temporary HTTP server, opens the browser for authorization,
// waits for the callback, and exchanges the authorization code for tokens.
func RunOAuthFlow(ctx context.Context, cfg ProviderAuth, clientID, clientSecret string) (*OAuthResult, error) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURL := fmt.Sprintf("http://localhost:%d/callback", port)

	state, err := randomState()
	if err != nil {
		listener.Close()
		return nil, err
	}

	oauthCfg := buildOAuthConfig(cfg, clientID, clientSecret, redirectURL)

	resultCh := make(chan *OAuthResult, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", makeCallbackHandler(ctx, oauthCfg, state, resultCh, errCh))

	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	defer server.Shutdown(context.Background())

	authURL := oauthCfg.AuthCodeURL(state)
	fmt.Printf("Opening browser for authorization...\n")
	fmt.Printf("If the browser doesn't open, visit:\n%s\n\n", authURL)
	openBrowser(authURL)

	fmt.Printf("Waiting for callback on http://localhost:%d/callback...\n", port)

	select {
	case result := <-resultCh:
		return result, nil
	case err := <-errCh:
		return nil, err
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("authorization timed out after 5 minutes")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// buildOAuthConfig creates an oauth2.Config from provider auth settings.
func buildOAuthConfig(cfg ProviderAuth, clientID, clientSecret, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  cfg.AuthURL,
			TokenURL: cfg.TokenURL,
		},
		RedirectURL: redirectURL,
		Scopes:      cfg.Scopes,
	}
}

// makeCallbackHandler returns an http.HandlerFunc that processes the OAuth2 callback.
func makeCallbackHandler(ctx context.Context, oauthCfg *oauth2.Config, state string, resultCh chan<- *OAuthResult, errCh chan<- error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			errCh <- fmt.Errorf("invalid state parameter")
			http.Error(w, "Invalid state", http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("no code in callback")
			http.Error(w, "No code received", http.StatusBadRequest)
			return
		}

		token, err := oauthCfg.Exchange(ctx, code)
		if err != nil {
			errCh <- fmt.Errorf("token exchange: %w", err)
			http.Error(w, "Token exchange failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body style="font-family:system-ui;display:flex;justify-content:center;align-items:center;height:100vh;margin:0">
			<div style="text-align:center">
				<h1>Connected!</h1>
				<p>You can close this window and return to the terminal.</p>
			</div>
		</body></html>`)

		resultCh <- &OAuthResult{
			AccessToken:  token.AccessToken,
			RefreshToken: token.RefreshToken,
			TokenExpiry:  token.Expiry,
		}
	}
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	}
	if cmd != nil {
		cmd.Start()
	}
}
