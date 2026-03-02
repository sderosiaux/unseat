package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sderosiaux/saas-watcher/internal/auth"
	"github.com/sderosiaux/saas-watcher/internal/credentials"
	"github.com/spf13/cobra"
)

var (
	apiKeyFlag       string
	clientIDFlag     string
	clientSecretFlag string
)

var providersAddCmd = &cobra.Command{
	Use:   "add [provider...]",
	Short: "Connect a SaaS provider (OAuth2 browser flow or API key)",
	Long:  "Authenticate with one or more SaaS providers. Opens a browser for OAuth2 providers.",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runProvidersAdd,
}

var providersListKnownCmd = &cobra.Command{
	Use:   "supported",
	Short: "List all supported providers",
	RunE:  runProvidersListKnown,
}

func init() {
	providersAddCmd.Flags().StringVar(&apiKeyFlag, "api-key", "", "API key (for providers that use API key auth)")
	providersAddCmd.Flags().StringVar(&clientIDFlag, "client-id", "", "OAuth2 client ID (or set <PROVIDER>_CLIENT_ID env var)")
	providersAddCmd.Flags().StringVar(&clientSecretFlag, "client-secret", "", "OAuth2 client secret (or set <PROVIDER>_CLIENT_SECRET env var)")
	providersCmd.AddCommand(providersAddCmd, providersListKnownCmd)
}

func runProvidersAdd(cmd *cobra.Command, args []string) error {
	credStore := credentials.NewFileStore(credentials.DefaultPath())

	for _, name := range args {
		providerAuth, known := auth.KnownProviders[name]
		if !known {
			fmt.Fprintf(os.Stderr, "Unknown provider %q. Run `saas-watcher providers supported` to see available providers.\n", name)
			continue
		}

		fmt.Printf("\n--- Connecting %s ---\n", name)

		switch providerAuth.AuthMethod {
		case "api_key":
			if err := handleAPIKeyAuth(credStore, name, providerAuth); err != nil {
				fmt.Fprintf(os.Stderr, "Error connecting %s: %v\n", name, err)
				continue
			}
		case "oauth2":
			if err := handleOAuth2Auth(cmd.Context(), credStore, name, providerAuth); err != nil {
				fmt.Fprintf(os.Stderr, "Error connecting %s: %v\n", name, err)
				continue
			}
		}

		fmt.Printf("  %s connected successfully.\n", name)
	}

	return nil
}

func handleAPIKeyAuth(store *credentials.FileStore, name string, _ auth.ProviderAuth) error {
	key := apiKeyFlag
	if key == "" {
		envKey := strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_API_KEY"
		key = os.Getenv(envKey)
	}
	if key == "" {
		return fmt.Errorf("API key required. Use --api-key flag or set %s_API_KEY env var", strings.ToUpper(strings.ReplaceAll(name, "-", "_")))
	}

	return store.Set(credentials.Credential{
		Provider:  name,
		Type:      credentials.CredentialAPIKey,
		APIKey:    key,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
}

func handleOAuth2Auth(ctx context.Context, store *credentials.FileStore, name string, providerAuth auth.ProviderAuth) error {
	clientID := clientIDFlag
	if clientID == "" {
		envKey := strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_CLIENT_ID"
		clientID = os.Getenv(envKey)
	}
	clientSecret := clientSecretFlag
	if clientSecret == "" {
		envKey := strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_CLIENT_SECRET"
		clientSecret = os.Getenv(envKey)
	}

	if clientID == "" || clientSecret == "" {
		envPrefix := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
		return fmt.Errorf("OAuth2 requires client credentials. Set %s_CLIENT_ID and %s_CLIENT_SECRET env vars, or use --client-id and --client-secret flags.\n  %s", envPrefix, envPrefix, providerAuth.Instructions)
	}

	result, err := auth.RunOAuthFlow(ctx, providerAuth, clientID, clientSecret)
	if err != nil {
		return err
	}

	return store.Set(credentials.Credential{
		Provider:     name,
		Type:         credentials.CredentialOAuth2,
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		TokenExpiry:  result.TokenExpiry,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	})
}

func runProvidersListKnown(cmd *cobra.Command, args []string) error {
	providers := auth.ListKnownProviders()

	if jsonOutput {
		type providerInfo struct {
			Name       string `json:"name"`
			AuthMethod string `json:"auth_method"`
		}
		var infos []providerInfo
		for _, name := range providers {
			p := auth.KnownProviders[name]
			infos = append(infos, providerInfo{Name: name, AuthMethod: p.AuthMethod})
		}
		return printJSON(infos)
	}

	rows := make([][]string, len(providers))
	for i, name := range providers {
		p := auth.KnownProviders[name]
		rows[i] = []string{name, p.AuthMethod, p.Instructions}
	}
	printTable([]string{"PROVIDER", "AUTH METHOD", "INSTRUCTIONS"}, rows)
	return nil
}
