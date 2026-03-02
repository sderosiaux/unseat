package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	IdentitySource IdentitySource            `yaml:"identity_source"`
	Providers      map[string]ProviderConfig `yaml:"providers"`
	Mappings       []Mapping                 `yaml:"mappings"`
	Policies       Policies                  `yaml:"policies"`
}

type IdentitySource struct {
	Provider        string `yaml:"provider"`
	Domain          string `yaml:"domain"`
	CredentialsFile string `yaml:"credentials_file"`
}

type ProviderConfig struct {
	APIKey    string            `yaml:"api_key"`
	BaseURL   string            `yaml:"base_url,omitempty"`
	ExtraArgs map[string]string `yaml:"extra,omitempty"`
}

type Mapping struct {
	Group     string            `yaml:"group" json:"group"`
	Providers []ProviderMapping `yaml:"providers" json:"providers"`
}

type ProviderMapping struct {
	Name string `yaml:"name" json:"name"`
	Role string `yaml:"role" json:"role"`
}

type Policies struct {
	GracePeriod    time.Duration `yaml:"grace_period"`
	SyncInterval   time.Duration `yaml:"sync_interval"`
	DryRun         bool          `yaml:"dry_run"`
	NotifyOnRemove bool          `yaml:"notify_on_remove"`
	NotifyChannels []string      `yaml:"notify_channels"`
	Exceptions     []Exception   `yaml:"exceptions"`
	Notify         NotifyConfig  `yaml:"notify"`
}

// NotifyConfig holds credentials for notification backends.
type NotifyConfig struct {
	SlackWebhookURL string `yaml:"slack_webhook_url"`
	SMTPHost        string `yaml:"smtp_host"`
	SMTPPort        int    `yaml:"smtp_port"`
	SMTPFrom        string `yaml:"smtp_from"`
	SMTPUser        string `yaml:"smtp_user"`
	SMTPPass        string `yaml:"smtp_pass"`
}

type Exception struct {
	Email     string   `yaml:"email"`
	Providers []string `yaml:"providers"`
}

type GroupMapping struct {
	Group string
	Role  string
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

func (c *Config) GroupsForProvider(providerName string) []GroupMapping {
	var result []GroupMapping
	for _, m := range c.Mappings {
		for _, p := range m.Providers {
			if p.Name == providerName {
				result = append(result, GroupMapping{Group: m.Group, Role: p.Role})
			}
		}
	}
	return result
}

func (c *Config) IsException(email string, providerName string) bool {
	for _, ex := range c.Policies.Exceptions {
		if ex.Email == email {
			for _, p := range ex.Providers {
				if p == "*" || p == providerName {
					return true
				}
			}
		}
	}
	return false
}
