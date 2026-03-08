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
	GetInactiveUsers(ctx context.Context, since time.Time) ([]InactiveUser, error)
	InsertEvent(ctx context.Context, event core.Event) error
	ListEvents(ctx context.Context, filter EventFilter) ([]core.Event, error)
	InsertPendingRemoval(ctx context.Context, provider, email string, expiresAt time.Time) error
	GetPendingRemovals(ctx context.Context, provider string) ([]PendingRemoval, error)
	CancelPendingRemoval(ctx context.Context, provider, email string) error
	GetExpiredRemovals(ctx context.Context) ([]PendingRemoval, error)
	UpdateSyncState(ctx context.Context, provider string, userCount int) error
	GetSyncState(ctx context.Context, provider string) (*SyncState, error)
	ListSyncStates(ctx context.Context) ([]SyncState, error)
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
