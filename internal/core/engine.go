package core

import (
	"context"
	"strings"
	"time"
)

// DesiredResolver fetches the list of users that should have access based on a group email.
type DesiredResolver func(ctx context.Context, groupEmail string) ([]User, error)

// GroupMappingInput maps a group (e.g. Google Group) to a role in the SaaS provider.
type GroupMappingInput struct {
	GroupEmail string
	Role       string
}

// ReconcileInput holds everything needed to compute a reconciliation plan.
type ReconcileInput struct {
	ProviderName    string
	GroupMappings   []GroupMappingInput
	DesiredResolver DesiredResolver
	ActualUsers     []User
	Exceptions      map[string]bool // lowercased emails to never remove
	DryRun          bool
	GracePeriod     time.Duration
}

// ReconcilePlan is the computed diff: who to add, who to remove, how many unchanged.
type ReconcilePlan struct {
	ProviderName string       `json:"provider"`
	ToAdd        []UserAction `json:"to_add"`
	ToRemove     []UserAction `json:"to_remove"`
	Unchanged    int          `json:"unchanged"`
	DryRun       bool         `json:"dry_run"`
}

// UserAction represents a single add or remove action on a SaaS seat.
type UserAction struct {
	Email string `json:"email"`
	Role  string `json:"role,omitempty"`
}

// Reconcile computes the diff between desired (from group resolver) and actual (from SaaS provider).
// It returns a plan of add/remove actions, respecting exceptions and dry-run mode.
func Reconcile(ctx context.Context, input ReconcileInput) (*ReconcilePlan, error) {
	// Build desired set from all group mappings.
	desiredMap := make(map[string]string) // email -> role
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

	// Build actual set.
	actualSet := make(map[string]bool, len(input.ActualUsers))
	for _, u := range input.ActualUsers {
		actualSet[strings.ToLower(u.Email)] = true
	}

	exceptions := input.Exceptions
	if exceptions == nil {
		exceptions = make(map[string]bool)
	}

	plan := &ReconcilePlan{
		ProviderName: input.ProviderName,
		DryRun:       input.DryRun,
	}

	// To add: in desired but not in actual.
	for email, role := range desiredMap {
		if !actualSet[email] {
			plan.ToAdd = append(plan.ToAdd, UserAction{Email: email, Role: role})
		}
	}

	// To remove: in actual but not in desired, minus exceptions.
	for _, u := range input.ActualUsers {
		key := strings.ToLower(u.Email)
		if _, desired := desiredMap[key]; !desired && !exceptions[key] {
			plan.ToRemove = append(plan.ToRemove, UserAction{Email: key})
		}
	}

	plan.Unchanged = len(input.ActualUsers) - len(plan.ToRemove)

	return plan, nil
}
