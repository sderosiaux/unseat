package stripe

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sderosiaux/unseat/internal/core"
)

func TestProviderName(t *testing.T) {
	p := New()
	assert.Equal(t, "stripe", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := New()
	caps := p.Capabilities()
	want := core.Capabilities{}
	assert.Equal(t, want, caps)
}

func TestListUsers(t *testing.T) {
	p := New()
	users, err := p.ListUsers(context.Background())
	assert.Nil(t, users)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no public API")
}

func TestAddUserNotSupported(t *testing.T) {
	p := New()
	err := p.AddUser(context.Background(), "test@co.com", "member")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no public API")
}

func TestRemoveUser(t *testing.T) {
	p := New()
	err := p.RemoveUser(context.Background(), "test@co.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no public API")
}

func TestSetRoleNotSupported(t *testing.T) {
	p := New()
	err := p.SetRole(context.Background(), "test@co.com", "admin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no public API")
}
