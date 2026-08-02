package provider

import (
	"context"

	"github.com/sderosiaux/unseat/internal/core"
)

// Provider is the interface every SaaS connector must implement.
type Provider interface {
	Name() string
	ListUsers(ctx context.Context) ([]core.User, error)
	AddUser(ctx context.Context, email string, role string) error
	RemoveUser(ctx context.Context, email string) error
	SetRole(ctx context.Context, email string, role string) error
	Capabilities() core.Capabilities
}

// IdentityProvider extends Provider with group-level operations (e.g. Google Workspace).
type IdentityProvider interface {
	Provider
	ListGroups(ctx context.Context) ([]core.Group, error)
	ListGroupMembers(ctx context.Context, groupEmail string) ([]core.User, error)
}

// BillingProvider is implemented by connectors whose API exposes subscription
// data — plan, billed seat count, renewal date, sometimes the rate itself.
//
// Implement it wherever the vendor exposes anything: a price unseat can read
// is a price the operator does not have to type, and asking for a value the
// API already knows is a defect. Connectors that cannot answer simply do not
// implement it, and unseat reports billing as unavailable instead of inventing
// a price elsewhere.
type BillingProvider interface {
	Provider
	Billing(ctx context.Context) (*core.Billing, error)
}

// CredentialProvider is implemented by connectors whose API exposes the
// non-human access the org has granted: installed apps, integrations,
// webhooks, tokens.
//
// It is separate from Provider because these are not seats and must not be
// counted as such — they cost nothing per month and cannot be deprovisioned by
// suspending a person. They are also, for the same reason, the access that
// offboarding structurally cannot reach.
//
// The connector reports; it does not judge. Whether a credential is orphaned
// is a question about the directory, which the connector cannot see.
type CredentialProvider interface {
	Provider
	ListCredentials(ctx context.Context) ([]core.Credential, error)
}
