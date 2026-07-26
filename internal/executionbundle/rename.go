package executionbundle

import (
	"context"
	"errors"
	"os"
	"runtime"
	"syscall"
	"time"
)

var windowsRenameRetryDelays = []time.Duration{
	10 * time.Millisecond,
	25 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
}

func renameDir(ctx context.Context, source, destination string) error {
	return renameWithRetry(
		ctx,
		func() error { return os.Rename(source, destination) },
		isRetryableRenameError,
		windowsRenameRetryDelays,
	)
}

func renameWithRetry(ctx context.Context, rename func() error, retryable func(error) bool, delays []time.Duration) error {
	for attempt := 0; ; attempt++ {
		err := rename()
		if err == nil || attempt == len(delays) || !retryable(err) {
			return err
		}
		timer := time.NewTimer(delays[attempt])
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func isRetryableRenameError(err error) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	return errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.Errno(32))
}
