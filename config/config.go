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
	Aliases        map[string][]string       `yaml:"aliases,omitempty"`
	// Currency labels the cost_per_seat amounts. Purely cosmetic — unseat does
	// no conversion, so every cost_per_seat must be in the same currency.
	Currency string `yaml:"currency,omitempty"`
}

// CurrencyLabel returns the configured currency, defaulting to EUR.
func (c *Config) CurrencyLabel() string {
	if c.Currency == "" {
		return "EUR"
	}
	return c.Currency
}

type IdentitySource struct {
	Provider        string `yaml:"provider"`
	Domain          string `yaml:"domain"`
	CredentialsFile string `yaml:"credentials_file"`
	AdminEmail      string `yaml:"admin_email"`
}

type ProviderConfig struct {
	APIKey    string            `yaml:"api_key"`
	BaseURL   string            `yaml:"base_url,omitempty"`
	ExtraArgs map[string]string `yaml:"extra,omitempty"`
	// CostPerSeat is the monthly price of one seat, in Currency.
	// Zero means unpriced: counts are still reported, money is not.
	CostPerSeat float64 `yaml:"cost_per_seat,omitempty"`
	// MonthlySpend is what this provider actually costs per month, taken from
	// an invoice. unseat divides it by the active seat count to get the
	// effective rate, so negotiated discounts are included and the rate stays
	// correct as headcount moves. CostPerSeat wins when both are set.
	MonthlySpend float64 `yaml:"monthly_spend,omitempty"`
	// BillsSuspendedSeats overrides the connector's own knowledge of whether
	// deactivated seats keep being charged. Nil means "use what the connector
	// declares"; set it when your contract differs from the vendor's default.
	BillsSuspendedSeats *bool `yaml:"bills_suspended_seats,omitempty"`
}

// SuspendedBillingFor resolves the billing behaviour for a provider's
// deactivated seats: an explicit config value wins, otherwise the connector's
// own declaration stands.
func SuspendedBillingOverride(pc ProviderConfig) (billed bool, set bool) {
	if pc.BillsSuspendedSeats == nil {
		return false, false
	}
	return *pc.BillsSuspendedSeats, true
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

	expanded, err := ExpandEnv(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	cfg := Config{
		Policies: Policies{DryRun: true},
	}
	if err := yaml.Unmarshal(expanded, &cfg); err != nil {
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
