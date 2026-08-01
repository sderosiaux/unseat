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
	Exceptions      map[string]bool   // lowercased emails to never remove
	AliasIndex      map[string]string // lowercased alias -> canonical email
	DryRun          bool
	GracePeriod     time.Duration
	// Directory is every identity known to the identity source, keyed by
	// lowercased email. Removal requires it: without a directory there is no
	// way to tell a departed employee from an unmapped one, and everything
	// falls into ToReview instead of ToRemove.
	Directory map[string]DirectoryUser
	// Domain is the corporate domain used to spot external identities.
	Domain string
}

// ReconcilePlan is the computed diff: who to add, who to remove, what a human
// must look at, and how many seats are already correct.
type ReconcilePlan struct {
	ProviderName string       `json:"provider"`
	ToAdd        []UserAction `json:"to_add"`
	// ToRemove holds only seats with no active directory identity. An active
	// employee is never in here, however incomplete the mappings are.
	ToRemove []UserAction `json:"to_remove"`
	// ToReview holds seats that differ from the mappings but must not be
	// touched automatically: active employees outside every mapped group,
	// external identities, and usernames that resolve to nobody.
	ToReview []SeatReview `json:"to_review"`
	// AlreadyDeactivated holds orphaned seats the provider already reports as
	// suspended. There is nothing left to execute on them, but most vendors
	// keep billing a deactivated seat until it is fully deleted, so they are
	// surfaced rather than folded into Unchanged.
	AlreadyDeactivated []UserAction `json:"already_deactivated"`
	Unchanged          int          `json:"unchanged"`
	DryRun             bool         `json:"dry_run"`
}

// SeatReview is a seat that needs a human decision rather than an action.
type SeatReview struct {
	Email  string    `json:"email"`
	Class  SeatClass `json:"class"`
	Reason string    `json:"reason"`
}

// UserAction represents a single add or remove action on a SaaS seat.
type UserAction struct {
	// Email is the canonical corporate identity, after alias resolution.
	Email string `json:"email"`
	// ProviderEmail is the identifier the provider itself uses, before alias
	// resolution. Removal must target this: a provider that only knows the
	// login "tiger-khan" 404s on "tkhan@co.com", and the seat stays billed
	// while the run reports it as reclaimed.
	ProviderEmail string `json:"provider_email,omitempty"`
	Role          string `json:"role,omitempty"`
}

// Target returns the identifier to use when calling a provider API.
func (a UserAction) Target() string {
	if a.ProviderEmail != "" {
		return a.ProviderEmail
	}
	return a.Email
}

// BuildAliasIndex creates a lookup table mapping aliases to canonical emails.
// It generates implicit aliases from the local part of each desired email,
// then adds explicit aliases from the config.
func BuildAliasIndex(explicitAliases map[string][]string, desiredEmails []string) map[string]string {
	index := make(map[string]string)

	// Implicit: local part of each known email -> full email. This is what
	// lets a provider that only exposes usernames — GitHub without SSO hands
	// back bare logins — be matched to a person at all.
	//
	// Local parts that map to more than one identity are dropped rather than
	// resolved to whichever came last. Guessing here would attribute someone
	// else's seat to a real person, in a report that drives deprovisioning.
	ambiguous := make(map[string]bool)
	add := func(key, email string) {
		if key == "" {
			return
		}
		if existing, seen := index[key]; seen && existing != email {
			ambiguous[key] = true
			return
		}
		index[key] = email
	}

	for _, email := range desiredEmails {
		lower := strings.ToLower(email)
		at := strings.IndexByte(lower, '@')
		if at <= 0 {
			continue
		}
		localPart := lower[:at]
		add(localPart, lower)
		// Separator-insensitive form too: a directory of first.last@ meets
		// providers whose usernames are first-last or firstlast, and matching
		// on the exact local part alone leaves those unattributable.
		add(squashSeparators(localPart), lower)
	}
	for key := range ambiguous {
		delete(index, key)
	}

	// Explicit: config-declared aliases override implicit ones.
	for canonical, aliases := range explicitAliases {
		canonicalLower := strings.ToLower(canonical)
		for _, alias := range aliases {
			index[strings.ToLower(alias)] = canonicalLower
		}
	}

	return index
}

// squashSeparators strips the characters people vary between a directory
// address and a provider username: jane.doe, jane-doe and jane_doe all become
// janedoe.
func squashSeparators(s string) string {
	return strings.NewReplacer(".", "", "-", "", "_", "").Replace(s)
}

// resolveEmail maps an email or username to its canonical form via the alias index.
func (input ReconcileInput) resolveEmail(email string) string {
	return resolveAlias(input.AliasIndex, email)
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

	// Build actual set, resolving aliases to canonical emails.
	actualSet := make(map[string]bool, len(input.ActualUsers))
	for _, u := range input.ActualUsers {
		actualSet[input.resolveEmail(u.Email)] = true
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

	// Removal is driven by directory status, not by group membership.
	// "Not in a mapped group" is a mapping gap; "not in the directory" is a
	// departure. Only the second justifies taking a seat away.
	desiredSet := make(map[string]bool, len(desiredMap))
	for email := range desiredMap {
		desiredSet[email] = true
	}

	seats := ClassifySeats(ClassifyInput{
		ProviderName:  input.ProviderName,
		ActualUsers:   input.ActualUsers,
		Directory:     input.Directory,
		DesiredEmails: desiredSet,
		Domain:        input.Domain,
		AliasIndex:    input.AliasIndex,
		Exceptions:    exceptions,
	})

	for _, seat := range seats {
		switch {
		case seat.Protected:
			// Declared intentional by policy: never acted on, and never
			// re-reported either — an exception the operator has to re-read
			// every run is an exception they stop reading.
			plan.Unchanged++
		case seat.Class == SeatOrphan:
			// Most providers implement removal as deactivation, so the seat
			// keeps coming back as an orphan on every run. Re-issuing the
			// removal would re-log an event and re-send a notification for the
			// same person forever, so an already-suspended seat is recorded,
			// not re-actioned.
			if seat.User.Status == StatusSuspended {
				plan.AlreadyDeactivated = append(plan.AlreadyDeactivated, UserAction{
					Email:         seat.Email,
					ProviderEmail: seat.RawEmail,
				})
				continue
			}
			plan.ToRemove = append(plan.ToRemove, UserAction{
				Email:         seat.Email,
				ProviderEmail: seat.RawEmail,
			})
		case seat.Class == SeatManaged:
			plan.Unchanged++
		default:
			plan.ToReview = append(plan.ToReview, SeatReview{
				Email:  seat.Email,
				Class:  seat.Class,
				Reason: seat.Reason,
			})
		}
	}

	return plan, nil
}
