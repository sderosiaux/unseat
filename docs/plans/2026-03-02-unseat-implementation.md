# unseat Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build an Identity Lifecycle Management tool that cross-references Google Workspace with SaaS providers to automate provisioning/deprovisioning.

**Architecture:** Full custom Go binary. Kubernetes-style reconciliation loop comparing desired state (Google Groups) vs actual state (SaaS seats). 4 interfaces: CLI, Web API, MCP Server, Sync Engine.

**Tech Stack:** Go 1.25, Cobra (CLI), Chi (HTTP), Goose+sqlc (DB), SQLite/Postgres, Official MCP Go SDK (github.com/modelcontextprotocol/go-sdk v1.2.0), Google Admin Directory API.

**Design doc:** `docs/plans/2026-03-02-unseat-design.md`

---

## Task 1: Project Scaffold

**Files:**
- Create: `go.mod`
- Create: `cmd/unseat/main.go`
- Create: `Makefile`
- Create: `.gitignore`

**Step 1: Initialize Go module**

Run: `go mod init github.com/sderosiaux/unseat`

**Step 2: Create entry point**

```go
// cmd/unseat/main.go
package main

import (
	"fmt"
	"os"

	"github.com/sderosiaux/unseat/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

**Step 3: Create root CLI command**

```go
// cli/root.go
package cli

import (
	"github.com/spf13/cobra"
)

var (
	jsonOutput bool
	configFile string
)

var rootCmd = &cobra.Command{
	Use:   "unseat",
	Short: "Identity Lifecycle Management across SaaS providers",
	Long:  "Cross-reference Google Workspace with SaaS providers to automate provisioning, deprovisioning, and seat optimization.",
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "unseat.yaml", "Config file path")
}

func Execute() error {
	return rootCmd.Execute()
}
```

**Step 4: Create Makefile**

```makefile
# Makefile
BINARY := unseat
PKG := github.com/sderosiaux/unseat
VERSION := 0.1.0

.PHONY: build test lint clean

build:
	go build -o bin/$(BINARY) ./cmd/unseat

test:
	go test ./... -v -race

lint:
	golangci-lint run

clean:
	rm -rf bin/
```

**Step 5: Create .gitignore**

```
bin/
*.db
*.sqlite
unseat.yaml
.env
```

**Step 6: Install Cobra dependency and verify build**

Run: `go get github.com/spf13/cobra && go build ./cmd/unseat`
Expected: binary compiles without errors

**Step 7: Run it**

Run: `go run ./cmd/unseat --help`
Expected: shows help text with `--json` and `--config` flags

**Step 8: Init git and commit**

```bash
git init
git add .
git commit -m "feat: project scaffold with cobra CLI root command"
```

---

## Task 2: Core Types

**Files:**
- Create: `internal/core/types.go`
- Create: `internal/core/types_test.go`

**Step 1: Write tests for core types**

```go
// internal/core/types_test.go
package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUserKey(t *testing.T) {
	u := User{Email: "Alice@Company.com", DisplayName: "Alice"}
	assert.Equal(t, "alice@company.com", u.Key())
}

func TestCapabilities(t *testing.T) {
	c := Capabilities{CanAdd: true, CanRemove: true}
	assert.True(t, c.CanAdd)
	assert.True(t, c.CanRemove)
	assert.False(t, c.CanSuspend)
}

func TestEventType(t *testing.T) {
	assert.Equal(t, EventType("user_added"), EventUserAdded)
	assert.Equal(t, EventType("user_removed"), EventUserRemoved)
}

func TestDiffResult(t *testing.T) {
	desired := []User{
		{Email: "alice@co.com"},
		{Email: "bob@co.com"},
	}
	actual := []User{
		{Email: "bob@co.com"},
		{Email: "charlie@co.com"},
	}
	diff := ComputeDiff(desired, actual)
	assert.Len(t, diff.ToAdd, 1)
	assert.Equal(t, "alice@co.com", diff.ToAdd[0].Email)
	assert.Len(t, diff.ToRemove, 1)
	assert.Equal(t, "charlie@co.com", diff.ToRemove[0].Email)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/core/ -v`
Expected: FAIL — types not defined yet

**Step 3: Implement core types**

```go
// internal/core/types.go
package core

import (
	"strings"
	"time"
)

type User struct {
	Email       string            `json:"email"`
	DisplayName string            `json:"display_name"`
	Role        string            `json:"role"`
	Status      string            `json:"status"` // active, suspended, invited
	ProviderID  string            `json:"provider_id"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

func (u User) Key() string {
	return strings.ToLower(u.Email)
}

type Group struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MemberCount int    `json:"member_count"`
}

type Capabilities struct {
	CanAdd       bool `json:"can_add"`
	CanRemove    bool `json:"can_remove"`
	CanSuspend   bool `json:"can_suspend"`
	CanSetRole   bool `json:"can_set_role"`
	CanListRoles bool `json:"can_list_roles"`
	HasWebhook   bool `json:"has_webhook"`
}

type EventType string

const (
	EventUserAdded     EventType = "user_added"
	EventUserRemoved   EventType = "user_removed"
	EventUserSuspended EventType = "user_suspended"
	EventSyncCompleted EventType = "sync_completed"
)

type Event struct {
	ID         string    `json:"id"`
	Type       EventType `json:"type"`
	Provider   string    `json:"provider"`
	Email      string    `json:"email,omitempty"`
	Details    string    `json:"details,omitempty"`
	Trigger    string    `json:"trigger"` // agent, human, cron
	OccurredAt time.Time `json:"occurred_at"`
}

type DiffResult struct {
	ToAdd    []User `json:"to_add"`
	ToRemove []User `json:"to_remove"`
}

func ComputeDiff(desired, actual []User) DiffResult {
	desiredMap := make(map[string]User, len(desired))
	for _, u := range desired {
		desiredMap[u.Key()] = u
	}
	actualMap := make(map[string]User, len(actual))
	for _, u := range actual {
		actualMap[u.Key()] = u
	}

	var toAdd []User
	for key, u := range desiredMap {
		if _, exists := actualMap[key]; !exists {
			toAdd = append(toAdd, u)
		}
	}

	var toRemove []User
	for key, u := range actualMap {
		if _, exists := desiredMap[key]; !exists {
			toRemove = append(toRemove, u)
		}
	}

	return DiffResult{ToAdd: toAdd, ToRemove: toRemove}
}
```

**Step 4: Run tests**

Run: `go get github.com/stretchr/testify && go test ./internal/core/ -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add internal/core/
git commit -m "feat: core types — User, Group, Event, Capabilities, ComputeDiff"
```

---

## Task 3: Provider Interface & Registry

**Files:**
- Create: `internal/provider/provider.go`
- Create: `internal/provider/registry.go`
- Create: `internal/provider/registry_test.go`

**Step 1: Write tests for registry**

```go
// internal/provider/registry_test.go
package provider

import (
	"context"
	"testing"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mock provider for testing
type mockProvider struct {
	name  string
	users []core.User
}

func (m *mockProvider) Name() string                                           { return m.name }
func (m *mockProvider) ListUsers(_ context.Context) ([]core.User, error)       { return m.users, nil }
func (m *mockProvider) AddUser(_ context.Context, _ string, _ string) error    { return nil }
func (m *mockProvider) RemoveUser(_ context.Context, _ string) error           { return nil }
func (m *mockProvider) SetRole(_ context.Context, _ string, _ string) error    { return nil }
func (m *mockProvider) Capabilities() core.Capabilities                        { return core.Capabilities{CanAdd: true, CanRemove: true} }

func TestRegistryRegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	mock := &mockProvider{name: "test-provider"}

	reg.Register(mock)

	p, err := reg.Get("test-provider")
	require.NoError(t, err)
	assert.Equal(t, "test-provider", p.Name())
}

func TestRegistryGetUnknown(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.Get("nonexistent")
	assert.Error(t, err)
}

func TestRegistryList(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockProvider{name: "alpha"})
	reg.Register(&mockProvider{name: "beta"})

	names := reg.List()
	assert.Len(t, names, 2)
	assert.Contains(t, names, "alpha")
	assert.Contains(t, names, "beta")
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/provider/ -v`
Expected: FAIL

**Step 3: Implement provider interface and registry**

```go
// internal/provider/provider.go
package provider

import (
	"context"

	"github.com/sderosiaux/unseat/internal/core"
)

// Provider is the interface every SaaS connector must implement.
type Provider interface {
	Name() string
	ListUsers(ctx context.Context) ([]core.User, error)
	AddUser(ctx context.Context, email string, role string) error
	RemoveUser(ctx context.Context, email string) error
	SetRole(ctx context.Context, email string, role string) error
	Capabilities() core.Capabilities
}

// IdentityProvider extends Provider for identity sources (e.g. Google Directory).
type IdentityProvider interface {
	Provider
	ListGroups(ctx context.Context) ([]core.Group, error)
	ListGroupMembers(ctx context.Context, groupEmail string) ([]core.User, error)
}
```

```go
// internal/provider/registry.go
package provider

import (
	"fmt"
	"sort"
	"sync"
)

type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]Provider)}
}

func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

func (r *Registry) Get(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not registered", name)
	}
	return p, nil
}

func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
```

**Step 4: Run tests**

Run: `go test ./internal/provider/ -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add internal/provider/
git commit -m "feat: Provider interface and thread-safe Registry"
```

---

## Task 4: Config Layer (YAML Parsing)

**Files:**
- Create: `config/config.go`
- Create: `config/config_test.go`
- Create: `unseat.example.yaml`

**Step 1: Write config tests**

```go
// config/config_test.go
package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	yaml := `
identity_source:
  provider: google-directory
  domain: mycompany.com
  credentials_file: /path/to/creds.json

providers:
  linear:
    api_key: "${LINEAR_API_KEY}"
  figma:
    api_key: "${FIGMA_API_KEY}"

mappings:
  - group: design-team@mycompany.com
    providers:
      - name: figma
        role: editor
      - name: miro
        role: member

  - group: engineering@mycompany.com
    providers:
      - name: linear
        role: member

policies:
  grace_period: 72h
  dry_run: false
  notify_on_remove: true
  notify_channels:
    - slack:#it-ops
  exceptions:
    - email: cto@mycompany.com
      providers: ["*"]
`
	tmpFile, err := os.CreateTemp("", "unseat-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	_, err = tmpFile.WriteString(yaml)
	require.NoError(t, err)
	tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	require.NoError(t, err)

	assert.Equal(t, "google-directory", cfg.IdentitySource.Provider)
	assert.Equal(t, "mycompany.com", cfg.IdentitySource.Domain)
	assert.Len(t, cfg.Mappings, 2)
	assert.Equal(t, "design-team@mycompany.com", cfg.Mappings[0].Group)
	assert.Len(t, cfg.Mappings[0].Providers, 2)
	assert.Equal(t, "figma", cfg.Mappings[0].Providers[0].Name)
	assert.Equal(t, "editor", cfg.Mappings[0].Providers[0].Role)
	assert.Equal(t, 72*time.Hour, cfg.Policies.GracePeriod)
	assert.False(t, cfg.Policies.DryRun)
	assert.Len(t, cfg.Policies.Exceptions, 1)
	assert.Equal(t, "cto@mycompany.com", cfg.Policies.Exceptions[0].Email)
}

func TestLoadConfigInvalid(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "bad-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString("not: [valid: yaml: {{")
	tmpFile.Close()

	_, err = Load(tmpFile.Name())
	assert.Error(t, err)
}

func TestDesiredUsersForProvider(t *testing.T) {
	cfg := &Config{
		Mappings: []Mapping{
			{
				Group: "design@co.com",
				Providers: []ProviderMapping{
					{Name: "figma", Role: "editor"},
				},
			},
			{
				Group: "eng@co.com",
				Providers: []ProviderMapping{
					{Name: "figma", Role: "viewer"},
					{Name: "linear", Role: "member"},
				},
			},
		},
	}

	groups := cfg.GroupsForProvider("figma")
	assert.Len(t, groups, 2)
	assert.Equal(t, "design@co.com", groups[0].Group)
	assert.Equal(t, "eng@co.com", groups[1].Group)

	groups = cfg.GroupsForProvider("linear")
	assert.Len(t, groups, 1)

	groups = cfg.GroupsForProvider("unknown")
	assert.Len(t, groups, 0)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./config/ -v`
Expected: FAIL

**Step 3: Implement config**

```go
// config/config.go
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	IdentitySource IdentitySource    `yaml:"identity_source"`
	Providers      map[string]ProviderConfig `yaml:"providers"`
	Mappings       []Mapping         `yaml:"mappings"`
	Policies       Policies          `yaml:"policies"`
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
```

**Step 4: Install yaml dep and run tests**

Run: `go get gopkg.in/yaml.v3 && go test ./config/ -v`
Expected: all PASS

**Step 5: Create example config file**

```yaml
# unseat.example.yaml
identity_source:
  provider: google-directory
  domain: mycompany.com
  credentials_file: ./credentials.json

providers:
  linear:
    api_key: "${LINEAR_API_KEY}"
  figma:
    api_key: "${FIGMA_API_KEY}"
  hubspot:
    api_key: "${HUBSPOT_API_KEY}"

mappings:
  - group: design-team@mycompany.com
    providers:
      - name: figma
        role: editor
      - name: miro
        role: member
      - name: framer
        role: editor

  - group: engineering@mycompany.com
    providers:
      - name: linear
        role: member
      - name: figma
        role: viewer

  - group: sales@mycompany.com
    providers:
      - name: hubspot
        role: sales-rep

policies:
  grace_period: 72h
  dry_run: true
  notify_on_remove: true
  notify_channels:
    - slack:#it-ops
    - email:admin@mycompany.com
  exceptions:
    - email: cto@mycompany.com
      providers: ["*"]
```

**Step 6: Commit**

```bash
git add config/ unseat.example.yaml
git commit -m "feat: YAML config parsing — mappings, policies, exceptions"
```

---

## Task 5: Storage Layer (Schema + SQLite)

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/sqlite.go`
- Create: `internal/store/sqlite_test.go`
- Create: `internal/store/migrations/001_init.sql`

**Step 1: Write the migration SQL**

```sql
-- internal/store/migrations/001_init.sql
-- +goose Up

CREATE TABLE provider_users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT NOT NULL,
    email TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    provider_id TEXT NOT NULL DEFAULT '',
    synced_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(provider, email)
);

CREATE TABLE events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    type TEXT NOT NULL,
    provider TEXT NOT NULL,
    email TEXT NOT NULL DEFAULT '',
    details TEXT NOT NULL DEFAULT '',
    trigger TEXT NOT NULL DEFAULT 'system',
    occurred_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_events_provider ON events(provider);
CREATE INDEX idx_events_type ON events(type);
CREATE INDEX idx_events_occurred_at ON events(occurred_at);

CREATE TABLE pending_removals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT NOT NULL,
    email TEXT NOT NULL,
    detected_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    cancelled BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE(provider, email)
);

CREATE TABLE sync_state (
    provider TEXT PRIMARY KEY,
    last_synced_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    user_count INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'ok'
);

-- +goose Down
DROP TABLE IF EXISTS sync_state;
DROP TABLE IF EXISTS pending_removals;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS provider_users;
```

**Step 2: Write store interface and tests**

```go
// internal/store/store.go
package store

import (
	"context"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
)

type Store interface {
	// Provider users (cached state)
	UpsertProviderUsers(ctx context.Context, provider string, users []core.User) error
	GetProviderUsers(ctx context.Context, provider string) ([]core.User, error)

	// Events (history)
	InsertEvent(ctx context.Context, event core.Event) error
	ListEvents(ctx context.Context, filter EventFilter) ([]core.Event, error)

	// Pending removals
	InsertPendingRemoval(ctx context.Context, provider, email string, expiresAt time.Time) error
	GetPendingRemovals(ctx context.Context, provider string) ([]PendingRemoval, error)
	CancelPendingRemoval(ctx context.Context, provider, email string) error
	GetExpiredRemovals(ctx context.Context) ([]PendingRemoval, error)

	// Sync state
	UpdateSyncState(ctx context.Context, provider string, userCount int) error
	GetSyncState(ctx context.Context, provider string) (*SyncState, error)
	ListSyncStates(ctx context.Context) ([]SyncState, error)

	Close() error
}

type EventFilter struct {
	Provider *string
	Type     *core.EventType
	Since    *time.Time
	Limit    int
}

type PendingRemoval struct {
	Provider   string    `json:"provider"`
	Email      string    `json:"email"`
	DetectedAt time.Time `json:"detected_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Cancelled  bool      `json:"cancelled"`
}

type SyncState struct {
	Provider     string    `json:"provider"`
	LastSyncedAt time.Time `json:"last_synced_at"`
	UserCount    int       `json:"user_count"`
	Status       string    `json:"status"`
}
```

**Step 3: Write SQLite store tests**

```go
// internal/store/sqlite_test.go
package store

import (
	"context"
	"testing"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	s, err := NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUpsertAndGetProviderUsers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	users := []core.User{
		{Email: "alice@co.com", DisplayName: "Alice", Role: "editor", Status: "active"},
		{Email: "bob@co.com", DisplayName: "Bob", Role: "viewer", Status: "active"},
	}
	require.NoError(t, s.UpsertProviderUsers(ctx, "figma", users))

	got, err := s.GetProviderUsers(ctx, "figma")
	require.NoError(t, err)
	assert.Len(t, got, 2)

	// Upsert again with changes
	users[0].Role = "admin"
	require.NoError(t, s.UpsertProviderUsers(ctx, "figma", users))

	got, err = s.GetProviderUsers(ctx, "figma")
	require.NoError(t, err)
	assert.Equal(t, "admin", got[0].Role)
}

func TestInsertAndListEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	event := core.Event{
		Type:       core.EventUserAdded,
		Provider:   "linear",
		Email:      "alice@co.com",
		Trigger:    "cron",
		OccurredAt: time.Now(),
	}
	require.NoError(t, s.InsertEvent(ctx, event))

	events, err := s.ListEvents(ctx, EventFilter{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, core.EventUserAdded, events[0].Type)
}

func TestPendingRemovals(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	expires := time.Now().Add(72 * time.Hour)
	require.NoError(t, s.InsertPendingRemoval(ctx, "figma", "old@co.com", expires))

	removals, err := s.GetPendingRemovals(ctx, "figma")
	require.NoError(t, err)
	assert.Len(t, removals, 1)
	assert.Equal(t, "old@co.com", removals[0].Email)

	// Cancel it
	require.NoError(t, s.CancelPendingRemoval(ctx, "figma", "old@co.com"))
	removals, err = s.GetPendingRemovals(ctx, "figma")
	require.NoError(t, err)
	assert.Len(t, removals, 0) // cancelled ones excluded

	// No expired removals (expires in 72h)
	expired, err := s.GetExpiredRemovals(ctx)
	require.NoError(t, err)
	assert.Len(t, expired, 0)
}

func TestSyncState(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpdateSyncState(ctx, "linear", 42))

	state, err := s.GetSyncState(ctx, "linear")
	require.NoError(t, err)
	assert.Equal(t, 42, state.UserCount)
	assert.Equal(t, "ok", state.Status)

	states, err := s.ListSyncStates(ctx)
	require.NoError(t, err)
	assert.Len(t, states, 1)
}
```

**Step 4: Run tests to verify they fail**

Run: `go test ./internal/store/ -v`
Expected: FAIL

**Step 5: Implement SQLite store**

```go
// internal/store/sqlite.go
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/sderosiaux/unseat/internal/core"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/*.sql
var migrations embed.FS

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLite(dsn string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")

	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return nil, err
	}
	if err := goose.Up(db, "migrations"); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

func (s *SQLiteStore) UpsertProviderUsers(ctx context.Context, provider string, users []core.User) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Remove existing users for this provider
	if _, err := tx.ExecContext(ctx, "DELETE FROM provider_users WHERE provider = ?", provider); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx,
		"INSERT INTO provider_users (provider, email, display_name, role, status, provider_id, synced_at) VALUES (?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now()
	for _, u := range users {
		if _, err := stmt.ExecContext(ctx, provider, u.Email, u.DisplayName, u.Role, u.Status, u.ProviderID, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) GetProviderUsers(ctx context.Context, provider string) ([]core.User, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT email, display_name, role, status, provider_id FROM provider_users WHERE provider = ? ORDER BY email", provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []core.User
	for rows.Next() {
		var u core.User
		if err := rows.Scan(&u.Email, &u.DisplayName, &u.Role, &u.Status, &u.ProviderID); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *SQLiteStore) InsertEvent(ctx context.Context, event core.Event) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO events (type, provider, email, details, trigger, occurred_at) VALUES (?, ?, ?, ?, ?, ?)",
		event.Type, event.Provider, event.Email, event.Details, event.Trigger, event.OccurredAt)
	return err
}

func (s *SQLiteStore) ListEvents(ctx context.Context, filter EventFilter) ([]core.Event, error) {
	query := "SELECT type, provider, email, details, trigger, occurred_at FROM events WHERE 1=1"
	var args []any

	if filter.Provider != nil {
		query += " AND provider = ?"
		args = append(args, *filter.Provider)
	}
	if filter.Type != nil {
		query += " AND type = ?"
		args = append(args, *filter.Type)
	}
	if filter.Since != nil {
		query += " AND occurred_at >= ?"
		args = append(args, *filter.Since)
	}
	query += " ORDER BY occurred_at DESC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []core.Event
	for rows.Next() {
		var e core.Event
		if err := rows.Scan(&e.Type, &e.Provider, &e.Email, &e.Details, &e.Trigger, &e.OccurredAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *SQLiteStore) InsertPendingRemoval(ctx context.Context, provider, email string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT OR REPLACE INTO pending_removals (provider, email, detected_at, expires_at, cancelled) VALUES (?, ?, ?, ?, FALSE)",
		provider, email, time.Now(), expiresAt)
	return err
}

func (s *SQLiteStore) GetPendingRemovals(ctx context.Context, provider string) ([]PendingRemoval, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT provider, email, detected_at, expires_at FROM pending_removals WHERE provider = ? AND cancelled = FALSE", provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var removals []PendingRemoval
	for rows.Next() {
		var r PendingRemoval
		if err := rows.Scan(&r.Provider, &r.Email, &r.DetectedAt, &r.ExpiresAt); err != nil {
			return nil, err
		}
		removals = append(removals, r)
	}
	return removals, rows.Err()
}

func (s *SQLiteStore) CancelPendingRemoval(ctx context.Context, provider, email string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE pending_removals SET cancelled = TRUE WHERE provider = ? AND email = ?", provider, email)
	return err
}

func (s *SQLiteStore) GetExpiredRemovals(ctx context.Context) ([]PendingRemoval, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT provider, email, detected_at, expires_at FROM pending_removals WHERE cancelled = FALSE AND expires_at <= ?", time.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var removals []PendingRemoval
	for rows.Next() {
		var r PendingRemoval
		if err := rows.Scan(&r.Provider, &r.Email, &r.DetectedAt, &r.ExpiresAt); err != nil {
			return nil, err
		}
		removals = append(removals, r)
	}
	return removals, rows.Err()
}

func (s *SQLiteStore) UpdateSyncState(ctx context.Context, provider string, userCount int) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sync_state (provider, last_synced_at, user_count, status) VALUES (?, ?, ?, 'ok')
		 ON CONFLICT(provider) DO UPDATE SET last_synced_at = excluded.last_synced_at, user_count = excluded.user_count, status = excluded.status`,
		provider, time.Now(), userCount)
	return err
}

func (s *SQLiteStore) GetSyncState(ctx context.Context, provider string) (*SyncState, error) {
	var ss SyncState
	err := s.db.QueryRowContext(ctx,
		"SELECT provider, last_synced_at, user_count, status FROM sync_state WHERE provider = ?", provider).
		Scan(&ss.Provider, &ss.LastSyncedAt, &ss.UserCount, &ss.Status)
	if err != nil {
		return nil, err
	}
	return &ss, nil
}

func (s *SQLiteStore) ListSyncStates(ctx context.Context) ([]SyncState, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT provider, last_synced_at, user_count, status FROM sync_state ORDER BY provider")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var states []SyncState
	for rows.Next() {
		var ss SyncState
		if err := rows.Scan(&ss.Provider, &ss.LastSyncedAt, &ss.UserCount, &ss.Status); err != nil {
			return nil, err
		}
		states = append(states, ss)
	}
	return states, rows.Err()
}
```

**Step 6: Install deps and run tests**

Run: `go get github.com/pressly/goose/v3 github.com/mattn/go-sqlite3 && go test ./internal/store/ -v`
Expected: all PASS

**Step 7: Commit**

```bash
git add internal/store/
git commit -m "feat: storage layer — SQLite with goose migrations, full Store interface"
```

---

## Task 6: Reconciliation Engine

**Files:**
- Create: `internal/core/engine.go`
- Create: `internal/core/engine_test.go`

**Step 1: Write engine tests**

```go
// internal/core/engine_test.go
package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mock identity provider
type mockIdentity struct {
	groups  map[string][]User
}

func (m *mockIdentity) ResolveDesiredUsers(ctx context.Context, groupEmail string) ([]User, error) {
	return m.groups[groupEmail], nil
}

// mock target provider
type mockTarget struct {
	name  string
	users []User
	added   []string
	removed []string
}

func (m *mockTarget) Name() string                                        { return m.name }
func (m *mockTarget) ListUsers(_ context.Context) ([]User, error)        { return m.users, nil }
func (m *mockTarget) AddUser(_ context.Context, email, role string) error { m.added = append(m.added, email); return nil }
func (m *mockTarget) RemoveUser(_ context.Context, email string) error    { m.removed = append(m.removed, email); return nil }
func (m *mockTarget) SetRole(_ context.Context, _, _ string) error        { return nil }
func (m *mockTarget) Capabilities() Capabilities                          { return Capabilities{CanAdd: true, CanRemove: true} }

func TestReconcile(t *testing.T) {
	identity := &mockIdentity{
		groups: map[string][]User{
			"design@co.com": {
				{Email: "alice@co.com"},
				{Email: "bob@co.com"},
			},
		},
	}

	target := &mockTarget{
		name: "figma",
		users: []User{
			{Email: "bob@co.com"},
			{Email: "charlie@co.com"},
		},
	}

	plan, err := Reconcile(context.Background(), ReconcileInput{
		ProviderName: "figma",
		GroupMappings: []GroupMappingInput{
			{GroupEmail: "design@co.com", Role: "editor"},
		},
		DesiredResolver: identity.ResolveDesiredUsers,
		ActualUsers:     target.users,
		Exceptions:      nil,
	})
	require.NoError(t, err)

	assert.Len(t, plan.ToAdd, 1)
	assert.Equal(t, "alice@co.com", plan.ToAdd[0].Email)
	assert.Len(t, plan.ToRemove, 1)
	assert.Equal(t, "charlie@co.com", plan.ToRemove[0].Email)
}

func TestReconcileWithExceptions(t *testing.T) {
	target := &mockTarget{
		name: "figma",
		users: []User{
			{Email: "alice@co.com"},
			{Email: "cto@co.com"}, // exception
		},
	}

	plan, err := Reconcile(context.Background(), ReconcileInput{
		ProviderName: "figma",
		GroupMappings: []GroupMappingInput{
			{GroupEmail: "design@co.com", Role: "editor"},
		},
		DesiredResolver: func(_ context.Context, _ string) ([]User, error) {
			return []User{{Email: "alice@co.com"}}, nil
		},
		ActualUsers: target.users,
		Exceptions: map[string]bool{
			"cto@co.com": true,
		},
	})
	require.NoError(t, err)

	assert.Len(t, plan.ToRemove, 0) // cto excluded
}

func TestReconcileDryRun(t *testing.T) {
	plan, err := Reconcile(context.Background(), ReconcileInput{
		ProviderName: "figma",
		GroupMappings: []GroupMappingInput{
			{GroupEmail: "design@co.com", Role: "editor"},
		},
		DesiredResolver: func(_ context.Context, _ string) ([]User, error) {
			return []User{{Email: "new@co.com"}}, nil
		},
		ActualUsers: nil,
		DryRun:      true,
	})
	require.NoError(t, err)

	assert.Len(t, plan.ToAdd, 1)
	assert.True(t, plan.DryRun)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/core/ -v -run TestReconcile`
Expected: FAIL

**Step 3: Implement reconciliation engine**

```go
// internal/core/engine.go
package core

import (
	"context"
	"strings"
	"time"
)

type DesiredResolver func(ctx context.Context, groupEmail string) ([]User, error)

type GroupMappingInput struct {
	GroupEmail string
	Role       string
}

type ReconcileInput struct {
	ProviderName    string
	GroupMappings   []GroupMappingInput
	DesiredResolver DesiredResolver
	ActualUsers     []User
	Exceptions      map[string]bool // lowercased emails
	DryRun          bool
	GracePeriod     time.Duration
}

type ReconcilePlan struct {
	ProviderName string        `json:"provider"`
	ToAdd        []UserAction  `json:"to_add"`
	ToRemove     []UserAction  `json:"to_remove"`
	Unchanged    int           `json:"unchanged"`
	DryRun       bool          `json:"dry_run"`
}

type UserAction struct {
	Email string `json:"email"`
	Role  string `json:"role,omitempty"`
}

func Reconcile(ctx context.Context, input ReconcileInput) (*ReconcilePlan, error) {
	// Build desired set from all group mappings
	desiredMap := make(map[string]string) // email -> role (last wins for role)
	for _, gm := range input.GroupMappings {
		users, err := input.DesiredResolver(ctx, gm.GroupEmail)
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			key := strings.ToLower(u.Email)
			desiredMap[key] = gm.Role
		}
	}

	// Build actual set
	actualSet := make(map[string]bool, len(input.ActualUsers))
	for _, u := range input.ActualUsers {
		actualSet[strings.ToLower(u.Email)] = true
	}

	// Exceptions set
	exceptions := input.Exceptions
	if exceptions == nil {
		exceptions = make(map[string]bool)
	}

	plan := &ReconcilePlan{
		ProviderName: input.ProviderName,
		DryRun:       input.DryRun,
	}

	// To add: in desired but not in actual
	for email, role := range desiredMap {
		if !actualSet[email] {
			plan.ToAdd = append(plan.ToAdd, UserAction{Email: email, Role: role})
		}
	}

	// To remove: in actual but not in desired, minus exceptions
	for _, u := range input.ActualUsers {
		key := strings.ToLower(u.Email)
		if _, desired := desiredMap[key]; !desired && !exceptions[key] {
			plan.ToRemove = append(plan.ToRemove, UserAction{Email: key})
		}
	}

	// Unchanged count
	plan.Unchanged = len(input.ActualUsers) - len(plan.ToRemove)

	return plan, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/core/ -v`
Expected: all PASS

**Step 5: Commit**

```bash
git add internal/core/engine.go internal/core/engine_test.go
git commit -m "feat: reconciliation engine — desired vs actual diff with exceptions and dry-run"
```

---

## Task 7: CLI — Audit Commands

**Files:**
- Create: `cli/audit.go`
- Create: `cli/output.go`
- Modify: `cli/root.go` — wire audit subcommands

**Step 1: Create output helpers**

```go
// cli/output.go
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
)

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func newTabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
}

func printTable(headers []string, rows [][]string) {
	w := newTabWriter()
	for i, h := range headers {
		if i > 0 {
			fmt.Fprint(w, "\t")
		}
		fmt.Fprint(w, h)
	}
	fmt.Fprintln(w)
	for _, row := range rows {
		for i, col := range row {
			if i > 0 {
				fmt.Fprint(w, "\t")
			}
			fmt.Fprint(w, col)
		}
		fmt.Fprintln(w)
	}
	w.Flush()
}
```

**Step 2: Create audit commands**

```go
// cli/audit.go
package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/store"
	"github.com/spf13/cobra"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Inspect current state — orphans, costs, drift",
}

var auditOrphansCmd = &cobra.Command{
	Use:   "orphans",
	Short: "List seats with no matching Google Workspace user",
	RunE:  runAuditOrphans,
}

var auditDriftCmd = &cobra.Command{
	Use:   "drift",
	Short: "Show diff between desired state (groups) and actual state (SaaS)",
	RunE:  runAuditDrift,
}

func init() {
	auditCmd.AddCommand(auditOrphansCmd, auditDriftCmd)
	rootCmd.AddCommand(auditCmd)
}

func runAuditOrphans(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := store.NewSQLite("unseat.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	ctx := context.Background()
	states, err := db.ListSyncStates(ctx)
	if err != nil {
		return err
	}

	type orphan struct {
		Provider string `json:"provider"`
		Email    string `json:"email"`
		Role     string `json:"role"`
	}
	var orphans []orphan

	// For each synced provider, get cached users and check against mappings
	for _, ss := range states {
		users, err := db.GetProviderUsers(ctx, ss.Provider)
		if err != nil {
			return err
		}
		groups := cfg.GroupsForProvider(ss.Provider)
		if len(groups) == 0 {
			// No mappings = all users are technically unmapped
			for _, u := range users {
				orphans = append(orphans, orphan{Provider: ss.Provider, Email: u.Email, Role: u.Role})
			}
		}
		// Full orphan detection requires identity resolution (group members).
		// For now, show users from cache. Full reconciliation via `sync dry-run`.
		_ = groups
	}

	if jsonOutput {
		return printJSON(orphans)
	}

	if len(orphans) == 0 {
		fmt.Println("No orphans detected. Run `sync run` first to populate cache.")
		return nil
	}

	rows := make([][]string, len(orphans))
	for i, o := range orphans {
		rows[i] = []string{o.Provider, o.Email, o.Role}
	}
	printTable([]string{"PROVIDER", "EMAIL", "ROLE"}, rows)

	// Exit code 3 if orphans found
	if len(orphans) > 0 {
		os.Exit(3)
	}
	return nil
}

func runAuditDrift(cmd *cobra.Command, args []string) error {
	fmt.Println("Drift detection requires a sync. Use `sync dry-run` to preview actions.")
	return nil
}
```

**Step 3: Verify build**

Run: `go build ./cmd/unseat && ./bin/unseat audit --help`
Expected: shows audit subcommands (orphans, drift)

**Step 4: Commit**

```bash
git add cli/
git commit -m "feat: CLI audit commands — orphans and drift with --json support"
```

---

## Task 8: CLI — Providers Commands

**Files:**
- Create: `cli/providers.go`

**Step 1: Create providers commands**

```go
// cli/providers.go
package cli

import (
	"context"
	"fmt"

	"github.com/sderosiaux/unseat/internal/store"
	"github.com/spf13/cobra"
)

var providersCmd = &cobra.Command{
	Use:   "providers",
	Short: "Manage and inspect SaaS provider connections",
}

var providersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured providers and their sync status",
	RunE:  runProvidersList,
}

var providersUsersCmd = &cobra.Command{
	Use:   "users [provider]",
	Short: "List cached users for a specific provider",
	Args:  cobra.ExactArgs(1),
	RunE:  runProvidersUsers,
}

func init() {
	providersCmd.AddCommand(providersListCmd, providersUsersCmd)
	rootCmd.AddCommand(providersCmd)
}

func runProvidersList(cmd *cobra.Command, args []string) error {
	db, err := store.NewSQLite("unseat.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	states, err := db.ListSyncStates(context.Background())
	if err != nil {
		return err
	}

	if jsonOutput {
		return printJSON(states)
	}

	if len(states) == 0 {
		fmt.Println("No providers synced yet. Run `sync run` first.")
		return nil
	}

	rows := make([][]string, len(states))
	for i, s := range states {
		rows[i] = []string{s.Provider, s.LastSyncedAt.Format("2006-01-02 15:04:05"), fmt.Sprintf("%d", s.UserCount), s.Status}
	}
	printTable([]string{"PROVIDER", "LAST SYNCED", "USERS", "STATUS"}, rows)
	return nil
}

func runProvidersUsers(cmd *cobra.Command, args []string) error {
	provider := args[0]

	db, err := store.NewSQLite("unseat.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	users, err := db.GetProviderUsers(context.Background(), provider)
	if err != nil {
		return err
	}

	if jsonOutput {
		return printJSON(users)
	}

	if len(users) == 0 {
		fmt.Printf("No users cached for %s. Run `sync run` first.\n", provider)
		return nil
	}

	rows := make([][]string, len(users))
	for i, u := range users {
		rows[i] = []string{u.Email, u.DisplayName, u.Role, u.Status}
	}
	printTable([]string{"EMAIL", "NAME", "ROLE", "STATUS"}, rows)
	return nil
}
```

**Step 2: Verify build**

Run: `go build ./cmd/unseat && ./bin/unseat providers --help`
Expected: shows list, users subcommands

**Step 3: Commit**

```bash
git add cli/providers.go
git commit -m "feat: CLI providers commands — list and users with --json"
```

---

## Task 9: CLI — History Commands

**Files:**
- Create: `cli/history.go`

**Step 1: Create history commands**

```go
// cli/history.go
package cli

import (
	"context"
	"fmt"

	"github.com/sderosiaux/unseat/internal/store"
	"github.com/spf13/cobra"
)

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "View event timeline and trends",
}

var historyEventsCmd = &cobra.Command{
	Use:   "events",
	Short: "List recent events",
	RunE:  runHistoryEvents,
}

var eventsLimit int

func init() {
	historyEventsCmd.Flags().IntVar(&eventsLimit, "limit", 50, "Max events to show")
	historyCmd.AddCommand(historyEventsCmd)
	rootCmd.AddCommand(historyCmd)
}

func runHistoryEvents(cmd *cobra.Command, args []string) error {
	db, err := store.NewSQLite("unseat.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	events, err := db.ListEvents(context.Background(), store.EventFilter{Limit: eventsLimit})
	if err != nil {
		return err
	}

	if jsonOutput {
		return printJSON(events)
	}

	if len(events) == 0 {
		fmt.Println("No events recorded yet.")
		return nil
	}

	rows := make([][]string, len(events))
	for i, e := range events {
		rows[i] = []string{e.OccurredAt.Format("2006-01-02 15:04:05"), string(e.Type), e.Provider, e.Email, e.Trigger}
	}
	printTable([]string{"TIME", "TYPE", "PROVIDER", "EMAIL", "TRIGGER"}, rows)
	return nil
}
```

**Step 2: Verify build**

Run: `go build ./cmd/unseat && ./bin/unseat history --help`
Expected: shows events subcommand

**Step 3: Commit**

```bash
git add cli/history.go
git commit -m "feat: CLI history events command with --limit and --json"
```

---

## Task 10: CLI — Sync Commands (dry-run + run)

**Files:**
- Create: `cli/sync.go`

**Step 1: Create sync commands**

```go
// cli/sync.go
package cli

import (
	"context"
	"fmt"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/store"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Run reconciliation between desired and actual state",
}

var syncDryRunCmd = &cobra.Command{
	Use:   "dry-run",
	Short: "Preview sync actions without executing them",
	RunE:  runSyncDryRun,
}

var syncRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Execute one-shot reconciliation",
	RunE:  runSyncRun,
}

var autoConfirm bool

func init() {
	syncRunCmd.Flags().BoolVar(&autoConfirm, "yes", false, "Skip confirmation prompt")
	syncCmd.AddCommand(syncDryRunCmd, syncRunCmd)
	rootCmd.AddCommand(syncCmd)
}

func runSyncDryRun(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := store.NewSQLite("unseat.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	ctx := context.Background()
	_ = cfg
	_ = ctx

	fmt.Println("Dry-run mode: no actions will be taken.")
	fmt.Println("(Full sync requires provider connections. Configure providers in unseat.yaml)")
	return nil
}

func runSyncRun(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := store.NewSQLite("unseat.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	ctx := context.Background()
	_ = cfg
	_ = ctx

	fmt.Println("Sync requires provider connections. Configure providers in unseat.yaml")
	return nil
}
```

**Step 2: Verify build and full CLI**

Run: `go build -o bin/unseat ./cmd/unseat && ./bin/unseat --help`
Expected: shows all command groups (audit, sync, providers, history)

**Step 3: Commit**

```bash
git add cli/sync.go
git commit -m "feat: CLI sync commands — dry-run and run stubs"
```

---

## Task 11: Google Directory Provider

**Files:**
- Create: `internal/provider/google/google.go`
- Create: `internal/provider/google/google_test.go`

**Step 1: Write tests with mock HTTP server**

```go
// internal/provider/google/google_test.go
package google

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProviderName(t *testing.T) {
	p := &Provider{domain: "example.com"}
	assert.Equal(t, "google-directory", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := &Provider{}
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.True(t, caps.CanAdd)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/provider/google/ -v`
Expected: FAIL

**Step 3: Implement Google Directory provider**

```go
// internal/provider/google/google.go
package google

import (
	"context"
	"fmt"

	"github.com/sderosiaux/unseat/internal/core"
	admin "google.golang.org/api/admin/directory/v1"
	"google.golang.org/api/option"
)

type Provider struct {
	service *admin.Service
	domain  string
}

func New(ctx context.Context, credentialsFile, domain string) (*Provider, error) {
	svc, err := admin.NewService(ctx, option.WithCredentialsFile(credentialsFile))
	if err != nil {
		return nil, fmt.Errorf("create admin service: %w", err)
	}
	return &Provider{service: svc, domain: domain}, nil
}

func (p *Provider) Name() string { return "google-directory" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     true,
		CanRemove:  true,
		CanSuspend: true,
		CanSetRole: true,
		HasWebhook: true,
	}
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	var users []core.User
	call := p.service.Users.List().Domain(p.domain).MaxResults(500)
	err := call.Pages(ctx, func(resp *admin.Users) error {
		for _, u := range resp.Users {
			status := "active"
			if u.Suspended {
				status = "suspended"
			}
			users = append(users, core.User{
				Email:       u.PrimaryEmail,
				DisplayName: u.Name.FullName,
				Role:        boolToRole(u.IsAdmin),
				Status:      status,
				ProviderID:  u.Id,
			})
		}
		return nil
	})
	return users, err
}

func (p *Provider) AddUser(ctx context.Context, email, role string) error {
	// Google Directory: creating a user requires more info (name, password).
	// This is typically handled by Google Workspace admin, not unseat.
	return fmt.Errorf("google-directory: AddUser not supported — manage users via Google Workspace admin")
}

func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	return p.service.Users.Delete(email).Context(ctx).Do()
}

func (p *Provider) SetRole(ctx context.Context, email, role string) error {
	_, err := p.service.Users.Update(email, &admin.User{
		IsAdmin: role == "admin",
	}).Context(ctx).Do()
	return err
}

// IdentityProvider methods

func (p *Provider) ListGroups(ctx context.Context) ([]core.Group, error) {
	var groups []core.Group
	call := p.service.Groups.List().Domain(p.domain).MaxResults(200)
	err := call.Pages(ctx, func(resp *admin.Groups) error {
		for _, g := range resp.Groups {
			groups = append(groups, core.Group{
				ID:          g.Id,
				Email:       g.Email,
				Name:        g.Name,
				Description: g.Description,
				MemberCount: int(g.DirectMembersCount),
			})
		}
		return nil
	})
	return groups, err
}

func (p *Provider) ListGroupMembers(ctx context.Context, groupEmail string) ([]core.User, error) {
	var users []core.User
	call := p.service.Members.List(groupEmail).MaxResults(200)
	err := call.Pages(ctx, func(resp *admin.Members) error {
		for _, m := range resp.Members {
			if m.Type != "USER" {
				continue
			}
			users = append(users, core.User{
				Email:      m.Email,
				ProviderID: m.Id,
				Role:       m.Role,
				Status:     m.Status,
			})
		}
		return nil
	})
	return users, err
}

func boolToRole(isAdmin bool) string {
	if isAdmin {
		return "admin"
	}
	return "member"
}
```

**Step 4: Install dependency and run tests**

Run: `go get google.golang.org/api/admin/directory/v1 google.golang.org/api/option && go test ./internal/provider/google/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/provider/google/
git commit -m "feat: Google Directory provider — ListUsers, ListGroups, ListGroupMembers"
```

---

## Task 12: Linear Provider

**Files:**
- Create: `internal/provider/linear/linear.go`
- Create: `internal/provider/linear/linear_test.go`

**Step 1: Write tests**

```go
// internal/provider/linear/linear_test.go
package linear

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProviderName(t *testing.T) {
	p := &Provider{}
	assert.Equal(t, "linear", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := &Provider{}
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.False(t, caps.CanAdd) // Linear doesn't support programmatic user invite via API
}
```

**Step 2: Implement Linear provider**

Linear's API is GraphQL-based. We'll use a simple HTTP client.

```go
// internal/provider/linear/linear.go
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sderosiaux/unseat/internal/core"
)

const apiURL = "https://api.linear.app/graphql"

type Provider struct {
	apiKey string
	client *http.Client
}

func New(apiKey string) *Provider {
	return &Provider{apiKey: apiKey, client: &http.Client{}}
}

func (p *Provider) Name() string { return "linear" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{
		CanAdd:     false, // Linear doesn't support programmatic user invites
		CanRemove:  true,
		CanSuspend: true,
		CanSetRole: false,
	}
}

type gqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (p *Provider) graphql(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	body, _ := json.Marshal(gqlRequest{Query: query, Variables: variables})
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var gqlResp gqlResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", gqlResp.Errors[0].Message)
	}
	return gqlResp.Data, nil
}

func (p *Provider) ListUsers(ctx context.Context) ([]core.User, error) {
	query := `query { users { nodes { id name email active admin guest } } }`
	data, err := p.graphql(ctx, query, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Users struct {
			Nodes []struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Email  string `json:"email"`
				Active bool   `json:"active"`
				Admin  bool   `json:"admin"`
				Guest  bool   `json:"guest"`
			} `json:"nodes"`
		} `json:"users"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	users := make([]core.User, 0, len(result.Users.Nodes))
	for _, u := range result.Users.Nodes {
		status := "active"
		if !u.Active {
			status = "suspended"
		}
		role := "member"
		if u.Admin {
			role = "admin"
		} else if u.Guest {
			role = "guest"
		}
		users = append(users, core.User{
			Email:       u.Email,
			DisplayName: u.Name,
			Role:        role,
			Status:      status,
			ProviderID:  u.ID,
		})
	}
	return users, nil
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return fmt.Errorf("linear: programmatic user invites not supported via API")
}

func (p *Provider) RemoveUser(ctx context.Context, email string) error {
	// First find user by email
	users, err := p.ListUsers(ctx)
	if err != nil {
		return err
	}
	var userID string
	for _, u := range users {
		if u.Email == email {
			userID = u.ProviderID
			break
		}
	}
	if userID == "" {
		return fmt.Errorf("linear: user %s not found", email)
	}

	// Suspend the user
	query := `mutation($id: String!) { userSuspend(id: $id) { success } }`
	_, err = p.graphql(ctx, query, map[string]any{"id": userID})
	return err
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return fmt.Errorf("linear: role changes not supported via API")
}
```

**Step 3: Run tests**

Run: `go test ./internal/provider/linear/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/provider/linear/
git commit -m "feat: Linear provider — GraphQL ListUsers and RemoveUser (suspend)"
```

---

## Task 13: Web API (Chi REST Server)

**Files:**
- Create: `api/server.go`
- Create: `api/handlers.go`
- Create: `cli/serve.go`

**Step 1: Implement API server**

```go
// api/server.go
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/store"
)

type Server struct {
	store  store.Store
	config *config.Config
	router chi.Router
}

func NewServer(s store.Store, cfg *config.Config) *Server {
	srv := &Server{store: s, config: cfg}
	srv.setupRoutes()
	return srv
}

func (s *Server) setupRoutes() {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.SetHeader("Content-Type", "application/json"))

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/providers", s.handleListProviders)
		r.Get("/providers/{name}/users", s.handleProviderUsers)
		r.Get("/orphans", s.handleListOrphans)
		r.Get("/history/events", s.handleListEvents)
		r.Get("/mappings", s.handleGetMappings)
	})

	s.router = r
}

func (s *Server) Handler() http.Handler {
	return s.router
}
```

```go
// api/handlers.go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/sderosiaux/unseat/internal/store"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	states, err := s.store.ListSyncStates(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, states)
}

func (s *Server) handleProviderUsers(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	users, err := s.store.GetProviderUsers(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) handleListOrphans(w http.ResponseWriter, r *http.Request) {
	// Returns all pending removals as orphan candidates
	states, err := s.store.ListSyncStates(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type orphan struct {
		Provider string `json:"provider"`
		Email    string `json:"email"`
	}
	var orphans []orphan
	for _, ss := range states {
		removals, err := s.store.GetPendingRemovals(r.Context(), ss.Provider)
		if err != nil {
			continue
		}
		for _, rem := range removals {
			orphans = append(orphans, orphan{Provider: rem.Provider, Email: rem.Email})
		}
	}
	writeJSON(w, http.StatusOK, orphans)
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			limit = n
		}
	}
	events, err := s.store.ListEvents(r.Context(), store.EventFilter{Limit: limit})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handleGetMappings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.config.Mappings)
}
```

**Step 2: Create serve CLI command**

```go
// cli/serve.go
package cli

import (
	"fmt"
	"net/http"

	"github.com/sderosiaux/unseat/api"
	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/store"
	"github.com/spf13/cobra"
)

var servePort int

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the web API server",
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", 8080, "Port to listen on")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := store.NewSQLite("unseat.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	srv := api.NewServer(db, cfg)

	addr := fmt.Sprintf(":%d", servePort)
	fmt.Printf("Starting unseat API on %s\n", addr)
	return http.ListenAndServe(addr, srv.Handler())
}
```

**Step 3: Install chi and verify build**

Run: `go get github.com/go-chi/chi/v5 && go build -o bin/unseat ./cmd/unseat`
Expected: compiles

**Step 4: Commit**

```bash
git add api/ cli/serve.go
git commit -m "feat: REST API server with Chi — providers, users, orphans, events, mappings endpoints"
```

---

## Task 14: MCP Server

**Files:**
- Create: `api/mcp/server.go`
- Create: `cli/mcp.go`

**Step 1: Implement MCP server**

```go
// api/mcp/server.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/store"
)

type MCPServer struct {
	store  store.Store
	config *config.Config
	server *mcpsdk.Server
}

// Tool input types
type ListProvidersInput struct{}
type ListProvidersOutput struct {
	Providers []store.SyncState `json:"providers"`
}

type ProviderUsersInput struct {
	Provider string `json:"provider" jsonschema:"the name of the SaaS provider"`
}
type ProviderUsersOutput struct {
	Users json.RawMessage `json:"users"`
}

type ListOrphansInput struct{}
type ListOrphansOutput struct {
	Orphans json.RawMessage `json:"orphans"`
}

type ListEventsInput struct {
	Limit int `json:"limit" jsonschema:"maximum number of events to return"`
}
type ListEventsOutput struct {
	Events json.RawMessage `json:"events"`
}

type GetMappingsInput struct{}
type GetMappingsOutput struct {
	Mappings json.RawMessage `json:"mappings"`
}

func New(s store.Store, cfg *config.Config) *MCPServer {
	m := &MCPServer{store: s, config: cfg}
	m.server = mcpsdk.NewServer(
		&mcpsdk.Implementation{Name: "unseat", Version: "0.1.0"},
		nil,
	)
	m.registerTools()
	return m
}

func (m *MCPServer) registerTools() {
	mcpsdk.AddTool(m.server, &mcpsdk.Tool{
		Name:        "list_providers",
		Description: "List all configured SaaS providers and their sync status (last synced, user count)",
	}, m.handleListProviders)

	mcpsdk.AddTool(m.server, &mcpsdk.Tool{
		Name:        "provider_users",
		Description: "List all cached users for a specific SaaS provider",
	}, m.handleProviderUsers)

	mcpsdk.AddTool(m.server, &mcpsdk.Tool{
		Name:        "list_orphans",
		Description: "List all orphaned seats (pending removals) across all providers",
	}, m.handleListOrphans)

	mcpsdk.AddTool(m.server, &mcpsdk.Tool{
		Name:        "list_events",
		Description: "List recent events (user_added, user_removed, sync_completed)",
	}, m.handleListEvents)

	mcpsdk.AddTool(m.server, &mcpsdk.Tool{
		Name:        "get_mappings",
		Description: "Get current Google Group to SaaS provider mappings",
	}, m.handleGetMappings)
}

func (m *MCPServer) handleListProviders(ctx context.Context, req *mcpsdk.CallToolRequest, input ListProvidersInput) (*mcpsdk.CallToolResult, ListProvidersOutput, error) {
	states, err := m.store.ListSyncStates(ctx)
	if err != nil {
		return nil, ListProvidersOutput{}, err
	}
	return nil, ListProvidersOutput{Providers: states}, nil
}

func (m *MCPServer) handleProviderUsers(ctx context.Context, req *mcpsdk.CallToolRequest, input ProviderUsersInput) (*mcpsdk.CallToolResult, ProviderUsersOutput, error) {
	if input.Provider == "" {
		return nil, ProviderUsersOutput{}, fmt.Errorf("provider name is required")
	}
	users, err := m.store.GetProviderUsers(ctx, input.Provider)
	if err != nil {
		return nil, ProviderUsersOutput{}, err
	}
	data, _ := json.Marshal(users)
	return nil, ProviderUsersOutput{Users: data}, nil
}

func (m *MCPServer) handleListOrphans(ctx context.Context, req *mcpsdk.CallToolRequest, input ListOrphansInput) (*mcpsdk.CallToolResult, ListOrphansOutput, error) {
	states, err := m.store.ListSyncStates(ctx)
	if err != nil {
		return nil, ListOrphansOutput{}, err
	}

	type orphan struct {
		Provider string `json:"provider"`
		Email    string `json:"email"`
	}
	var orphans []orphan
	for _, ss := range states {
		removals, err := m.store.GetPendingRemovals(ctx, ss.Provider)
		if err != nil {
			continue
		}
		for _, rem := range removals {
			orphans = append(orphans, orphan{Provider: rem.Provider, Email: rem.Email})
		}
	}
	data, _ := json.Marshal(orphans)
	return nil, ListOrphansOutput{Orphans: data}, nil
}

func (m *MCPServer) handleListEvents(ctx context.Context, req *mcpsdk.CallToolRequest, input ListEventsInput) (*mcpsdk.CallToolResult, ListEventsOutput, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	events, err := m.store.ListEvents(ctx, store.EventFilter{Limit: limit})
	if err != nil {
		return nil, ListEventsOutput{}, err
	}
	data, _ := json.Marshal(events)
	return nil, ListEventsOutput{Events: data}, nil
}

func (m *MCPServer) handleGetMappings(ctx context.Context, req *mcpsdk.CallToolRequest, input GetMappingsInput) (*mcpsdk.CallToolResult, GetMappingsOutput, error) {
	data, _ := json.Marshal(m.config.Mappings)
	return nil, GetMappingsOutput{Mappings: data}, nil
}

func (m *MCPServer) Run(ctx context.Context) error {
	return m.server.Run(ctx, &mcpsdk.StdioTransport{})
}
```

**Step 2: Create MCP CLI command**

```go
// cli/mcp.go
package cli

import (
	"context"
	"fmt"

	mcpserver "github.com/sderosiaux/unseat/api/mcp"
	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/store"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start MCP server (stdio transport) for LLM agent integration",
	RunE:  runMCP,
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}

func runMCP(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := store.NewSQLite("unseat.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	srv := mcpserver.New(db, cfg)
	return srv.Run(context.Background())
}
```

**Step 3: Install MCP SDK and verify build**

Run: `go get github.com/modelcontextprotocol/go-sdk@v1.2.0 && go build -o bin/unseat ./cmd/unseat`
Expected: compiles

**Step 4: Commit**

```bash
git add api/mcp/ cli/mcp.go
git commit -m "feat: MCP server — list_providers, provider_users, list_orphans, list_events, get_mappings tools"
```

---

## Task 15: Integration Wiring — Full Sync Flow

**Files:**
- Create: `internal/sync/reconciler.go`
- Create: `internal/sync/reconciler_test.go`
- Modify: `cli/sync.go` — wire real reconciler

**Step 1: Write reconciler tests**

```go
// internal/sync/reconciler_test.go
package sync

import (
	"context"
	"testing"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider"
	"github.com/sderosiaux/unseat/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeIdentity struct {
	groups map[string][]core.User
}

func (f *fakeIdentity) Name() string                                        { return "fake-identity" }
func (f *fakeIdentity) ListUsers(_ context.Context) ([]core.User, error)    { return nil, nil }
func (f *fakeIdentity) AddUser(_ context.Context, _, _ string) error        { return nil }
func (f *fakeIdentity) RemoveUser(_ context.Context, _ string) error        { return nil }
func (f *fakeIdentity) SetRole(_ context.Context, _, _ string) error        { return nil }
func (f *fakeIdentity) Capabilities() core.Capabilities                     { return core.Capabilities{} }
func (f *fakeIdentity) ListGroups(_ context.Context) ([]core.Group, error)  { return nil, nil }
func (f *fakeIdentity) ListGroupMembers(_ context.Context, group string) ([]core.User, error) {
	return f.groups[group], nil
}

type fakeTarget struct {
	name    string
	users   []core.User
	added   []string
	removed []string
}

func (f *fakeTarget) Name() string                                        { return f.name }
func (f *fakeTarget) ListUsers(_ context.Context) ([]core.User, error)    { return f.users, nil }
func (f *fakeTarget) AddUser(_ context.Context, email, _ string) error    { f.added = append(f.added, email); return nil }
func (f *fakeTarget) RemoveUser(_ context.Context, email string) error    { f.removed = append(f.removed, email); return nil }
func (f *fakeTarget) SetRole(_ context.Context, _, _ string) error        { return nil }
func (f *fakeTarget) Capabilities() core.Capabilities                     { return core.Capabilities{CanAdd: true, CanRemove: true} }

func TestReconcilerFullSync(t *testing.T) {
	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"design@co.com": {{Email: "alice@co.com"}, {Email: "bob@co.com"}},
		},
	}

	target := &fakeTarget{
		name:  "figma",
		users: []core.User{{Email: "bob@co.com"}, {Email: "charlie@co.com"}},
	}

	reg := provider.NewRegistry()
	reg.Register(target)

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	defer db.Close()

	cfg := &config.Config{
		Mappings: []config.Mapping{
			{Group: "design@co.com", Providers: []config.ProviderMapping{{Name: "figma", Role: "editor"}}},
		},
		Policies: config.Policies{DryRun: false},
	}

	r := NewReconciler(db, cfg, reg, identity)
	results, err := r.Run(context.Background())
	require.NoError(t, err)

	assert.Len(t, results, 1)
	assert.Equal(t, "figma", results[0].ProviderName)
	assert.Len(t, results[0].ToAdd, 1)
	assert.Len(t, results[0].ToRemove, 1)
	assert.Contains(t, target.added, "alice@co.com")
	assert.Contains(t, target.removed, "charlie@co.com")
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/sync/ -v`
Expected: FAIL

**Step 3: Implement reconciler**

```go
// internal/sync/reconciler.go
package sync

import (
	"context"
	"log/slog"
	"time"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider"
	"github.com/sderosiaux/unseat/internal/store"
)

type Reconciler struct {
	store    store.Store
	config   *config.Config
	registry *provider.Registry
	identity provider.IdentityProvider
}

func NewReconciler(
	s store.Store,
	cfg *config.Config,
	reg *provider.Registry,
	identity provider.IdentityProvider,
) *Reconciler {
	return &Reconciler{store: s, config: cfg, registry: reg, identity: identity}
}

func (r *Reconciler) Run(ctx context.Context) ([]*core.ReconcilePlan, error) {
	// Collect all unique providers from mappings
	providerNames := make(map[string]bool)
	for _, m := range r.config.Mappings {
		for _, p := range m.Providers {
			providerNames[p.Name] = true
		}
	}

	var plans []*core.ReconcilePlan

	for name := range providerNames {
		p, err := r.registry.Get(name)
		if err != nil {
			slog.Warn("provider not registered, skipping", "provider", name, "error", err)
			continue
		}

		// Get actual users from provider
		actualUsers, err := p.ListUsers(ctx)
		if err != nil {
			slog.Error("failed to list users", "provider", name, "error", err)
			continue
		}

		// Cache the actual users
		if err := r.store.UpsertProviderUsers(ctx, name, actualUsers); err != nil {
			slog.Error("failed to cache users", "provider", name, "error", err)
		}
		r.store.UpdateSyncState(ctx, name, len(actualUsers))

		// Build group mappings for this provider
		groupMappings := r.config.GroupsForProvider(name)
		var gmInputs []core.GroupMappingInput
		for _, gm := range groupMappings {
			gmInputs = append(gmInputs, core.GroupMappingInput{GroupEmail: gm.Group, Role: gm.Role})
		}

		// Build exceptions set
		exceptions := make(map[string]bool)
		for _, ex := range r.config.Policies.Exceptions {
			for _, prov := range ex.Providers {
				if prov == "*" || prov == name {
					exceptions[ex.Email] = true
				}
			}
		}

		// Reconcile
		plan, err := core.Reconcile(ctx, core.ReconcileInput{
			ProviderName: name,
			GroupMappings: gmInputs,
			DesiredResolver: func(ctx context.Context, groupEmail string) ([]core.User, error) {
				return r.identity.ListGroupMembers(ctx, groupEmail)
			},
			ActualUsers: actualUsers,
			Exceptions:  exceptions,
			DryRun:      r.config.Policies.DryRun,
			GracePeriod: r.config.Policies.GracePeriod,
		})
		if err != nil {
			slog.Error("reconcile failed", "provider", name, "error", err)
			continue
		}

		// Execute actions (unless dry run)
		if !plan.DryRun {
			caps := p.Capabilities()
			for _, ua := range plan.ToAdd {
				if caps.CanAdd {
					if err := p.AddUser(ctx, ua.Email, ua.Role); err != nil {
						slog.Error("add user failed", "provider", name, "email", ua.Email, "error", err)
						continue
					}
					r.store.InsertEvent(ctx, core.Event{
						Type: core.EventUserAdded, Provider: name, Email: ua.Email,
						Trigger: "sync", OccurredAt: time.Now(),
					})
				}
			}
			for _, ua := range plan.ToRemove {
				if r.config.Policies.GracePeriod > 0 {
					r.store.InsertPendingRemoval(ctx, name, ua.Email, time.Now().Add(r.config.Policies.GracePeriod))
				} else if caps.CanRemove {
					if err := p.RemoveUser(ctx, ua.Email); err != nil {
						slog.Error("remove user failed", "provider", name, "email", ua.Email, "error", err)
						continue
					}
					r.store.InsertEvent(ctx, core.Event{
						Type: core.EventUserRemoved, Provider: name, Email: ua.Email,
						Trigger: "sync", OccurredAt: time.Now(),
					})
				}
			}
		}

		// Log sync completed event
		r.store.InsertEvent(ctx, core.Event{
			Type: core.EventSyncCompleted, Provider: name,
			Details: fmt.Sprintf("add=%d remove=%d unchanged=%d", len(plan.ToAdd), len(plan.ToRemove), plan.Unchanged),
			Trigger: "sync", OccurredAt: time.Now(),
		})

		plans = append(plans, plan)
	}

	return plans, nil
}
```

Note: add `import "fmt"` to the import block above.

**Step 4: Run tests**

Run: `go test ./internal/sync/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/sync/
git commit -m "feat: full reconciliation flow — sync engine wiring providers, store, and identity source"
```

---

## Task 16: End-to-End Verification

**Step 1: Run all tests**

Run: `go test ./... -v -race`
Expected: all PASS

**Step 2: Build binary**

Run: `go build -o bin/unseat ./cmd/unseat`
Expected: compiles

**Step 3: Verify all CLI commands**

Run: `./bin/unseat --help`
Expected: shows audit, sync, providers, history, serve, mcp

**Step 4: Run with example config**

Run: `cp unseat.example.yaml unseat.yaml && ./bin/unseat providers list --json`
Expected: outputs `[]` (no synced providers yet)

**Step 5: Final commit and tag**

```bash
git add -A
git commit -m "chore: tidy go.sum and verify full build"
git tag v0.1.0
```

---

## Future Tasks (not in MVP)

These are documented for later implementation:

- **Task F1**: Figma provider (`internal/provider/figma/`)
- **Task F2**: HubSpot provider (`internal/provider/hubspot/`)
- **Task F3**: Miro provider (`internal/provider/miro/`)
- **Task F4**: Framer provider (`internal/provider/framer/`)
- **Task F5**: Notification layer (Slack, email) — `internal/notify/`
- **Task F6**: Sync scheduler (cron daemon) — `internal/sync/scheduler.go`
- **Task F7**: Google Directory webhook listener — `internal/sync/webhook.go`
- **Task F8**: Dashboard (Next.js) — `web/`
- **Task F9**: Postgres store implementation — `internal/store/postgres.go`
- **Task F10**: Cost tracking per provider — price per seat config + trends
