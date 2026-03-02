package provider

import (
	"context"
	"testing"

	"github.com/sderosiaux/saas-watcher/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockProvider struct {
	name  string
	users []core.User
}

func (m *mockProvider) Name() string                                        { return m.name }
func (m *mockProvider) ListUsers(_ context.Context) ([]core.User, error)    { return m.users, nil }
func (m *mockProvider) AddUser(_ context.Context, _ string, _ string) error { return nil }
func (m *mockProvider) RemoveUser(_ context.Context, _ string) error        { return nil }
func (m *mockProvider) SetRole(_ context.Context, _ string, _ string) error { return nil }
func (m *mockProvider) Capabilities() core.Capabilities {
	return core.Capabilities{CanAdd: true, CanRemove: true}
}

func TestRegistryRegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	mock := &mockProvider{name: "test-provider"}
	reg.Register(mock)
	p, err := reg.Get("test-provider")
	require.NoError(t, err)
	assert.Equal(t, "test-provider", p.Name())
}

func TestRegistryGetUnknown(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.Get("nonexistent")
	assert.Error(t, err)
}

func TestRegistryList(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockProvider{name: "alpha"})
	reg.Register(&mockProvider{name: "beta"})
	names := reg.List()
	assert.Len(t, names, 2)
	assert.Contains(t, names, "alpha")
	assert.Contains(t, names, "beta")
}
