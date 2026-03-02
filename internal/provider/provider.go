package provider

import (
	"context"

	"github.com/sderosiaux/saas-watcher/internal/core"
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
