package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
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

type decisionsInput struct {
	Provider string `json:"provider,omitempty" jsonschema:"optional provider filter"`
	Subject  string `json:"subject,omitempty" jsonschema:"optional subject filter"`
	Status   string `json:"status,omitempty" jsonschema:"optional decision status filter"`
	Limit    int    `json:"limit,omitempty" jsonschema:"maximum number of decisions to return (default 100)"`
}

type decisionInput struct {
	ID string `json:"id" jsonschema:"decision id"`
}

type certificatesInput struct {
	Subject string `json:"subject,omitempty" jsonschema:"optional subject filter"`
	Status  string `json:"status,omitempty" jsonschema:"optional certificate status filter"`
	Limit   int    `json:"limit,omitempty" jsonschema:"maximum number of certificates to return (default 50)"`
}

type certificateInput struct {
	ID string `json:"id" jsonschema:"offboarding certificate id"`
}

type decisionMutationInput struct {
	ID     string `json:"id" jsonschema:"decision id"`
	By     string `json:"by,omitempty" jsonschema:"actor recorded in the local decision ledger"`
	Reason string `json:"reason,omitempty" jsonschema:"required rejection reason for reject_decision"`
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

type listBillingOutput struct {
	Billing []core.BillingSnapshot `json:"billing"`
}

type providerBillingOutput struct {
	Provider string                `json:"provider"`
	Billing  *core.BillingSnapshot `json:"billing"`
	Reason   string                `json:"reason,omitempty"`
}

type listEventsOutput struct {
	Events []core.Event `json:"events"`
}

type listDecisionsOutput struct {
	Decisions []core.Decision `json:"decisions"`
}

type decisionOutput struct {
	Decision *core.Decision `json:"decision,omitempty"`
}

type decisionEventsOutput struct {
	Events []store.DecisionEvent `json:"events"`
}

type listCertificatesOutput struct {
	Certificates []core.OffboardingCertificate `json:"certificates"`
}

type certificateOutput struct {
	Certificate *core.OffboardingCertificate `json:"certificate,omitempty"`
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

type listCredentialsOutput struct {
	Credentials []core.ClassifiedCredential `json:"credentials"`
	SyncStates  []store.CredentialSyncState `json:"sync_states"`
}

type providerCredentialsOutput struct {
	Provider    string                      `json:"provider"`
	Credentials []core.ClassifiedCredential `json:"credentials"`
	SyncState   *store.CredentialSyncState  `json:"sync_state,omitempty"`
}

type credentialsSummaryOutput struct {
	Summary    []core.CredentialSummary    `json:"summary"`
	SyncStates []store.CredentialSyncState `json:"sync_states"`
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

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "list_billing",
		Description: "List latest cached provider billing snapshots. Billing amounts are API-only; missing money means unknown, not zero.",
	}, s.handleListBilling)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "provider_billing",
		Description: "Get the latest cached billing snapshot for one SaaS provider",
	}, s.handleProviderBilling)

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
		Name: "list_credentials",
		Description: "List cached non-human access findings: apps, integrations, webhooks, tokens, " +
			"and their classifications. Also returns credential sync states so skipped or unsupported " +
			"providers are visible and not mistaken for clean.",
	}, s.handleListCredentials)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "provider_credentials",
		Description: "List cached non-human access findings for one SaaS provider",
	}, s.handleProviderCredentials)

	mcp.AddTool(s.server, &mcp.Tool{
		Name: "credential_summary",
		Description: "Summarize cached non-human access findings by provider, including sync states " +
			"for providers that were skipped, failed, or not supported.",
	}, s.handleCredentialSummary)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "list_decisions",
		Description: "List proposed, approved, rejected, stale or blocked access decisions from the local ledger",
	}, s.handleListDecisions)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "decision_events",
		Description: "List append-only ledger events for one decision",
	}, s.handleDecisionEvents)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "approve_decision",
		Description: "Approve a proposed decision in the local ledger. This does not mutate any provider.",
	}, s.handleApproveDecision)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "reject_decision",
		Description: "Reject a proposed or approved decision with a reason. This does not mutate any provider.",
	}, s.handleRejectDecision)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "list_offboarding_certificates",
		Description: "List stored offboarding certificates from the local timeline",
	}, s.handleListCertificates)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "get_offboarding_certificate",
		Description: "Get one stored offboarding certificate by id",
	}, s.handleGetCertificate)

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

func (s *MCPServer) handleListBilling(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, listBillingOutput, error) {
	snapshots, err := s.store.ListLatestBillingSnapshots(ctx)
	if err != nil {
		return nil, listBillingOutput{}, err
	}
	if snapshots == nil {
		snapshots = []core.BillingSnapshot{}
	}
	return nil, listBillingOutput{Billing: snapshots}, nil
}

func (s *MCPServer) handleProviderBilling(ctx context.Context, _ *mcp.CallToolRequest, input providerInput) (*mcp.CallToolResult, providerBillingOutput, error) {
	snapshot, err := s.store.LatestBillingSnapshot(ctx, input.Provider)
	if err != nil {
		return nil, providerBillingOutput{}, err
	}
	if snapshot == nil {
		return nil, providerBillingOutput{
			Provider: input.Provider,
			Billing:  nil,
			Reason:   "no billing snapshot has been cached; run `unseat scan`",
		}, nil
	}
	return nil, providerBillingOutput{Provider: input.Provider, Billing: snapshot}, nil
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

func (s *MCPServer) handleListCredentials(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, listCredentialsOutput, error) {
	credentials, err := s.store.ListProviderCredentials(ctx)
	if err != nil {
		return nil, listCredentialsOutput{}, err
	}
	states, err := s.store.ListCredentialSyncStates(ctx)
	if err != nil {
		return nil, listCredentialsOutput{}, err
	}
	if credentials == nil {
		credentials = []core.ClassifiedCredential{}
	}
	if states == nil {
		states = []store.CredentialSyncState{}
	}
	return nil, listCredentialsOutput{Credentials: credentials, SyncStates: states}, nil
}

func (s *MCPServer) handleProviderCredentials(ctx context.Context, _ *mcp.CallToolRequest, input providerInput) (*mcp.CallToolResult, providerCredentialsOutput, error) {
	credentials, err := s.store.GetProviderCredentials(ctx, input.Provider)
	if err != nil {
		return nil, providerCredentialsOutput{}, err
	}
	states, err := s.store.ListCredentialSyncStates(ctx)
	if err != nil {
		return nil, providerCredentialsOutput{}, err
	}
	var state *store.CredentialSyncState
	for i := range states {
		if states[i].Provider == input.Provider {
			state = &states[i]
			break
		}
	}
	if credentials == nil {
		credentials = []core.ClassifiedCredential{}
	}
	return nil, providerCredentialsOutput{
		Provider:    input.Provider,
		Credentials: credentials,
		SyncState:   state,
	}, nil
}

func (s *MCPServer) handleCredentialSummary(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, credentialsSummaryOutput, error) {
	credentials, err := s.store.ListProviderCredentials(ctx)
	if err != nil {
		return nil, credentialsSummaryOutput{}, err
	}
	states, err := s.store.ListCredentialSyncStates(ctx)
	if err != nil {
		return nil, credentialsSummaryOutput{}, err
	}
	summary := summarizeCredentials(credentials, states)
	if states == nil {
		states = []store.CredentialSyncState{}
	}
	return nil, credentialsSummaryOutput{Summary: summary, SyncStates: states}, nil
}

func (s *MCPServer) handleListDecisions(ctx context.Context, _ *mcp.CallToolRequest, input decisionsInput) (*mcp.CallToolResult, listDecisionsOutput, error) {
	filter, err := decisionFilterFromMCP(input)
	if err != nil {
		return nil, listDecisionsOutput{}, err
	}
	decisions, err := s.store.ListDecisions(ctx, filter)
	if err != nil {
		return nil, listDecisionsOutput{}, err
	}
	if decisions == nil {
		decisions = []core.Decision{}
	}
	return nil, listDecisionsOutput{Decisions: decisions}, nil
}

func (s *MCPServer) handleDecisionEvents(ctx context.Context, _ *mcp.CallToolRequest, input decisionInput) (*mcp.CallToolResult, decisionEventsOutput, error) {
	events, err := s.store.ListDecisionEvents(ctx, input.ID)
	if err != nil {
		return nil, decisionEventsOutput{}, err
	}
	if events == nil {
		events = []store.DecisionEvent{}
	}
	return nil, decisionEventsOutput{Events: events}, nil
}

func (s *MCPServer) handleApproveDecision(ctx context.Context, _ *mcp.CallToolRequest, input decisionMutationInput) (*mcp.CallToolResult, decisionOutput, error) {
	decision, err := s.store.ApproveDecision(ctx, input.ID, strings.TrimSpace(input.By))
	if err != nil {
		return nil, decisionOutput{}, err
	}
	return nil, decisionOutput{Decision: decision}, nil
}

func (s *MCPServer) handleRejectDecision(ctx context.Context, _ *mcp.CallToolRequest, input decisionMutationInput) (*mcp.CallToolResult, decisionOutput, error) {
	decision, err := s.store.RejectDecision(ctx, input.ID, strings.TrimSpace(input.By), input.Reason)
	if err != nil {
		return nil, decisionOutput{}, err
	}
	return nil, decisionOutput{Decision: decision}, nil
}

func (s *MCPServer) handleListCertificates(ctx context.Context, _ *mcp.CallToolRequest, input certificatesInput) (*mcp.CallToolResult, listCertificatesOutput, error) {
	filter, err := certificateFilterFromMCP(input)
	if err != nil {
		return nil, listCertificatesOutput{}, err
	}
	certs, err := s.store.ListOffboardingCertificates(ctx, filter)
	if err != nil {
		return nil, listCertificatesOutput{}, err
	}
	if certs == nil {
		certs = []core.OffboardingCertificate{}
	}
	return nil, listCertificatesOutput{Certificates: certs}, nil
}

func (s *MCPServer) handleGetCertificate(ctx context.Context, _ *mcp.CallToolRequest, input certificateInput) (*mcp.CallToolResult, certificateOutput, error) {
	cert, err := s.store.GetOffboardingCertificate(ctx, input.ID)
	if err != nil {
		return nil, certificateOutput{}, err
	}
	return nil, certificateOutput{Certificate: cert}, nil
}

func decisionFilterFromMCP(input decisionsInput) (store.DecisionFilter, error) {
	filter := store.DecisionFilter{Limit: input.Limit}
	if filter.Limit == 0 {
		filter.Limit = 100
	}
	if input.Provider != "" {
		provider := strings.ToLower(strings.TrimSpace(input.Provider))
		filter.Provider = &provider
	}
	if input.Subject != "" {
		subject := strings.ToLower(strings.TrimSpace(input.Subject))
		filter.Subject = &subject
	}
	if input.Status != "" {
		status := core.DecisionStatus(strings.TrimSpace(input.Status))
		if !validDecisionStatus(status) {
			return store.DecisionFilter{}, fmt.Errorf("unknown decision status %q", input.Status)
		}
		filter.Status = &status
	}
	return filter, nil
}

func certificateFilterFromMCP(input certificatesInput) (store.CertificateFilter, error) {
	filter := store.CertificateFilter{Limit: input.Limit}
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	if input.Subject != "" {
		subject := strings.ToLower(strings.TrimSpace(input.Subject))
		filter.Subject = &subject
	}
	if input.Status != "" {
		status := core.CertificateStatus(strings.TrimSpace(input.Status))
		if !validCertificateStatus(status) {
			return store.CertificateFilter{}, fmt.Errorf("unknown certificate status %q", input.Status)
		}
		filter.Status = &status
	}
	return filter, nil
}

func validDecisionStatus(status core.DecisionStatus) bool {
	switch status {
	case core.DecisionProposed,
		core.DecisionApproved,
		core.DecisionRejected,
		core.DecisionExecuted,
		core.DecisionVerified,
		core.DecisionBlocked,
		core.DecisionStale,
		core.DecisionVerificationFailed:
		return true
	default:
		return false
	}
}

func validCertificateStatus(status core.CertificateStatus) bool {
	switch status {
	case core.CertificateComplete,
		core.CertificateCompleteWithProviderLimits,
		core.CertificateBlocked,
		core.CertificateIncomplete,
		core.CertificateStale:
		return true
	default:
		return false
	}
}

func summarizeCredentials(credentials []core.ClassifiedCredential, states []store.CredentialSyncState) []core.CredentialSummary {
	byProvider := make(map[string][]core.ClassifiedCredential)
	for _, c := range credentials {
		byProvider[c.Credential.Provider] = append(byProvider[c.Credential.Provider], c)
	}
	usageKnown := make(map[string]bool)
	for _, st := range states {
		usageKnown[st.Provider] = st.UsageKnown
	}

	providers := make([]string, 0, len(byProvider))
	for provider := range byProvider {
		providers = append(providers, provider)
	}
	sort.Strings(providers)

	out := make([]core.CredentialSummary, 0, len(providers))
	for _, provider := range providers {
		out = append(out, core.SummarizeCredentials(provider, byProvider[provider], usageKnown[provider]))
	}
	return out
}

func (s *MCPServer) handleGetMappings(_ context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, getMappingsOutput, error) {
	mappings := s.config.Mappings
	if mappings == nil {
		mappings = []config.Mapping{}
	}
	return nil, getMappingsOutput{Mappings: mappings}, nil
}
