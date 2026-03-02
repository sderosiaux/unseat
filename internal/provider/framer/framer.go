package framer

import (
	"context"
	"fmt"

	"github.com/sderosiaux/saas-watcher/internal/core"
)

var errNoAPI = fmt.Errorf("framer: no public API for user management")

// Provider is a stub — Framer has no public API for team/user management.
type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Name() string { return "framer" }

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
