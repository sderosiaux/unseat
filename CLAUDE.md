# CLAUDE.md

## What This Is

unseat — Identity Lifecycle Management tool. Go binary that cross-references Google Workspace with SaaS providers (Linear, Figma, Slack, Anthropic, HubSpot, Miro, etc.) to automate provisioning/deprovisioning and seat optimization.

## Product Principles

These decide design arguments. When a change would violate one, the change is
wrong — not the principle.

### 1. Connections in, value out. Configuration is a last resort.

A user supplies API keys. Everything unseat can determine for itself, it
determines for itself. Asking for a value the provider's own API could answer
is a defect, not a design choice.

Concretely: seat counts, plan tier, billed seats, subscription amounts,
activity, roles and account state all come from the APIs. Config exists for
facts no API can know — which Google Groups map to which providers, which seats
policy protects, and corrections when a contract differs from what the vendor
reports.

Before adding a config field, the question is always: can an API answer this?
If yes, call it. If only some providers can, call it for those and degrade
honestly for the rest — never demand the field from everyone because one
provider is silent.

`domain` was briefly readable only from `identity_source`, so telling an
external email from an internal one required setting up Google domain-wide
delegation. That is the failure mode this principle exists to prevent.

### 2. Degrade to reporting, never to acting.

Missing information reduces what unseat claims, never what it does. No
directory means nothing is removable. No activity API means "unknown", not
"inactive". An unpriced provider shows a dash, not 0.00.

### 3. Never state a number more precisely than it is known.

Every figure carries its provenance. A subscription amount read from a billing
API, billed-seat count read from an API, and plan metadata read from an API are
different levels of confidence and must be displayed as such. Manual prices,
public pricing page guesses and plan-name guesses are not accepted as spend.
A confident wrong number is worse than an absent one — it gets taken to a
budget meeting.

### 4. Read-only until explicitly told otherwise.

`scan` and `audit` cannot write. `sync plan` cannot write. Only `sync apply`
and `sync watch` can, and only once `dry_run` is explicitly false.

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

### Non-Human Access Is The Blind Spot Seats Cannot See

Seats are attached to people, so deprovisioning a person reaches them. Apps,
integrations, webhooks and tokens are not, so it never does. An integration
authorised under a departed engineer's OAuth grant keeps running on it; a
GitHub App installed in 2021 keeps its permissions forever. Nothing in the seat
model mentions either, which is exactly why they accumulate.

`core.Credential` models them and `core.ClassifyCredentials` judges them
against the same directory seat classification uses. Connectors implement the
optional `provider.CredentialProvider` and **report only** — whether something
is orphaned is a question about the directory, which a connector cannot see.

Two rules the classification encodes, both deliberate:

- **Dormant outranks orphaned.** A switched-off credential is settled first
  whoever created it: it cannot act, so the recommendation is to uninstall it,
  not to find it a new owner.
- **Reach is orthogonal to class.** An app installed by a current employee can
  still hold write access to every repository. Folding that into the class
  would force a choice between two facts that are both true, so
  `Overreaching` is a separate field.

`Capabilities.ReportsCredentialUsage` is the credential-side twin of
`ReportsActivity` and fails the same way. An installation's `updated_at` moves
when its permissions change, never when the app acts — reading it as usage
marks a dormant app "recently used" the day someone edits its scopes. No
connector sets the flag today, so nothing may be called unused.

What the APIs will not give, and must therefore not be claimed: GitHub names
nobody in the installations payload, so installers come from the audit log —
Enterprise-only, and bounded by retention, which attributed 3 of 31 apps on a
live org. The rest stay `unowned`, which is "GitHub will not say", not "clean".
Without SAML SSO and the fine-grained PAT approval policy, GitHub also reports
nothing at all about members' personal access tokens.

### Activity Data Is Opt-In Per Provider

`core.Capabilities.ReportsActivity` declares that `ListUsers` populates
`LastActivityAt` from a genuine usage signal. Only about a dozen of the
connectors set it — grep `ReportsActivity: true` under `internal/provider/`
rather than trusting a restated count.

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
`CredentialProvider` extends it with `ListCredentials()` for connectors whose API
exposes non-human access (github, linear).

### Billing Is API-Only

`BillingProvider` is an optional interface for connectors whose API exposes
subscription data. Implement it wherever the vendor exposes anything —
principle 1 makes a readable billing fact a fact nobody should have to type.

Linear reports the seat count it actually charges for. That count is useful
even without a price: comparing it against live accounts surfaces prepaid
blocks and plan minimums, which are invisible from the user list alone.

Accepted billing sources are provider APIs: invoice, subscription, billing
portal, or billed-seat endpoints. Rejected sources are `cost_per_seat` in YAML,
`monthly_spend` in YAML, public pricing pages, defaults, and plan-name guesses.
If the API can state seats but not spend, report the seats and mark money
unknown. Never invent a price for a plan that does not expose one — an
Enterprise contract is unknowable, and a plausible guess in a budget meeting is
worse than a blank.

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

`internal/store/store.go` defines the persistence interface. SQLite implementation
in `sqlite.go` with goose migrations under `internal/store/migrations/`. WAL mode
enabled and max open connections forced to 1 for local SQLite safety. Tables cover
provider users, events, pending removals, sync state, billing snapshots and
credential snapshots.

### Config

YAML at `unseat.yaml`. Key sections:
- `identity_source` — Google Directory connection
- `providers` — map of provider name → `{api_key, base_url, extra}` and provider selection
- no manual pricing — billing amounts come from provider APIs or stay unknown
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

`audit credentials` is the non-human counterpart to `audit seats`: it lists and
classifies installed apps, integrations and webhooks, and never revokes.

`scan` is the read-only provider entry point: provider API keys only, no identity
source and no mappings. It never writes to providers, but it does refresh local
SQLite snapshots for users, billing and non-human credentials so REST/MCP/dashboard
views do not look clean from an empty cache.

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

## Providers (see `internal/auth.KnownProviders` for the current count and full list; README has a formatted table)

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
