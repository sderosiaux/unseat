package sync

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sderosiaux/saas-watcher/config"
	"github.com/sderosiaux/saas-watcher/internal/core"
	"github.com/sderosiaux/saas-watcher/internal/provider"
	"github.com/sderosiaux/saas-watcher/internal/store"
)

// Reconciler orchestrates the full sync flow: fetch actual users from each
// provider, resolve desired users from the identity source, compute diffs via
// core.Reconcile, and execute add/remove actions.
type Reconciler struct {
	store    store.Store
	config   *config.Config
	registry *provider.Registry
	identity provider.IdentityProvider
}

// NewReconciler wires all dependencies into a ready-to-run reconciler.
func NewReconciler(s store.Store, cfg *config.Config, reg *provider.Registry, identity provider.IdentityProvider) *Reconciler {
	return &Reconciler{store: s, config: cfg, registry: reg, identity: identity}
}

// Run executes a single reconciliation pass across all configured providers.
// Returns one ReconcilePlan per provider processed.
func (r *Reconciler) Run(ctx context.Context) ([]*core.ReconcilePlan, error) {
	// Collect unique provider names from mappings.
	providerNames := make(map[string]bool)
	for _, m := range r.config.Mappings {
		for _, p := range m.Providers {
			providerNames[p.Name] = true
		}
	}

	var plans []*core.ReconcilePlan

	for name := range providerNames {
		plan, err := r.reconcileProvider(ctx, name)
		if err != nil {
			slog.Error("reconcile failed", "provider", name, "error", err)
			continue
		}
		plans = append(plans, plan)
	}

	return plans, nil
}

func (r *Reconciler) reconcileProvider(ctx context.Context, name string) (*core.ReconcilePlan, error) {
	p, err := r.registry.Get(name)
	if err != nil {
		slog.Warn("provider not registered, skipping", "provider", name, "error", err)
		return nil, err
	}

	// Fetch actual users from provider.
	actualUsers, err := p.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list users from %s: %w", name, err)
	}

	// Cache actual users in store.
	if err := r.store.UpsertProviderUsers(ctx, name, actualUsers); err != nil {
		slog.Error("failed to cache users", "provider", name, "error", err)
	}
	r.store.UpdateSyncState(ctx, name, len(actualUsers))

	// Build group mappings for this provider.
	groupMappings := r.config.GroupsForProvider(name)
	gmInputs := make([]core.GroupMappingInput, 0, len(groupMappings))
	for _, gm := range groupMappings {
		gmInputs = append(gmInputs, core.GroupMappingInput{GroupEmail: gm.Group, Role: gm.Role})
	}

	// Build exceptions set.
	exceptions := make(map[string]bool)
	for _, ex := range r.config.Policies.Exceptions {
		for _, prov := range ex.Providers {
			if prov == "*" || prov == name {
				exceptions[ex.Email] = true
			}
		}
	}

	// Compute diff.
	plan, err := core.Reconcile(ctx, core.ReconcileInput{
		ProviderName:  name,
		GroupMappings: gmInputs,
		DesiredResolver: func(ctx context.Context, groupEmail string) ([]core.User, error) {
			return r.identity.ListGroupMembers(ctx, groupEmail)
		},
		ActualUsers: actualUsers,
		Exceptions:  exceptions,
		DryRun:      r.config.Policies.DryRun,
		GracePeriod: r.config.Policies.GracePeriod,
	})
	if err != nil {
		return nil, fmt.Errorf("reconcile %s: %w", name, err)
	}

	// Execute actions unless dry-run.
	if !plan.DryRun {
		r.executeActions(ctx, name, p, plan)
	}

	// Log sync completed.
	r.store.InsertEvent(ctx, core.Event{
		Type:       core.EventSyncCompleted,
		Provider:   name,
		Details:    fmt.Sprintf("add=%d remove=%d unchanged=%d", len(plan.ToAdd), len(plan.ToRemove), plan.Unchanged),
		Trigger:    "sync",
		OccurredAt: time.Now(),
	})

	return plan, nil
}

func (r *Reconciler) executeActions(ctx context.Context, name string, p provider.Provider, plan *core.ReconcilePlan) {
	caps := p.Capabilities()

	for _, ua := range plan.ToAdd {
		if !caps.CanAdd {
			continue
		}
		if err := p.AddUser(ctx, ua.Email, ua.Role); err != nil {
			slog.Error("add user failed", "provider", name, "email", ua.Email, "error", err)
			continue
		}
		r.store.InsertEvent(ctx, core.Event{
			Type: core.EventUserAdded, Provider: name, Email: ua.Email,
			Trigger: "sync", OccurredAt: time.Now(),
		})
	}

	for _, ua := range plan.ToRemove {
		if r.config.Policies.GracePeriod > 0 {
			r.store.InsertPendingRemoval(ctx, name, ua.Email, time.Now().Add(r.config.Policies.GracePeriod))
			continue
		}
		if !caps.CanRemove {
			continue
		}
		if err := p.RemoveUser(ctx, ua.Email); err != nil {
			slog.Error("remove user failed", "provider", name, "email", ua.Email, "error", err)
			continue
		}
		r.store.InsertEvent(ctx, core.Event{
			Type: core.EventUserRemoved, Provider: name, Email: ua.Email,
			Trigger: "sync", OccurredAt: time.Now(),
		})
	}
}
