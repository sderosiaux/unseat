package provider

import (
	"context"
	"fmt"

	"github.com/sderosiaux/saas-watcher/config"
	"github.com/sderosiaux/saas-watcher/internal/provider/anthropic"
	"github.com/sderosiaux/saas-watcher/internal/provider/claudecode"
	"github.com/sderosiaux/saas-watcher/internal/provider/figma"
	"github.com/sderosiaux/saas-watcher/internal/provider/framer"
	googleprovider "github.com/sderosiaux/saas-watcher/internal/provider/google"
	"github.com/sderosiaux/saas-watcher/internal/provider/hubspot"
	"github.com/sderosiaux/saas-watcher/internal/provider/linear"
	"github.com/sderosiaux/saas-watcher/internal/provider/miro"
	slackprovider "github.com/sderosiaux/saas-watcher/internal/provider/slack"
)

// BuildRegistry creates a Registry and IdentityProvider from config, initializing
// real provider clients (Google Directory, Linear, etc.).
func BuildRegistry(ctx context.Context, cfg *config.Config) (*Registry, IdentityProvider, error) {
	var identity IdentityProvider
	switch cfg.IdentitySource.Provider {
	case "google-directory":
		var gopts []googleprovider.Option
		if cfg.IdentitySource.AdminEmail != "" {
			gopts = append(gopts, googleprovider.WithAdminEmail(cfg.IdentitySource.AdminEmail))
		}
		gp, err := googleprovider.New(ctx, cfg.IdentitySource.CredentialsFile, cfg.IdentitySource.Domain, gopts...)
		if err != nil {
			return nil, nil, fmt.Errorf("init google directory: %w", err)
		}
		identity = gp
	default:
		return nil, nil, fmt.Errorf("unknown identity provider: %s", cfg.IdentitySource.Provider)
	}
	return BuildRegistryWithIdentity(cfg, identity)
}

// BuildRegistryWithIdentity creates a Registry using a pre-built IdentityProvider.
// Useful for testing or when the identity source is constructed externally.
func BuildRegistryWithIdentity(cfg *config.Config, identity IdentityProvider) (*Registry, IdentityProvider, error) {
	reg := NewRegistry()
	if identity != nil {
		reg.Register(identity)
	}

	for name, pcfg := range cfg.Providers {
		switch name {
		case "linear":
			p := linear.New(pcfg.APIKey)
			if pcfg.BaseURL != "" {
				p = p.WithBaseURL(pcfg.BaseURL)
			}
			reg.Register(p)
		case "figma":
			p := figma.New(pcfg.APIKey, pcfg.ExtraArgs["tenant_id"])
			if pcfg.BaseURL != "" {
				p = p.WithBaseURL(pcfg.BaseURL)
			}
			reg.Register(p)
		case "hubspot":
			p := hubspot.New(pcfg.APIKey)
			if pcfg.BaseURL != "" {
				p = p.WithBaseURL(pcfg.BaseURL)
			}
			reg.Register(p)
		case "miro":
			p := miro.New(pcfg.APIKey, pcfg.ExtraArgs["org_id"])
			if pcfg.BaseURL != "" {
				p = p.WithBaseURL(pcfg.BaseURL)
			}
			reg.Register(p)
		case "framer":
			reg.Register(framer.New())
		case "slack":
			p := slackprovider.New(pcfg.APIKey)
			if pcfg.BaseURL != "" {
				p = p.WithBaseURL(pcfg.BaseURL)
			}
			reg.Register(p)
		case "anthropic":
			p := anthropic.New(pcfg.APIKey)
			if pcfg.BaseURL != "" {
				p = p.WithBaseURL(pcfg.BaseURL)
			}
			reg.Register(p)
		case "claude-code":
			p := claudecode.New(pcfg.APIKey)
			if pcfg.BaseURL != "" {
				p = p.WithBaseURL(pcfg.BaseURL)
			}
			reg.Register(p)
		default:
			return nil, nil, fmt.Errorf("unknown provider: %s", name)
		}
	}

	return reg, identity, nil
}
