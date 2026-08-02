package store

import (
	"context"
	"time"

	"github.com/sderosiaux/unseat/internal/core"
)

// Store defines the persistence contract for unseat.
type Store interface {
	UpsertProviderUsers(ctx context.Context, provider string, users []core.User) error
	GetProviderUsers(ctx context.Context, provider string) ([]core.User, error)
	GetInactiveUsers(ctx context.Context, since time.Time, providers []string) ([]InactiveUser, error)
	InsertEvent(ctx context.Context, event core.Event) error
	ListEvents(ctx context.Context, filter EventFilter) ([]core.Event, error)
	InsertPendingRemoval(ctx context.Context, provider, email string, expiresAt time.Time) error
	GetPendingRemovals(ctx context.Context, provider string) ([]PendingRemoval, error)
	CancelPendingRemoval(ctx context.Context, provider, email string) error
	GetExpiredRemovals(ctx context.Context, provider string) ([]PendingRemoval, error)
	UpdateSyncState(ctx context.Context, provider string, userCount int, reportsActivity bool) error
	GetSyncState(ctx context.Context, provider string) (*SyncState, error)
	ListSyncStates(ctx context.Context) ([]SyncState, error)
	ActivityReportingProviders(ctx context.Context) (reporting, silent []string, err error)
	InsertBillingSnapshot(ctx context.Context, snapshot core.BillingSnapshot) error
	LatestBillingSnapshot(ctx context.Context, provider string) (*core.BillingSnapshot, error)
	ListLatestBillingSnapshots(ctx context.Context) ([]core.BillingSnapshot, error)
	UpsertProviderCredentials(ctx context.Context, provider string, credentials []core.ClassifiedCredential) error
	GetProviderCredentials(ctx context.Context, provider string) ([]core.ClassifiedCredential, error)
	ListProviderCredentials(ctx context.Context) ([]core.ClassifiedCredential, error)
	UpdateCredentialSyncState(ctx context.Context, state CredentialSyncState) error
	ListCredentialSyncStates(ctx context.Context) ([]CredentialSyncState, error)
	Close() error
}

// EventFilter controls which events are returned by ListEvents.
type EventFilter struct {
	Provider *string
	Type     *core.EventType
	Since    *time.Time
	Limit    int
}

// PendingRemoval represents a user flagged for removal with a grace period.
type PendingRemoval struct {
	Provider   string    `json:"provider"`
	Email      string    `json:"email"`
	DetectedAt time.Time `json:"detected_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Cancelled  bool      `json:"cancelled"`
}

// InactiveUser represents a user with no recent activity across any provider.
type InactiveUser struct {
	Provider       string     `json:"provider"`
	Email          string     `json:"email"`
	DisplayName    string     `json:"display_name"`
	LastActivityAt *time.Time `json:"last_activity_at"`
	Status         string     `json:"status"`
}

// SyncState tracks the last sync status for a provider.
type SyncState struct {
	Provider     string    `json:"provider"`
	LastSyncedAt time.Time `json:"last_synced_at"`
	UserCount    int       `json:"user_count"`
	Status       string    `json:"status"`
}

// CredentialSyncState tracks the last credential inventory read for a provider.
//
// Status is intentionally explicit. A provider that is skipped or not supported
// is not clean; it is unevaluated, and callers must surface that distinction.
type CredentialSyncState struct {
	Provider        string    `json:"provider"`
	LastSyncedAt    time.Time `json:"last_synced_at"`
	CredentialCount int       `json:"credential_count"`
	Status          string    `json:"status"`
	UsageKnown      bool      `json:"usage_known"`
	Message         string    `json:"message,omitempty"`
}
