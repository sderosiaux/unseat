package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/credentials"
	"github.com/sderosiaux/unseat/internal/store"
)

// loadConfig reads the config file and fills provider credentials from the
// store written by `providers add`.
//
// Every command must go through this rather than config.Load directly:
// loading the raw file alone yields a config whose api_key fields are still
// unresolved, which is how `providers add` ended up writing tokens nothing
// ever read.
func loadConfig() (*config.Config, error) {
	if err := config.LoadDotEnv(envFile); err != nil {
		return nil, err
	}

	cfg, err := config.Load(configFile)
	if err != nil {
		// A missing config file at the default path is not an error: `scan`
		// and `providers` are meant to work off stored credentials alone,
		// before any YAML exists. An explicitly requested path must still fail.
		explicit := rootCmd.PersistentFlags().Changed("config")
		if !explicit && errors.Is(err, os.ErrNotExist) {
			cfg = &config.Config{Policies: config.Policies{DryRun: true}}
		} else {
			return nil, fmt.Errorf("load config: %w", err)
		}
	}

	credStore := credentials.NewFileStore(credentials.DefaultPath())
	if err := credStore.InjectIntoConfig(cfg); err != nil {
		return nil, fmt.Errorf("load stored credentials: %w", err)
	}

	return cfg, nil
}

// openStore opens the local SQLite cache at the configured path.
func openStore() (*store.SQLite, error) {
	db, err := store.NewSQLite(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db %s: %w", dbPath, err)
	}
	return db, nil
}

// requireIdentitySource fails early, and legibly, when a command that needs the
// directory has none configured. Letting it through produced
// "unknown identity provider: " with an empty name, which tells the operator
// nothing about what to do next.
func requireIdentitySource(cfg *config.Config) error {
	if cfg.IdentitySource.Provider != "" {
		return nil
	}
	return fmt.Errorf(
		"no identity source configured in %s — this command compares SaaS seats against your directory.\n"+
			"Add an `identity_source:` section, or run `unseat scan` for a read-only audit that needs no directory",
		configFile)
}

// configuredProviders returns the provider names that have a usable credential,
// which is what "configured" should mean — not "has been synced at least once".
func configuredProviders(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Providers))
	for name, pc := range cfg.Providers {
		if pc.APIKey != "" {
			names = append(names, name)
		}
	}
	return names
}
