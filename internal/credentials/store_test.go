package credentials

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/sderosiaux/saas-watcher/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tempStorePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "credentials.json")
}

func TestCredentialStoreSetAndGet(t *testing.T) {
	store := NewFileStore(tempStorePath(t))

	cred := Credential{
		Provider:    "linear",
		Type:        CredentialAPIKey,
		APIKey:      "lin_api_abc123",
		CreatedAt:   time.Now().Truncate(time.Second),
		UpdatedAt:   time.Now().Truncate(time.Second),
	}

	require.NoError(t, store.Set(cred))

	got, err := store.Get("linear")
	require.NoError(t, err)

	assert.Equal(t, cred.Provider, got.Provider)
	assert.Equal(t, cred.Type, got.Type)
	assert.Equal(t, cred.APIKey, got.APIKey)
	assert.Equal(t, cred.CreatedAt.Unix(), got.CreatedAt.Unix())
}

func TestCredentialStoreGetNotFound(t *testing.T) {
	store := NewFileStore(tempStorePath(t))

	_, err := store.Get("nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestCredentialStoreDelete(t *testing.T) {
	store := NewFileStore(tempStorePath(t))

	require.NoError(t, store.Set(Credential{
		Provider: "slack",
		Type:     CredentialOAuth2,
		AccessToken: "xoxb-token",
	}))

	require.NoError(t, store.Delete("slack"))

	_, err := store.Get("slack")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestCredentialStoreList(t *testing.T) {
	store := NewFileStore(tempStorePath(t))

	require.NoError(t, store.Set(Credential{Provider: "linear", Type: CredentialAPIKey, APIKey: "key1"}))
	require.NoError(t, store.Set(Credential{Provider: "slack", Type: CredentialOAuth2, AccessToken: "tok1"}))
	require.NoError(t, store.Set(Credential{Provider: "github", Type: CredentialAPIKey, APIKey: "ghp_xyz"}))

	list, err := store.List()
	require.NoError(t, err)
	assert.Len(t, list, 3)
}

func TestCredentialStoreListMasksTokens(t *testing.T) {
	store := NewFileStore(tempStorePath(t))

	require.NoError(t, store.Set(Credential{
		Provider:     "slack",
		Type:         CredentialOAuth2,
		AccessToken:  "xoxb-super-secret-token",
		RefreshToken: "refresh-secret-123",
	}))
	require.NoError(t, store.Set(Credential{
		Provider: "linear",
		Type:     CredentialAPIKey,
		APIKey:   "lin_api_abc123def456",
	}))

	list, err := store.List()
	require.NoError(t, err)

	for _, c := range list {
		// Tokens in List() output must be masked
		assert.NotContains(t, c.AccessToken, "super-secret")
		assert.NotContains(t, c.RefreshToken, "secret-123")
		assert.NotContains(t, c.APIKey, "abc123def456")
	}
}

func TestCredentialStoreUpsert(t *testing.T) {
	store := NewFileStore(tempStorePath(t))

	require.NoError(t, store.Set(Credential{
		Provider:    "linear",
		Type:        CredentialAPIKey,
		APIKey:      "old-key",
		CreatedAt:   time.Now().Add(-time.Hour).Truncate(time.Second),
	}))

	require.NoError(t, store.Set(Credential{
		Provider: "linear",
		Type:     CredentialAPIKey,
		APIKey:   "new-key",
	}))

	got, err := store.Get("linear")
	require.NoError(t, err)
	assert.Equal(t, "new-key", got.APIKey)
	// CreatedAt should be preserved from original
	assert.False(t, got.CreatedAt.IsZero(), "CreatedAt should be preserved")
	// UpdatedAt should be recent
	assert.WithinDuration(t, time.Now(), got.UpdatedAt, 5*time.Second)
}

func TestCredentialStoreFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission test not applicable on Windows")
	}

	path := tempStorePath(t)
	store := NewFileStore(path)

	require.NoError(t, store.Set(Credential{
		Provider: "test",
		Type:     CredentialAPIKey,
		APIKey:   "secret",
	}))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestCredentialStoreEmptyFile(t *testing.T) {
	// Non-existent file should return ErrNotFound on Get, not a file error
	path := filepath.Join(t.TempDir(), "does-not-exist", "credentials.json")
	store := NewFileStore(path)

	_, err := store.Get("anything")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestCredentialStoreDeleteNonExistent(t *testing.T) {
	store := NewFileStore(tempStorePath(t))

	// Deleting a non-existent provider should not error
	err := store.Delete("ghost")
	assert.NoError(t, err)
}

func TestCredentialStoreInjectIntoConfig(t *testing.T) {
	store := NewFileStore(tempStorePath(t))

	require.NoError(t, store.Set(Credential{
		Provider:    "linear",
		Type:        CredentialAPIKey,
		APIKey:      "lin_key_123",
	}))
	require.NoError(t, store.Set(Credential{
		Provider:    "slack",
		Type:        CredentialOAuth2,
		AccessToken: "xoxb-bearer",
	}))

	cfg := &config.Config{}
	require.NoError(t, store.InjectIntoConfig(cfg))

	assert.Equal(t, "lin_key_123", cfg.Providers["linear"].APIKey)
	assert.Equal(t, "xoxb-bearer", cfg.Providers["slack"].APIKey)
}

func TestCredentialStoreInjectPreservesExisting(t *testing.T) {
	store := NewFileStore(tempStorePath(t))

	require.NoError(t, store.Set(Credential{
		Provider: "linear",
		Type:     CredentialAPIKey,
		APIKey:   "new-key",
	}))

	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"linear": {BaseURL: "https://custom.linear.app"},
		},
	}
	require.NoError(t, store.InjectIntoConfig(cfg))

	// APIKey injected, BaseURL preserved
	assert.Equal(t, "new-key", cfg.Providers["linear"].APIKey)
	assert.Equal(t, "https://custom.linear.app", cfg.Providers["linear"].BaseURL)
}
