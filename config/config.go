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
	Group     string            `yaml:"group"`
	Providers []ProviderMapping `yaml:"providers"`
}

type ProviderMapping struct {
	Name string `yaml:"name"`
	Role string `yaml:"role"`
}

type Policies struct {
	GracePeriod    time.Duration `yaml:"grace_period"`
	DryRun         bool          `yaml:"dry_run"`
	NotifyOnRemove bool          `yaml:"notify_on_remove"`
	NotifyChannels []string      `yaml:"notify_channels"`
	Exceptions     []Exception   `yaml:"exceptions"`
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
