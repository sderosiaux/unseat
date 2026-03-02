package google

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProviderName(t *testing.T) {
	p := &Provider{domain: "example.com"}
	assert.Equal(t, "google-directory", p.Name())
}

func TestProviderCapabilities(t *testing.T) {
	p := &Provider{}
	caps := p.Capabilities()
	assert.True(t, caps.CanRemove)
	assert.True(t, caps.CanAdd)
	assert.True(t, caps.CanSuspend)
	assert.True(t, caps.HasWebhook)
}
