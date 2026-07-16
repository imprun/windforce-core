package webhook

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type dispatcherTestStore struct {
	Store
	claimed     *ClaimedDelivery
	claimErr    error
	completed   []DeliveryResult
	completeErr error
}

func (store *dispatcherTestStore) ClaimDelivery(_ context.Context, workerID string, leaseTTL time.Duration) (*ClaimedDelivery, error) {
	if store.claimErr != nil {
		return nil, store.claimErr
	}
	if store.claimed == nil {
		return nil, ErrNoPendingDelivery
	}
	claimed := store.claimed
	store.claimed = nil
	claimed.Lease = DeliveryLease{DeliveryID: claimed.Delivery.ID, WorkerID: workerID, Attempt: claimed.Delivery.Attempt, ExpiresAt: time.Now().Add(leaseTTL)}
	return claimed, nil
}

func (store *dispatcherTestStore) CompleteDelivery(_ context.Context, _ DeliveryLease, result DeliveryResult) error {
	store.completed = append(store.completed, result)
	return store.completeErr
}

type dispatcherTestSender struct {
	result AttemptResult
}

func (sender dispatcherTestSender) Send(_ context.Context, _ *ClaimedDelivery) AttemptResult {
	return sender.result
}

func TestDispatcherMapsAttemptsToDeliveryState(t *testing.T) {
	now := time.Date(2026, 7, 16, 16, 0, 0, 0, time.UTC)
	status := 503
	tests := []struct {
		name        string
		attempt     int
		sender      AttemptResult
		wantState   DeliveryState
		wantRetryAt time.Time
		wantError   string
	}{
		{name: "success", attempt: 1, sender: AttemptResult{Outcome: AttemptSucceeded, ResponseStatus: &status}, wantState: DeliverySucceeded},
		{name: "terminal", attempt: 1, sender: AttemptResult{Outcome: AttemptTerminal, ErrorSummary: "http_400"}, wantState: DeliveryFailed, wantError: "http_400"},
		{name: "retry after", attempt: 1, sender: AttemptResult{Outcome: AttemptRetry, RetryAt: timePointer(now.Add(2 * time.Minute)), ErrorSummary: "http_429"}, wantState: DeliveryRetrying, wantRetryAt: now.Add(2 * time.Minute), wantError: "http_429"},
		{name: "max attempts", attempt: 8, sender: AttemptResult{Outcome: AttemptRetry, ErrorSummary: "network_error"}, wantState: DeliveryFailed, wantError: "max_attempts_exceeded"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claimed := testClaimedDelivery(t, "https://hooks.example.test/private?token=secret")
			claimed.Delivery.Attempt = test.attempt
			store := &dispatcherTestStore{claimed: claimed}
			var logs bytes.Buffer
			dispatcher := Dispatcher{
				Store:       store,
				Sender:      dispatcherTestSender{result: test.sender},
				WorkerID:    "dispatcher-a",
				LeaseTTL:    time.Minute,
				MaxAttempts: 8,
				BackoffBase: 5 * time.Second,
				BackoffMax:  24 * time.Hour,
				Now:         func() time.Time { return now },
				Logger:      slog.New(slog.NewJSONHandler(&logs, nil)),
			}
			processed, err := dispatcher.ProcessOne(context.Background())
			if err != nil || !processed {
				t.Fatalf("ProcessOne() = %v, %v", processed, err)
			}
			if len(store.completed) != 1 {
				t.Fatalf("completed = %#v", store.completed)
			}
			result := store.completed[0]
			if result.State != test.wantState {
				t.Fatalf("state = %q, want %q", result.State, test.wantState)
			}
			if !test.wantRetryAt.IsZero() && !result.NextAttemptAt.Equal(test.wantRetryAt) {
				t.Fatalf("next attempt = %v, want %v", result.NextAttemptAt, test.wantRetryAt)
			}
			if test.wantError != "" && (result.ErrorSummary == nil || *result.ErrorSummary != test.wantError) {
				t.Fatalf("error summary = %v, want %q", result.ErrorSummary, test.wantError)
			}
			for _, protected := range []string{"hooks.example.test", "/private", "token=secret", claimed.Subscription.SigningSecret} {
				if strings.Contains(logs.String(), protected) {
					t.Fatalf("log contains protected value %q: %s", protected, logs.String())
				}
			}
		})
	}
}

func TestDispatcherReturnsNoWorkAndCompletionErrors(t *testing.T) {
	dispatcher := Dispatcher{Store: &dispatcherTestStore{}, Sender: dispatcherTestSender{}, WorkerID: "dispatcher-a"}
	processed, err := dispatcher.ProcessOne(context.Background())
	if err != nil || processed {
		t.Fatalf("empty ProcessOne() = %v, %v", processed, err)
	}
	claimed := testClaimedDelivery(t, "https://hooks.example.test")
	store := &dispatcherTestStore{claimed: claimed, completeErr: errors.New("database unavailable")}
	dispatcher.Store = store
	dispatcher.Sender = dispatcherTestSender{result: AttemptResult{Outcome: AttemptSucceeded}}
	processed, err = dispatcher.ProcessOne(context.Background())
	if !processed || err == nil {
		t.Fatalf("completion failure ProcessOne() = %v, %v", processed, err)
	}
}

func TestRetryDelayIsDeterministicJitteredAndCapped(t *testing.T) {
	base := 10 * time.Second
	maximum := time.Minute
	first := RetryDelay(base, maximum, 1, "delivery-a")
	if first < base/2 || first > base {
		t.Fatalf("first delay = %v", first)
	}
	if repeat := RetryDelay(base, maximum, 1, "delivery-a"); repeat != first {
		t.Fatalf("repeat delay = %v, want %v", repeat, first)
	}
	capped := RetryDelay(base, maximum, 100, "delivery-a")
	if capped < maximum/2 || capped > maximum {
		t.Fatalf("capped delay = %v", capped)
	}
	if other := RetryDelay(base, maximum, 1, "delivery-b"); other == first {
		t.Fatalf("different delivery has identical jitter %v", other)
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}
