# CLAUDE.md

## What This Is

unseat — Identity Lifecycle Management tool. Go binary that cross-references Google Workspace with SaaS providers (Linear, Figma, Slack, Anthropic, HubSpot, Miro, etc.) to automate provisioning/deprovisioning and seat optimization.

## Architecture

Kubernetes-style reconciliation loop:
1. **Desired state**: Google Workspace groups → YAML mappings → which users should have access to which SaaS
2. **Actual state**: Each provider's `ListUsers()` API call
3. **Diff**: `core.Reconcile()` computes to_add / to_remove
4. **Execute**: Provider `AddUser()` / `RemoveUser()` with grace period, exceptions, notifications

4 interfaces: CLI (cobra), REST API (chi), MCP server (stdio), Sync Engine (cron daemon).

## Key Patterns

### Provider Interface

Every SaaS connector implements `internal/provider/provider.go`:

```go
type Provider interface {
    Name() string
    ListUsers(ctx context.Context) ([]core.User, error)
    AddUser(ctx context.Context, email string, role string) error
    RemoveUser(ctx context.Context, email string) error
    SetRole(ctx context.Context, email string, role string) error
    Capabilities() core.Capabilities
}
```

`IdentityProvider` extends `Provider` with `ListGroups()` and `ListGroupMembers()` (only Google Directory).

### Provider Construction

- All providers use `New(token, ...) *Provider` constructor + `WithBaseURL(url) *Provider` for testing
- `internal/provider/factory.go` → `BuildRegistryWithIdentity(cfg, identity)` instantiates from config
- Provider config lives in YAML `providers:` map, auth in `internal/auth/providers.go`
- Credentials stored at `~/.config/unseat/credentials.json`

### Testing Pattern

All providers tested with `httptest.NewServer` mock + `WithBaseURL()` injection. No real API calls in tests.

```go
server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    json.NewEncoder(w).Encode(mockResponse)
}))
defer server.Close()
p := provider.New("test-key").WithBaseURL(server.URL)
```

Tests use `testify` (assert/require). Store tests use `:memory:` SQLite.

### Reconciler

`internal/sync/reconciler.go` orchestrates the full flow:
- Iterates providers from config mappings
- Fetches actual users → caches in store
- Resolves desired users from identity provider groups
- Calls `core.Reconcile()` for diff
- Executes actions (or inserts pending removals if grace period)
- Sends notifications via `notify.Dispatcher`
- Logs events to store

Uses functional options: `NewReconciler(store, cfg, reg, identity, WithNotifier(d))`

### Store

`internal/store/store.go` defines the interface (11 methods). SQLite implementation in `sqlite.go` with goose migrations (`migrations/001_init.sql`). WAL mode enabled. Tables: `provider_users`, `events`, `pending_removals`, `sync_state`.

### Config

YAML at `unseat.yaml`. Key sections:
- `identity_source` — Google Directory connection
- `providers` — map of provider name → `{api_key, base_url, extra}`
- `mappings` — Google Group → provider+role assignments
- `policies` — grace_period, dry_run, notify_on_remove, exceptions

`config.GroupsForProvider(name)` and `config.IsException(email, provider)` are the main query methods.

## File Layout

```
cmd/unseat/main.go     → cli.Execute()
cli/                          Cobra commands (root, audit, sync, providers, history, serve, mcp)
config/                       YAML parsing
internal/core/                Types + reconciliation engine
internal/provider/            Provider interface, registry, factory + 9 implementations
internal/store/               Store interface + SQLite
internal/sync/                Reconciler + scheduler (daemon)
internal/notify/              Slack webhook + email notifications
internal/auth/                OAuth2 browser flow + known provider configs
internal/credentials/         File-based credential store
api/                          Chi REST server + MCP server
```

## Providers (9 total)

| Provider | Package | API Type | Notes |
|----------|---------|----------|-------|
| google-directory | provider/google | REST (Admin SDK) | Identity source, implements IdentityProvider |
| linear | provider/linear | GraphQL | RemoveUser = userSuspend mutation |
| figma | provider/figma | SCIM v2 | Enterprise only, tenant_id in extra config |
| slack | provider/slack | SCIM v2 | Business+/Enterprise Grid |
| anthropic | provider/anthropic | REST (Admin API) | x-api-key + anthropic-version headers |
| claude-code | provider/claudecode | REST (Admin API) | Same API as anthropic, filters role=claude_code_user |
| hubspot | provider/hubspot | REST (Settings v3) | RemoveUser = permanent delete |
| miro | provider/miro | REST v2 | Enterprise only, org_id in extra config |
| framer | provider/framer | — | Stub, no public API |

## Commands

```
make build          Build to bin/unseat
make test           Run all tests (-v -race)
make lint           golangci-lint
go test ./...       Quick test run
```

## Adding a New Provider

1. Create `internal/provider/<name>/<name>.go` — implement `Provider` interface
2. Create `internal/provider/<name>/<name>_test.go` — httptest mock tests
3. Add case in `internal/provider/factory.go` `BuildRegistryWithIdentity()`
4. Add entry in `internal/auth/providers.go` `KnownProviders` map
5. Add to factory test `TestBuildRegistryWithIdentity_AllProviders`
6. Update `unseat.example.yaml`

## Conventions

- All providers return `core.User` with Email, DisplayName, Role, Status, ProviderID
- Status is "active" or "suspended"
- Unsupported operations return `fmt.Errorf("<provider>: <operation> not supported")`
- Capabilities struct declares what a provider can do (CanAdd, CanRemove, CanSuspend, CanSetRole)
- CLI uses `--json` flag for machine output, `--config` for config path
- Notify channels are strings: `"slack:#channel"` or `"email:addr@co.com"`
- SQL column is `trigger_source` (not `trigger`, reserved word), mapped to `Trigger` in Go

## Dependencies

- cobra (CLI), chi (HTTP), go-sqlite3 + goose (storage), MCP Go SDK, testify, golang.org/x/oauth2, google API client
- No ORM — raw SQL with goose migrations
- No external runtime dependencies — single binary + SQLite file
