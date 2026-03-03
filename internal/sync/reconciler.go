package sync

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/notify"
	"github.com/sderosiaux/unseat/internal/provider"
	"github.com/sderosiaux/unseat/internal/store"
)

// Reconciler orchestrates the full sync flow: fetch actual users from each
// provider, resolve desired users from the identity source, compute diffs via
// core.Reconcile, and execute add/remove actions.
type Reconciler struct {
	store    store.Store
	config   *config.Config
	registry *provider.Registry
	identity provider.IdentityProvider
	notifier *notify.Dispatcher
}

// NewReconciler wires all dependencies into a ready-to-run reconciler.
// The notifier is optional — pass nil to disable notifications.
func NewReconciler(s store.Store, cfg *config.Config, reg *provider.Registry, identity provider.IdentityProvider, opts ...ReconcilerOption) *Reconciler {
	r := &Reconciler{store: s, config: cfg, registry: reg, identity: identity}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// ReconcilerOption configures optional Reconciler dependencies.
type ReconcilerOption func(*Reconciler)

// WithNotifier attaches a notification dispatcher to the reconciler.
func WithNotifier(d *notify.Dispatcher) ReconcilerOption {
	return func(r *Reconciler) { r.notifier = d }
}

// Run executes a single reconciliation pass across all configured providers.
// Returns one ReconcilePlan per provider processed.
func (r *Reconciler) Run(ctx context.Context) ([]*core.ReconcilePlan, error) {
	// Collect unique provider names and group emails from mappings.
	providerNames := make(map[string]bool)
	groupEmails := make(map[string]bool)
	for _, m := range r.config.Mappings {
		groupEmails[m.Group] = true
		for _, p := range m.Providers {
			providerNames[p.Name] = true
		}
	}

	// Collect all desired emails from identity groups to build alias index.
	var allDesiredEmails []string
	seen := make(map[string]bool)
	for group := range groupEmails {
		members, err := r.identity.ListGroupMembers(ctx, group)
		if err != nil {
			slog.Error("list group members for alias index failed", "group", group, "error", err)
			continue
		}
		for _, m := range members {
			if !seen[m.Email] {
				seen[m.Email] = true
				allDesiredEmails = append(allDesiredEmails, m.Email)
			}
		}
	}

	aliasIndex := core.BuildAliasIndex(r.config.Aliases, allDesiredEmails)

	var plans []*core.ReconcilePlan

	for name := range providerNames {
		plan, err := r.reconcileProvider(ctx, name, aliasIndex)
		if err != nil {
			slog.Error("reconcile failed", "provider", name, "error", err)
			continue
		}
		plans = append(plans, plan)
	}

	return plans, nil
}

func (r *Reconciler) reconcileProvider(ctx context.Context, name string, aliasIndex map[string]string) (*core.ReconcilePlan, error) {
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
		AliasIndex:  aliasIndex,
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
			r.sendNotification(ctx, name, ua.Email, "pending_removal")
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
		r.sendNotification(ctx, name, ua.Email, "removed")
	}
}

func (r *Reconciler) sendNotification(ctx context.Context, providerName, email, action string) {
	if !r.config.Policies.NotifyOnRemove || r.notifier == nil {
		return
	}
	title := fmt.Sprintf("User %s from %s", action, providerName)
	body := fmt.Sprintf("%s was %s during reconciliation sync.", email, action)
	if err := r.notifier.Notify(ctx, notify.Message{
		Title:    title,
		Body:     body,
		Provider: providerName,
		Email:    email,
		Action:   action,
	}); err != nil {
		slog.Error("notification dispatch failed", "provider", providerName, "email", email, "error", err)
	}
}
