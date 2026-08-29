package main

import (
	"context"
	"log"
	"time"

	"github.com/Lin-xun1113/SkillGate/internal/store"
)

// startSchedulerSweeper continuously reconciles expired leases and budget
// deadlines. SweepExpired is idempotent, so a process restart or overlapping
// poller cannot create a second result for the same logical trial.
func startSchedulerSweeper(ctx context.Context, queue store.QueueStore, interval time.Duration, limit int) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if limit < 1 {
		limit = 100
	}

	run := func() {
		result, err := queue.SweepExpired(ctx, limit)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("scheduler sweep failed: %v", err)
			}
			return
		}
		if result.Processed > 0 {
			log.Printf("scheduler sweep processed=%d retried=%d terminal=%d cancelled=%d", result.Processed, result.Retried, result.Terminal, result.Cancelled)
		}
	}

	// Reconcile work that expired while the process was down before waiting
	// for the first tick.
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
