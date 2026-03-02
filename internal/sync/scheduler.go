package sync

import (
	"context"
	"log/slog"
	"time"
)

// Scheduler runs the Reconciler at a configured interval.
type Scheduler struct {
	reconciler *Reconciler
	interval   time.Duration

	// onRun is called after each reconciliation pass (for testing).
	onRun func()
}

// NewScheduler creates a scheduler that ticks at the given interval.
func NewScheduler(reconciler *Reconciler, interval time.Duration) *Scheduler {
	return &Scheduler{reconciler: reconciler, interval: interval}
}

// Start runs the reconciler immediately, then at every interval.
// Blocks until ctx is cancelled. Returns nil on graceful shutdown.
func (s *Scheduler) Start(ctx context.Context) error {
	slog.Info("scheduler started", "interval", s.interval)

	s.runOnce(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("scheduler stopped")
			return nil
		case <-ticker.C:
			s.runOnce(ctx)
		}
	}
}

func (s *Scheduler) runOnce(ctx context.Context) {
	plans, err := s.reconciler.Run(ctx)
	if err != nil {
		slog.Error("reconciliation failed", "error", err)
	} else {
		totalAdd, totalRemove, totalUnchanged := 0, 0, 0
		for _, p := range plans {
			totalAdd += len(p.ToAdd)
			totalRemove += len(p.ToRemove)
			totalUnchanged += p.Unchanged
		}
		slog.Info("reconciliation complete",
			"providers", len(plans),
			"add", totalAdd,
			"remove", totalRemove,
			"unchanged", totalUnchanged,
		)
	}

	if s.onRun != nil {
		s.onRun()
	}
}
