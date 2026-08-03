package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/imprun/windforce-core/internal/state"
)

const (
	defaultHumanTaskDeadlineInterval = time.Second
	defaultHumanTaskDeadlineBatch    = 100
	humanTaskDeadlineCycleBudget     = 5 * time.Second
)

func runHumanTaskDeadlineLoop(ctx context.Context, store state.HumanTaskDeadlineStore, interval time.Duration, batchSize int) {
	if interval <= 0 {
		interval = defaultHumanTaskDeadlineInterval
	}
	runHumanTaskDeadlineCycle(ctx, store, time.Now().UTC(), batchSize)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			runHumanTaskDeadlineCycle(ctx, store, now.UTC(), batchSize)
		}
	}
}

func runHumanTaskDeadlineCycle(ctx context.Context, store state.HumanTaskDeadlineStore, now time.Time, batchSize int) int64 {
	if batchSize <= 0 {
		batchSize = defaultHumanTaskDeadlineBatch
	}
	cycleContext, cancel := context.WithTimeout(ctx, humanTaskDeadlineCycleBudget)
	defer cancel()
	var total int64
	for cycleContext.Err() == nil {
		expired, err := store.ExpireDueHeldHumanTasks(cycleContext, now, batchSize)
		if err != nil {
			if ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "human task deadline sweep: %v\n", err)
			}
			return total
		}
		total += expired
		if expired < int64(batchSize) {
			return total
		}
	}
	return total
}
