package credentials

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sderosiaux/unseat/config"
)

var ErrNotFound = errors.New("credential not found")

type CredentialType string

const (
	CredentialOAuth2 CredentialType = "oauth2"
	CredentialAPIKey CredentialType = "api_key"
)

type Credential struct {
	Provider     string         `json:"provider"`
	Type         CredentialType `json:"type"`
	AccessToken  string         `json:"access_token,omitempty"`
	RefreshToken string         `json:"refresh_token,omitempty"`
	TokenExpiry  time.Time      `json:"token_expiry,omitempty"`
	APIKey       string         `json:"api_key,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type Store interface {
	Get(provider string) (*Credential, error)
	Set(cred Credential) error
	Delete(provider string) error
	List() ([]Credential, error)
}

type FileStore struct {
	path string
	mu   sync.RWMutex
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "unseat", "credentials.json")
}

func (s *FileStore) load() (map[string]Credential, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return make(map[string]Credential), nil
		}
		return nil, fmt.Errorf("read credentials: %w", err)
	}
	if len(data) == 0 {
		return make(map[string]Credential), nil
	}
	var creds map[string]Credential
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	return creds, nil
}

func (s *FileStore) save(creds map[string]Credential) error {
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("create credentials dir: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0600); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}
	return nil
}

func (s *FileStore) Get(provider string) (*Credential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	creds, err := s.load()
	if err != nil {
		return nil, err
	}
	cred, ok := creds[provider]
	if !ok {
		return nil, ErrNotFound
	}
	return &cred, nil
}

func (s *FileStore) Set(cred Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	creds, err := s.load()
	if err != nil {
		return err
	}

	now := time.Now().Truncate(time.Second)
	if existing, ok := creds[cred.Provider]; ok {
		// Upsert: preserve original CreatedAt
		if cred.CreatedAt.IsZero() {
			cred.CreatedAt = existing.CreatedAt
		}
	}
	if cred.CreatedAt.IsZero() {
		cred.CreatedAt = now
	}
	cred.UpdatedAt = now

	creds[cred.Provider] = cred
	return s.save(creds)
}

func (s *FileStore) Delete(provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	creds, err := s.load()
	if err != nil {
		return err
	}
	delete(creds, provider)
	return s.save(creds)
}

func (s *FileStore) List() ([]Credential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	creds, err := s.load()
	if err != nil {
		return nil, err
	}
	result := make([]Credential, 0, len(creds))
	for _, c := range creds {
		c.AccessToken = mask(c.AccessToken)
		c.RefreshToken = mask(c.RefreshToken)
		c.APIKey = mask(c.APIKey)
		result = append(result, c)
	}
	return result, nil
}

// mask returns a partially redacted version of a secret string.
// Shows the first 4 characters followed by "****" if long enough,
// otherwise replaces entirely with "****".
func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "****"
}

func (s *FileStore) InjectIntoConfig(cfg *config.Config) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	creds, err := s.load()
	if err != nil {
		return err
	}
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]config.ProviderConfig)
	}
	for _, c := range creds {
		existing := cfg.Providers[c.Provider]
		switch c.Type {
		case CredentialOAuth2:
			existing.APIKey = c.AccessToken
		case CredentialAPIKey:
			existing.APIKey = c.APIKey
		}
		cfg.Providers[c.Provider] = existing
	}
	return nil
}
