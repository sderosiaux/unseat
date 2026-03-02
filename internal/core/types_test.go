package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUserKey(t *testing.T) {
	u := User{Email: "Alice@Company.com", DisplayName: "Alice"}
	assert.Equal(t, "alice@company.com", u.Key())
}

func TestCapabilities(t *testing.T) {
	c := Capabilities{CanAdd: true, CanRemove: true}
	assert.True(t, c.CanAdd)
	assert.True(t, c.CanRemove)
	assert.False(t, c.CanSuspend)
}

func TestEventType(t *testing.T) {
	assert.Equal(t, EventType("user_added"), EventUserAdded)
	assert.Equal(t, EventType("user_removed"), EventUserRemoved)
}

func TestDiffResult(t *testing.T) {
	desired := []User{
		{Email: "alice@co.com"},
		{Email: "bob@co.com"},
	}
	actual := []User{
		{Email: "bob@co.com"},
		{Email: "charlie@co.com"},
	}
	diff := ComputeDiff(desired, actual)
	assert.Len(t, diff.ToAdd, 1)
	assert.Equal(t, "alice@co.com", diff.ToAdd[0].Email)
	assert.Len(t, diff.ToRemove, 1)
	assert.Equal(t, "charlie@co.com", diff.ToRemove[0].Email)
}
