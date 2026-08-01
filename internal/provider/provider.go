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
// implement it, and unseat degrades to whatever config provides.
type BillingProvider interface {
	Provider
	Billing(ctx context.Context) (*core.Billing, error)
}
