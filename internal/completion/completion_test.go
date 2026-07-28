package completion

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/state"
	"github.com/imprun/windforce-core/internal/webhook"
)

func TestCallbackSenderSignsCompletionEnvelopeWithoutJobIdentity(t *testing.T) {
	received := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		timestamp := r.Header.Get(webhook.HeaderTimestamp)
		if !webhook.VerifySignature("callback-secret", timestamp, body, r.Header.Get(webhook.HeaderSignature)) {
			t.Error("completion signature did not verify")
		}
		if strings.Contains(string(body), "job") {
			t.Errorf("completion envelope leaked job identity: %s", body)
		}
		var envelope Envelope
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Error(err)
		} else if envelope.RunID != "run-1" || envelope.DeliveryID != "source-delivery-1" {
			t.Errorf("completion envelope = %#v", envelope)
		}
		received <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	claim := &state.TriggerCompletionClaim{
		Delivery: state.TriggerDelivery{
			ID:            "trd-1",
			WorkspaceID:   "workspace-a",
			TriggerID:     "trigger-a",
			DeliveryID:    "source-delivery-1",
			CorrelationID: "correlation-1",
			Completion: state.TriggerCompletionPolicy{
				Mode:     state.TriggerCompletionModeCallback,
				Callback: &state.TriggerCompletionCallback{Endpoint: server.URL},
			},
		},
		Run: state.Run{
			ID:        "run-1",
			App:       "orders",
			Action:    "ingest",
			State:     state.RunSucceeded,
			Output:    json.RawMessage(`{"ok":true}`),
			UpdatedAt: time.Date(2026, 7, 28, 5, 0, 0, 0, time.UTC),
		},
		Trigger: state.TriggerDefinition{
			ID:           "trigger-a",
			SecretConfig: json.RawMessage(`{"completion":{"signing_secret":"callback-secret"}}`),
		},
	}
	result := (CallbackSender{
		Policy: webhook.EgressPolicy{AllowInsecureLoopback: true},
		Now:    func() time.Time { return time.Date(2026, 7, 28, 5, 0, 0, 0, time.UTC) },
	}).Send(context.Background(), claim)
	if result.Outcome != webhook.AttemptSucceeded || result.ResponseStatus == nil || *result.ResponseStatus != http.StatusNoContent {
		t.Fatalf("attempt = %#v", result)
	}
	select {
	case <-received:
	default:
		t.Fatal("callback was not received")
	}
}

type dispatcherTestStore struct {
	claim  *state.TriggerCompletionClaim
	result state.TriggerCompletionResult
}

func (store *dispatcherTestStore) ClaimTriggerCompletion(context.Context, string, time.Duration) (*state.TriggerCompletionClaim, error) {
	return store.claim, nil
}

func (store *dispatcherTestStore) CompleteTriggerCompletion(_ context.Context, _ state.TriggerCompletionLease, result state.TriggerCompletionResult) error {
	store.result = result
	return nil
}

type dispatcherTestSender struct {
	result webhook.AttemptResult
}

func (sender dispatcherTestSender) Send(context.Context, *state.TriggerCompletionClaim) webhook.AttemptResult {
	return sender.result
}

func TestDispatcherPersistsRetryPolicy(t *testing.T) {
	now := time.Date(2026, 7, 28, 5, 0, 0, 0, time.UTC)
	store := &dispatcherTestStore{claim: &state.TriggerCompletionClaim{
		Delivery: state.TriggerDelivery{
			ID:                "trd-1",
			Completion:        state.TriggerCompletionPolicy{Mode: state.TriggerCompletionModeCallback},
			CompletionAttempt: 1,
		},
		Lease: state.TriggerCompletionLease{DeliveryID: "trd-1"},
	}}
	dispatcher := &Dispatcher{
		Store:       store,
		Callback:    dispatcherTestSender{result: webhook.AttemptResult{Outcome: webhook.AttemptRetry, ErrorSummary: "network_error"}},
		Now:         func() time.Time { return now },
		BackoffBase: time.Second,
		BackoffMax:  time.Minute,
		MaxAttempts: 3,
	}
	processed, err := dispatcher.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("processed = %t, error = %v", processed, err)
	}
	if store.result.State != state.TriggerCompletionRetrying ||
		!store.result.NextAttemptAt.After(now) ||
		store.result.ErrorSummary != "network_error" {
		t.Fatalf("completion result = %#v", store.result)
	}
}
