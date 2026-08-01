package mcp

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/store"
)

// MCPServer exposes unseat capabilities over the MCP protocol.
type MCPServer struct {
	server *mcp.Server
	store  store.Store
	config *config.Config
}

// New creates an MCPServer with all tools registered.
func New(s store.Store, cfg *config.Config) *MCPServer {
	srv := &MCPServer{
		store:  s,
		config: cfg,
		server: mcp.NewServer(&mcp.Implementation{
			Name:    "unseat",
			Version: "0.1.0",
		}, nil),
	}
	srv.registerTools()
	return srv
}

// Run starts the MCP server over stdio transport, blocking until the client disconnects.
func (s *MCPServer) Run(ctx context.Context) error {
	return s.server.Run(ctx, &mcp.StdioTransport{})
}

// --- Tool input/output types ---

type emptyInput struct{}

type providerInput struct {
	Provider string `json:"provider" jsonschema:"SaaS provider name"`
}

type eventsInput struct {
	Limit int `json:"limit" jsonschema:"maximum number of events to return (default 50)"`
}

type orphanEntry struct {
	Provider string `json:"provider"`
	Email    string `json:"email"`
}

type listOrphansOutput struct {
	Orphans []orphanEntry `json:"orphans"`
}

type listProvidersOutput struct {
	Providers []store.SyncState `json:"providers"`
}

type providerUsersOutput struct {
	Users []core.User `json:"users"`
}

type listEventsOutput struct {
	Events []core.Event `json:"events"`
}

type getMappingsOutput struct {
	Mappings []config.Mapping `json:"mappings"`
}

type inactiveInput struct {
	Days int `json:"days" jsonschema:"inactivity threshold in days (default 30)"`
}

type listInactiveOutput struct {
	ThresholdDays int                  `json:"threshold_days"`
	Evaluated     []string             `json:"evaluated_providers"`
	Unevaluable   []string             `json:"unevaluable"`
	Users         []store.InactiveUser `json:"users"`
}

// --- Tool registration ---

func (s *MCPServer) registerTools() {
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "list_providers",
		Description: "List all configured SaaS providers and their sync status",
	}, s.handleListProviders)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "provider_users",
		Description: "List cached users for a specific SaaS provider",
	}, s.handleProviderUsers)

	// Named for what it actually reads. It used to be called list_orphans,
	// which invited agents to treat "empty" as "no orphaned accounts" when it
	// only ever meant "no removal is currently counting down".
	mcp.AddTool(s.server, &mcp.Tool{
		Name: "list_pending_removals",
		Description: "List seats currently inside their grace period, awaiting removal. " +
			"This is NOT the set of orphaned accounts: it is empty until a sync with a " +
			"grace period has detected departures. To find orphaned seats, run `unseat audit orphans`.",
	}, s.handleListOrphans)

	mcp.AddTool(s.server, &mcp.Tool{
		Name: "list_inactive_users",
		Description: "List users with no activity in the last N days. Only providers whose API " +
			"exposes activity data are evaluated; the others are returned in `unevaluable` and " +
			"their absence from the list means unknown, not active.",
	}, s.handleListInactive)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "list_events",
		Description: "List recent lifecycle events (additions, removals, syncs)",
	}, s.handleListEvents)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "get_mappings",
		Description: "Get current group-to-provider role mappings from config",
	}, s.handleGetMappings)
}

// --- Tool handlers ---

func (s *MCPServer) handleListProviders(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, listProvidersOutput, error) {
	states, err := s.store.ListSyncStates(ctx)
	if err != nil {
		return nil, listProvidersOutput{}, err
	}
	if states == nil {
		states = []store.SyncState{}
	}
	return nil, listProvidersOutput{Providers: states}, nil
}

func (s *MCPServer) handleProviderUsers(ctx context.Context, _ *mcp.CallToolRequest, input providerInput) (*mcp.CallToolResult, providerUsersOutput, error) {
	users, err := s.store.GetProviderUsers(ctx, input.Provider)
	if err != nil {
		return nil, providerUsersOutput{}, err
	}
	if users == nil {
		users = []core.User{}
	}
	return nil, providerUsersOutput{Users: users}, nil
}

func (s *MCPServer) handleListOrphans(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, listOrphansOutput, error) {
	states, err := s.store.ListSyncStates(ctx)
	if err != nil {
		return nil, listOrphansOutput{}, err
	}
	var orphans []orphanEntry
	for _, ss := range states {
		removals, err := s.store.GetPendingRemovals(ctx, ss.Provider)
		if err != nil {
			continue
		}
		for _, r := range removals {
			orphans = append(orphans, orphanEntry{Provider: r.Provider, Email: r.Email})
		}
	}
	if orphans == nil {
		orphans = []orphanEntry{}
	}
	return nil, listOrphansOutput{Orphans: orphans}, nil
}

func (s *MCPServer) handleListEvents(ctx context.Context, _ *mcp.CallToolRequest, input eventsInput) (*mcp.CallToolResult, listEventsOutput, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	events, err := s.store.ListEvents(ctx, store.EventFilter{Limit: limit})
	if err != nil {
		return nil, listEventsOutput{}, err
	}
	if events == nil {
		events = []core.Event{}
	}
	return nil, listEventsOutput{Events: events}, nil
}

func (s *MCPServer) handleListInactive(ctx context.Context, _ *mcp.CallToolRequest, input inactiveInput) (*mcp.CallToolResult, listInactiveOutput, error) {
	days := input.Days
	if days <= 0 {
		days = 30
	}

	// From what was observed, not recomputed: GitHub only learns whether it can
	// report activity by calling the org audit log, so a provider rebuilt from
	// config answers false and this tool would contradict the scan that filled
	// the cache it reads.
	reporting, silent, err := s.store.ActivityReportingProviders(ctx)
	if err != nil {
		return nil, listInactiveOutput{}, err
	}

	users, err := s.store.GetInactiveUsers(ctx, time.Now().AddDate(0, 0, -days), reporting)
	if err != nil {
		return nil, listInactiveOutput{}, err
	}

	out := listInactiveOutput{
		ThresholdDays: days,
		Evaluated:     reporting,
		Unevaluable:   silent,
		Users:         users,
	}
	// Empty slices rather than nil: an agent reading `null` cannot tell an
	// empty result from a missing field.
	if out.Evaluated == nil {
		out.Evaluated = []string{}
	}
	if out.Unevaluable == nil {
		out.Unevaluable = []string{}
	}
	if out.Users == nil {
		out.Users = []store.InactiveUser{}
	}
	return nil, out, nil
}

func (s *MCPServer) handleGetMappings(_ context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, getMappingsOutput, error) {
	mappings := s.config.Mappings
	if mappings == nil {
		mappings = []config.Mapping{}
	}
	return nil, getMappingsOutput{Mappings: mappings}, nil
}
