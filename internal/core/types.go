package core

import (
	"strings"
	"time"
)

// Seat statuses. Providers must report one of these: reconciliation treats
// StatusSuspended as "already deactivated, do not act again", so a connector
// inventing its own vocabulary silently re-triggers removals every sync.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
	StatusInvited   = "invited"
)

type User struct {
	Email          string            `json:"email"`
	DisplayName    string            `json:"display_name"`
	Role           string            `json:"role"`
	Status         string            `json:"status"` // one of the Status* constants
	ProviderID     string            `json:"provider_id"`
	LastActivityAt *time.Time        `json:"last_activity_at,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// Key returns a normalized identifier for deduplication and comparison.
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
	CanAdd     bool `json:"can_add"`
	CanRemove  bool `json:"can_remove"`
	CanSuspend bool `json:"can_suspend"`
	CanSetRole bool `json:"can_set_role"`
	HasWebhook bool `json:"has_webhook"`
	// ReportsActivity declares that ListUsers populates User.LastActivityAt.
	// Without it, a nil LastActivityAt means "this API tells us nothing", not
	// "this person never logged in" — conflating the two made every user of
	// every non-instrumented provider look inactive.
	ReportsActivity bool `json:"reports_activity"`
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

// DiffResult holds the outcome of comparing a desired user set against the actual set.
type DiffResult struct {
	ToAdd    []User `json:"to_add"`
	ToRemove []User `json:"to_remove"`
}

// ComputeDiff returns users present in desired but missing from actual (ToAdd),
// and users present in actual but missing from desired (ToRemove).
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
