package completion

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/state"
	"github.com/imprun/windforce-core/internal/webhook"
)

type Store interface {
	ClaimTriggerCompletion(context.Context, string, time.Duration) (*state.TriggerCompletionClaim, error)
	CompleteTriggerCompletion(context.Context, state.TriggerCompletionLease, state.TriggerCompletionResult) error
}

type Dispatcher struct {
	Store       Store
	Callback    Sender
	Publish     Sender
	WorkerID    string
	LeaseTTL    time.Duration
	MaxAttempts int
	BackoffBase time.Duration
	BackoffMax  time.Duration
	Now         func() time.Time
	Logger      *slog.Logger
}

func (dispatcher *Dispatcher) ProcessOne(ctx context.Context) (bool, error) {
	if dispatcher.Store == nil {
		return false, errors.New("trigger completion dispatcher requires store")
	}
	dispatcher.applyDefaults()
	claimed, err := dispatcher.Store.ClaimTriggerCompletion(ctx, dispatcher.WorkerID, dispatcher.LeaseTTL)
	if errors.Is(err, state.ErrNoCompletion) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var sender Sender
	switch claimed.Delivery.Completion.Mode {
	case state.TriggerCompletionModeCallback:
		sender = dispatcher.Callback
	case state.TriggerCompletionModePublish:
		sender = dispatcher.Publish
	}
	if sender == nil {
		return true, dispatcher.Store.CompleteTriggerCompletion(ctx, claimed.Lease, state.TriggerCompletionResult{
			State:        state.TriggerCompletionFailed,
			ErrorSummary: "completion_sender_missing",
		})
	}
	attempt := sender.Send(ctx, claimed)
	result := dispatcher.result(claimed, attempt)
	if err := dispatcher.Store.CompleteTriggerCompletion(ctx, claimed.Lease, result); err != nil {
		return true, err
	}
	dispatcher.logger().Info("trigger completion attempted",
		"workspace", claimed.Delivery.WorkspaceID,
		"trigger", claimed.Delivery.TriggerID,
		"delivery", claimed.Delivery.ID,
		"run", claimed.Run.ID,
		"mode", claimed.Delivery.Completion.Mode,
		"attempt", claimed.Delivery.CompletionAttempt,
		"outcome", attempt.Outcome,
		"state", result.State,
		"latency_ms", attempt.Latency.Milliseconds(),
	)
	return true, nil
}

func (dispatcher *Dispatcher) RunLoop(ctx context.Context, pollInterval time.Duration) error {
	if pollInterval <= 0 {
		pollInterval = 500 * time.Millisecond
	}
	for {
		processed, err := dispatcher.ProcessOne(ctx)
		if err != nil && ctx.Err() == nil {
			dispatcher.logger().Error("trigger completion dispatcher iteration failed", "error", err)
		}
		if ctx.Err() != nil {
			return nil
		}
		if processed && err == nil {
			continue
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}
	}
}

func (dispatcher *Dispatcher) result(claim *state.TriggerCompletionClaim, attempt webhook.AttemptResult) state.TriggerCompletionResult {
	now := dispatcher.Now().UTC()
	result := state.TriggerCompletionResult{
		ResponseStatus: attempt.ResponseStatus,
		ErrorSummary:   attempt.ErrorSummary,
	}
	switch attempt.Outcome {
	case webhook.AttemptSucceeded:
		result.State = state.TriggerCompletionSucceeded
		result.CompletedAt = &now
	case webhook.AttemptRetry:
		if claim.Delivery.CompletionAttempt >= dispatcher.MaxAttempts {
			result.State = state.TriggerCompletionFailed
			result.ErrorSummary = "max_attempts_exceeded"
			result.CompletedAt = &now
			break
		}
		result.State = state.TriggerCompletionRetrying
		delay := webhook.RetryDelay(dispatcher.BackoffBase, dispatcher.BackoffMax, claim.Delivery.CompletionAttempt, claim.Delivery.ID)
		if attempt.RetryAt != nil && attempt.RetryAt.Sub(now) > delay {
			delay = attempt.RetryAt.Sub(now)
		}
		if delay > dispatcher.BackoffMax {
			delay = dispatcher.BackoffMax
		}
		if delay < 0 {
			delay = 0
		}
		result.NextAttemptAt = now.Add(delay)
	default:
		result.State = state.TriggerCompletionFailed
		result.CompletedAt = &now
	}
	return result
}

func (dispatcher *Dispatcher) applyDefaults() {
	if strings.TrimSpace(dispatcher.WorkerID) == "" {
		hostname, _ := os.Hostname()
		dispatcher.WorkerID = fmt.Sprintf("trigger-completion-%s-%d", firstNonEmpty(hostname, "dispatcher"), os.Getpid())
	}
	if dispatcher.LeaseTTL <= 0 {
		dispatcher.LeaseTTL = 30 * time.Second
	}
	if dispatcher.MaxAttempts <= 0 {
		dispatcher.MaxAttempts = 8
	}
	if dispatcher.BackoffBase <= 0 {
		dispatcher.BackoffBase = 5 * time.Second
	}
	if dispatcher.BackoffMax <= 0 {
		dispatcher.BackoffMax = 24 * time.Hour
	}
	if dispatcher.Now == nil {
		dispatcher.Now = time.Now
	}
}

func (dispatcher *Dispatcher) logger() *slog.Logger {
	if dispatcher.Logger != nil {
		return dispatcher.Logger
	}
	return slog.Default()
}
