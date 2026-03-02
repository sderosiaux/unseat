package stripe

import (
	"context"
	"fmt"

	"github.com/sderosiaux/unseat/internal/core"
)

var errNoAPI = fmt.Errorf("stripe: no public API for dashboard user management")

// Provider is a stub -- Stripe has no public API for managing dashboard team members.
type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Name() string { return "stripe" }

func (p *Provider) Capabilities() core.Capabilities {
	return core.Capabilities{}
}

func (p *Provider) ListUsers(_ context.Context) ([]core.User, error) {
	return nil, errNoAPI
}

func (p *Provider) AddUser(_ context.Context, _, _ string) error {
	return errNoAPI
}

func (p *Provider) RemoveUser(_ context.Context, _ string) error {
	return errNoAPI
}

func (p *Provider) SetRole(_ context.Context, _, _ string) error {
	return errNoAPI
}
