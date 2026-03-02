# saas-watcher Design Document

Identity Lifecycle Management tool. Cross-references Google Workspace (source of truth) with SaaS providers to automate user provisioning, deprovisioning, and seat optimization.

## Problem

- Paying for SaaS seats of users who left the company
- Security surface: orphaned accounts in SaaS products
- Manual onboarding/offboarding across N tools
- No visibility into who has access to what, or cost trends

## Decision Record

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Architecture | Full custom Go | Unified read+write, single binary, no external deps |
| Not Steampipe | Despite 153 plugins | Read-only, missing Miro/Figma/Framer, adds process dependency |
| Storage | SQLite (self-hosted) / Postgres (hosted) | Cache + history + config |
| Config | YAML | Versionable in git, declarative mappings |
| Sync model | Kubernetes-style reconciliation | Desired state (GWS groups) vs actual state (SaaS) = actions |
| Change detection | Hybrid (webhooks + polling) | Real-time + periodic reconciliation fallback |

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                   saas-watcher                       │
│                                                      │
│  ┌───────────┐  ┌───────────┐  ┌──────────────────┐ │
│  │  CLI      │  │  Web API  │  │  Sync Engine     │ │
│  │  (cobra)  │  │  (REST)   │  │  (cron+webhook)  │ │
│  └─────┬─────┘  └─────┬─────┘  └────────┬─────────┘ │
│        │              │                  │           │
│        │     ┌────────┴──────┐           │           │
│        │     │  MCP Server   │           │           │
│        │     └────────┬──────┘           │           │
│        ▼              ▼                  ▼           │
│  ┌──────────────────────────────────────────────┐   │
│  │              Core Engine                      │   │
│  │  ┌─────────────┐  ┌────────────────────────┐ │   │
│  │  │ Policy      │  │ Reconciliation Loop    │ │   │
│  │  │ Engine      │  │ desired(GWS) vs actual │ │   │
│  │  └─────────────┘  └────────────────────────┘ │   │
│  └──────────────────────┬───────────────────────┘   │
│  ┌──────────────────────▼───────────────────────┐   │
│  │           Provider Registry                   │   │
│  │  ┌────────┐ ┌───────┐ ┌──────┐ ┌──────────┐ │   │
│  │  │ Google │ │Linear │ │Figma │ │ HubSpot  │ │   │
│  │  │  Dir   │ │       │ │      │ │          │ │   │
│  │  └────────┘ └───────┘ └──────┘ └──────────┘ │   │
│  └──────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────┐   │
│  │  Storage (SQLite / Postgres)                  │   │
│  │  - live state cache  - history (append-only)  │   │
│  │  - config/mappings   - audit log              │   │
│  └──────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────┘
```

### 4 Interfaces

| Interface | Target | Purpose |
|-----------|--------|---------|
| CLI | Humans + LLM agents | Commands, flags, `--json` output, composable, pipeable |
| MCP Server | LLM agents (Claude, etc.) | Structured tool calls with built-in guardrails |
| Web API | Dashboard + integrations | REST JSON, webhook ingress |
| Sync Engine | Automation | Cron + push, autonomous reconciliation |

## Provider Interface

```go
type User struct {
    Email       string
    DisplayName string
    Role        string
    Status      string            // active, suspended, invited
    ProviderID  string
    Metadata    map[string]string
}

type Provider interface {
    Name() string
    ListUsers(ctx context.Context) ([]User, error)
    AddUser(ctx context.Context, email string, role string) error
    RemoveUser(ctx context.Context, email string) error
    SetRole(ctx context.Context, email string, role string) error
    Capabilities() Capabilities
}

type Capabilities struct {
    CanAdd       bool
    CanRemove    bool
    CanSuspend   bool
    CanSetRole   bool
    CanListRoles bool
    HasWebhook   bool
}

// Google Directory extends Provider with identity source capabilities
type IdentityProvider interface {
    Provider
    ListGroups(ctx context.Context) ([]Group, error)
    ListGroupMembers(ctx context.Context, groupID string) ([]User, error)
    WatchChanges(ctx context.Context, callback func(Event)) error
}
```

Providers are compiled into the binary. Adding a SaaS = implement `Provider` + register in registry + rebuild.

## Mappings & Policies

Declarative YAML config, versionable in git:

```yaml
identity_source:
  provider: google-directory
  domain: mycompany.com

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
      - name: figma
        role: viewer

policies:
  grace_period: 72h
  notify_on_remove: true
  notify_channels:
    - slack:#it-ops
    - email:admin@mycompany.com
  dry_run: false
  exceptions:
    - email: cto@mycompany.com
      providers: ["*"]
```

### Reconciliation Logic

```
For each provider P:
  desired_users = UNION(members of all groups mapped to P)
  actual_users  = P.ListUsers() (from cache)

  to_add    = desired - actual
  to_remove = actual - desired - exceptions

  For to_add:    → P.AddUser(email, role) → log event
  For to_remove: → if grace_period active and not expired → mark pending_removal
                 → if expired → P.RemoveUser(email) → log event → notify
```

## Storage Layer

```
┌─────────────────────────────────────────┐
│  Live State (cache)                     │
│  - users per provider, last_synced_at   │
│  - refreshed on sync cycle only         │
├─────────────────────────────────────────┤
│  History (append-only)                  │
│  - events: user_added, user_removed,    │
│    sync_completed                       │
│  - daily snapshots: seat_count/provider │
│  - cost tracking over time              │
├─────────────────────────────────────────┤
│  Config (mappings & policies)           │
│  - group → provider mappings            │
│  - grace periods, exceptions            │
│  - notification rules                   │
└─────────────────────────────────────────┘
```

Dashboard reads from cache (instant). APIs hit only during sync cycles.

## CLI Design (LLM-friendly)

```
saas-watcher
├── audit orphans          # List orphaned seats
├── audit costs            # Estimated cost per provider
├── audit drift            # Diff desired vs actual
├── sync run               # One-shot reconciliation
├── sync watch             # Daemon mode (cron + webhooks)
├── sync dry-run           # Preview actions without executing
├── providers list         # Configured providers + status
├── providers test         # Test connectivity
├── providers users <name> # List users of a provider
├── groups list            # Google Groups + mappings
├── groups members <id>    # Members of a group
├── history events         # Event timeline
├── history trends         # Seat count evolution
└── serve                  # Start web API + dashboard
```

All commands support `--json` for machine consumption. Exit codes: 0=ok, 1=error, 2=drift detected, 3=action needed. No interactive prompts; `--yes` to skip confirmation. No colors/spinners when stdout is not a TTY.

## MCP Server Tools

```
list_orphans    - Orphaned seats across all providers
audit_drift     - Desired vs actual state diff
provision_user  - Add user to N providers
remove_user     - Remove user from a provider
list_providers  - All configured providers + status
get_history     - Event timeline / trends
sync_now        - Trigger reconciliation
get_mappings    - Current group → provider mappings
```

Guardrails: `dry_run` by default for destructive actions, human confirmation for bulk removals (>N users), audit trail for agent vs human vs cron triggers.

## Project Structure

```
saas-watcher/
├── cmd/saas-watcher/main.go
├── internal/
│   ├── core/              # engine, policy, types
│   ├── provider/          # interfaces + registry
│   │   ├── google/
│   │   ├── linear/
│   │   ├── figma/
│   │   ├── hubspot/
│   │   ├── miro/
│   │   └── framer/
│   ├── store/             # storage interface + sqlite/postgres + migrations
│   ├── sync/              # scheduler, webhook listener, reconciler
│   └── notify/            # slack, email
├── api/
│   ├── server.go          # HTTP server
│   ├── handlers.go        # REST handlers
│   └── mcp/server.go      # MCP server
├── cli/                   # Cobra commands
├── web/                   # Dashboard (Next.js)
├── config/config.go       # YAML parsing
├── saas-watcher.example.yaml
├── go.mod
└── Makefile
```

## Target: Open-source + hosted

- Open-source core (CLI + providers + sync engine)
- Self-hosted with SQLite
- Hosted version with Postgres, managed sync, dashboard
- Community-contributed providers via PRs
