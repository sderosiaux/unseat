package sync

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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

	// The full directory decides who has actually left. Without it every seat
	// degrades to "review", which is the safe direction: unseat would report
	// rather than remove.
	directory, err := r.buildDirectory(ctx)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	var plans []*core.ReconcilePlan

	for name := range providerNames {
		plan, err := r.reconcileProvider(ctx, name, aliasIndex, directory)
		if err != nil {
			slog.Error("reconcile failed", "provider", name, "error", err)
			continue
		}
		plans = append(plans, plan)
	}

	return plans, nil
}

func (r *Reconciler) buildDirectory(ctx context.Context) (map[string]core.DirectoryUser, error) {
	users, err := r.identity.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	directory := make(map[string]core.DirectoryUser, len(users))
	for _, u := range users {
		key := strings.ToLower(u.Email)
		directory[key] = core.DirectoryUser{Email: key, Suspended: u.Status == "suspended"}
	}
	return directory, nil
}

func (r *Reconciler) reconcileProvider(ctx context.Context, name string, aliasIndex map[string]string, directory map[string]core.DirectoryUser) (*core.ReconcilePlan, error) {
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
	r.store.UpdateSyncState(ctx, name, len(actualUsers), p.Capabilities().ReportsActivity)

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
		Directory:   directory,
		Domain:      r.config.IdentitySource.Domain,
	})
	if err != nil {
		return nil, fmt.Errorf("reconcile %s: %w", name, err)
	}

	// Execute actions unless dry-run.
	if !plan.DryRun {
		r.cancelReturnedUsers(ctx, name, plan)
		r.executeActions(ctx, name, p, plan)
		r.executeExpiredRemovals(ctx, name, p, actualUsers)
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
			slog.Warn("removal skipped: provider does not support it",
				"provider", name, "email", ua.Email)
			continue
		}
		r.removeSeat(ctx, name, p, ua.Email, ua.Target())
	}
}

// removeSeat performs one removal and records it. target is the identifier the
// provider itself knows, which differs from email whenever an alias is in play.
func (r *Reconciler) removeSeat(ctx context.Context, name string, p provider.Provider, email, target string) {
	if err := p.RemoveUser(ctx, target); err != nil {
		slog.Error("remove user failed", "provider", name, "email", email, "target", target, "error", err)
		return
	}
	r.store.InsertEvent(ctx, core.Event{
		Type: core.EventUserRemoved, Provider: name, Email: email,
		Trigger: "sync", OccurredAt: time.Now(),
	})
	r.sendNotification(ctx, name, email, "removed")
}

// cancelReturnedUsers clears the countdown for anyone who is no longer an
// orphan. Without it, someone who leaves a group and comes back before the
// grace period elapses is still removed when it fires.
func (r *Reconciler) cancelReturnedUsers(ctx context.Context, name string, plan *core.ReconcilePlan) {
	if r.config.Policies.GracePeriod <= 0 {
		return
	}

	stillOrphaned := make(map[string]bool, len(plan.ToRemove))
	for _, ua := range plan.ToRemove {
		stillOrphaned[ua.Email] = true
	}

	pending, err := r.store.GetPendingRemovals(ctx, name)
	if err != nil {
		slog.Error("read pending removals failed", "provider", name, "error", err)
		return
	}
	for _, pr := range pending {
		if stillOrphaned[pr.Email] {
			continue
		}
		if err := r.store.CancelPendingRemoval(ctx, name, pr.Email); err != nil {
			slog.Error("cancel pending removal failed", "provider", name, "email", pr.Email, "error", err)
			continue
		}
		slog.Info("pending removal cancelled: identity is active again", "provider", name, "email", pr.Email)
	}
}

// executeExpiredRemovals reclaims seats whose grace period has elapsed.
//
// Nothing used to read the pending_removals table, so a configured grace
// period silently meant "never remove anything".
func (r *Reconciler) executeExpiredRemovals(ctx context.Context, name string, p provider.Provider, actualUsers []core.User) {
	if r.config.Policies.GracePeriod <= 0 {
		return
	}
	if !p.Capabilities().CanRemove {
		return
	}

	expired, err := r.store.GetExpiredRemovals(ctx, name)
	if err != nil {
		slog.Error("read expired removals failed", "provider", name, "error", err)
		return
	}

	// Map canonical identities back to the identifier the provider uses.
	target := make(map[string]string, len(actualUsers))
	for _, u := range actualUsers {
		target[strings.ToLower(u.Email)] = u.Email
	}
	if r.config.Aliases != nil {
		for canonical, aliases := range r.config.Aliases {
			for _, alias := range aliases {
				if raw, ok := target[strings.ToLower(alias)]; ok {
					target[strings.ToLower(canonical)] = raw
				}
			}
		}
	}

	for _, pr := range expired {
		raw, ok := target[strings.ToLower(pr.Email)]
		if !ok {
			// The seat is gone from the provider already: close the countdown
			// rather than retrying a removal that can only fail.
			if err := r.store.CancelPendingRemoval(ctx, name, pr.Email); err != nil {
				slog.Error("close stale pending removal failed", "provider", name, "email", pr.Email, "error", err)
			}
			continue
		}
		r.removeSeat(ctx, name, p, pr.Email, raw)
		if err := r.store.CancelPendingRemoval(ctx, name, pr.Email); err != nil {
			slog.Error("close pending removal failed", "provider", name, "email", pr.Email, "error", err)
		}
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
