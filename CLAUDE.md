# CLAUDE.md

## What This Is

unseat — Identity Lifecycle Management tool. Go binary that cross-references Google Workspace with SaaS providers (Linear, Figma, Slack, Anthropic, HubSpot, Miro, etc.) to automate provisioning/deprovisioning and seat optimization.

## Architecture

Kubernetes-style reconciliation loop:
1. **Desired state**: Google Workspace groups → YAML mappings → which users should have access to which SaaS
2. **Actual state**: Each provider's `ListUsers()` API call
3. **Directory state**: `identity.ListUsers()` — every corporate identity and whether it is suspended
4. **Classify**: `core.ClassifySeats()` labels each seat managed / unmapped / orphan / external / unresolved
5. **Diff**: `core.Reconcile()` turns that into to_add / to_remove / to_review
6. **Execute**: Provider `AddUser()` / `RemoveUser()` with grace period, exceptions, notifications

4 interfaces: CLI (cobra), REST API (chi), MCP server (stdio), Sync Engine (cron daemon).

### Removal Is Driven By The Directory, Not By Mappings

This is the load-bearing invariant. `ToRemove` contains **only** seats whose
identity is absent from or suspended in the directory. An active employee who
is not in any mapped group is `SeatUnmapped` and goes to `ToReview` — never to
`ToRemove`, however incomplete the mappings are.

Breaking this turns an incomplete config into a mass-deprovisioning event. It is
covered by `TestReconcileNeverRemovesActiveEmployee` and
`TestReconcilerNeverRemovesActiveEmployee`; if you change classification, verify
both still fail when `SeatUnmapped` is routed to `ToRemove`.

With no directory available, nothing is removable and every seat falls to
`ToReview`. Degrade toward reporting, never toward deleting.

### Activity Data Is Opt-In Per Provider

`core.Capabilities.ReportsActivity` declares that `ListUsers` populates
`LastActivityAt` from a genuine usage signal. Only ~12 of 53 providers can.

For a provider with the flag, a nil `LastActivityAt` means "never seen active"
— the strongest inactivity signal. For every other provider it means "unknown".
Conflating them made every user of every uninstrumented provider look inactive.
`store.GetInactiveUsers` therefore takes an explicit provider list, and callers
must surface the non-reporting providers separately.

Do not set the flag off a field that merely looks temporal: `box` fills
`LastActivityAt` from `modified_at` (an admin edit) and is deliberately flagged
`false`. The rule is: does this timestamp move when the *person* uses the tool?

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
- `providers` — map of provider name → `{api_key, base_url, extra, cost_per_seat}`
- `currency` — labels every `cost_per_seat`; no conversion is performed
- `mappings` — Google Group → provider+role assignments
- `policies` — grace_period, dry_run, notify_on_remove, exceptions

`config.GroupsForProvider(name)` and `config.IsException(email, provider)` are the main query methods.

`${VAR}` references are expanded from the environment (and from `.env`) by
`config.Load`. An undefined variable is a hard error — passing the literal
through produced unexplained 401s.

**Never call `config.Load` from a CLI command.** Use `cli.loadConfig()`: it also
loads `.env` and merges the credential store from `providers add`. Skipping it is
how stored credentials ended up written but never read. Likewise use
`cli.openStore()` rather than hardcoding the database path.

The config file is optional at the default path: `scan` and `providers` must
work from stored credentials alone, before any YAML exists. An explicitly passed
`--config` that is missing is still an error.

### Commands

`sync plan` never mutates. `sync apply` shows the plan, confirms, then executes.
`policies.dry_run` is a lock that makes `apply` refuse — not a mode that makes it
silently no-op, which is what the old `sync run` did.

`scan` is the read-only entry point: provider API keys only, no identity source,
no mappings, no writes. It is what a new user should run first.

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

## Providers (55 total, see README for full list)

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
