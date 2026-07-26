package executionbundle

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRenameWithRetryRecoversFromTransientError(t *testing.T) {
	transient := errors.New("transient")
	attempts := 0
	err := renameWithRetry(
		context.Background(),
		func() error {
			attempts++
			if attempts < 3 {
				return transient
			}
			return nil
		},
		func(err error) bool { return errors.Is(err, transient) },
		[]time.Duration{0, 0},
	)
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestRenameWithRetryStopsOnPermanentError(t *testing.T) {
	permanent := errors.New("permanent")
	attempts := 0
	err := renameWithRetry(
		context.Background(),
		func() error {
			attempts++
			return permanent
		},
		func(error) bool { return false },
		[]time.Duration{0, 0},
	)
	if !errors.Is(err, permanent) {
		t.Fatalf("error = %v, want permanent error", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRenameWithRetryHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := renameWithRetry(
		ctx,
		func() error { return errors.New("transient") },
		func(error) bool { return true },
		[]time.Duration{time.Hour},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}
