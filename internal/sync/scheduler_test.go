package sync

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sderosiaux/unseat/config"
	"github.com/sderosiaux/unseat/internal/core"
	"github.com/sderosiaux/unseat/internal/provider"
	"github.com/sderosiaux/unseat/internal/store"
)

func newTestReconciler(t *testing.T) *Reconciler {
	t.Helper()

	identity := &fakeIdentity{
		groups: map[string][]core.User{
			"eng@co.com": {{Email: "alice@co.com"}},
		},
	}
	target := &fakeTarget{
		name:  "linear",
		users: []core.User{{Email: "alice@co.com"}},
		caps:  core.Capabilities{CanAdd: true, CanRemove: true},
	}
	reg := provider.NewRegistry()
	reg.Register(target)

	db, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	cfg := &config.Config{
		Mappings: []config.Mapping{
			{Group: "eng@co.com", Providers: []config.ProviderMapping{{Name: "linear", Role: "member"}}},
		},
		Policies: config.Policies{DryRun: true},
	}

	return NewReconciler(db, cfg, reg, identity)
}

func TestNewScheduler(t *testing.T) {
	rec := newTestReconciler(t)
	s := NewScheduler(rec, 5*time.Minute)

	assert.NotNil(t, s)
	assert.Equal(t, 5*time.Minute, s.interval)
}

func TestSchedulerRunsImmediatelyThenStops(t *testing.T) {
	rec := newTestReconciler(t)
	s := NewScheduler(rec, 1*time.Hour) // long interval — won't tick

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := s.Start(ctx)
	assert.NoError(t, err) // graceful shutdown returns nil
}

func TestSchedulerRunsOnTick(t *testing.T) {
	rec := newTestReconciler(t)
	s := NewScheduler(rec, 50*time.Millisecond) // fast tick

	var runs atomic.Int32
	s.onRun = func() { runs.Add(1) }

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Millisecond)
	defer cancel()

	err := s.Start(ctx)
	assert.NoError(t, err)

	// 1 immediate + at least 2 ticks at 50ms within 180ms
	assert.GreaterOrEqual(t, runs.Load(), int32(3))
}

func TestSchedulerGracefulShutdown(t *testing.T) {
	rec := newTestReconciler(t)
	s := NewScheduler(rec, 50*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()

	// Let it run a couple ticks then cancel.
	time.Sleep(120 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop within 2s after cancel")
	}
}
