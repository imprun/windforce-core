package state

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

func TestLocalTriggerCompletionClaimPinsPolicyAndSurvivesTriggerDeletion(t *testing.T) {
	ctx := context.Background()
	store := NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	store.ConfigureInputCrypto("test-completion-secret", "")
	now := time.Date(2026, 7, 28, 5, 0, 0, 0, time.UTC)
	store.leaseNow = func() time.Time { return now }
	trigger, err := store.CreateTrigger(ctx, TriggerDefinition{
		WorkspaceID: "workspace-a",
		Name:        "Partner orders",
		Kind:        "webhook",
		AppKey:      "orders",
		ActionKey:   "ingest",
		Config:      json.RawMessage(`{}`),
		Completion: TriggerCompletionPolicy{
			Mode:     TriggerCompletionModeCallback,
			Callback: &TriggerCompletionCallback{Endpoint: "https://callback.example.test/completed"},
		},
		SecretConfig: json.RawMessage(`{"secret":"source-secret","completion":{"signing_secret":"callback-secret"}}`),
	}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := store.UpsertTriggerDelivery(ctx, TriggerDelivery{
		WorkspaceID:   "workspace-a",
		TriggerID:     trigger.ID,
		DeliveryID:    "partner-delivery-1",
		CorrelationID: "correlation-1",
		State:         "admitted",
		RunID:         "run-1",
		Completion:    trigger.Completion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.update(ctx, func(snapshot *Snapshot, _ time.Time) error {
		snapshot.Runs["run-1"] = Run{
			ID:         "run-1",
			App:        "orders",
			Action:     "ingest",
			State:      RunSucceeded,
			Deployment: contract.Deployment{Workspace: "workspace-a"},
			UpdatedAt:  now,
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteTrigger(ctx, "workspace-a", trigger.ID, "tester"); err != nil {
		t.Fatal(err)
	}

	claimed, err := store.ClaimTriggerCompletion(ctx, "completion-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Delivery.ID != delivery.ID ||
		claimed.Delivery.Completion.Mode != TriggerCompletionModeCallback ||
		claimed.Trigger.SecretConfig == nil ||
		claimed.Run.ID != "run-1" {
		t.Fatalf("claimed = %#v", claimed)
	}
	retryAt := now.Add(-time.Second)
	if err := store.CompleteTriggerCompletion(ctx, claimed.Lease, TriggerCompletionResult{
		State:         TriggerCompletionRetrying,
		NextAttemptAt: retryAt,
		ErrorSummary:  "temporary failure",
	}); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := store.ClaimTriggerCompletion(ctx, "completion-b", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed.Delivery.CompletionAttempt != claimed.Delivery.CompletionAttempt+1 {
		t.Fatalf("reclaimed attempt = %d, first = %d", reclaimed.Delivery.CompletionAttempt, claimed.Delivery.CompletionAttempt)
	}
	if err := store.CompleteTriggerCompletion(ctx, claimed.Lease, TriggerCompletionResult{
		State: TriggerCompletionFailed,
	}); !errors.Is(err, ErrInvalidLease) {
		t.Fatalf("stale lease error = %v", err)
	}
	status := 204
	if err := store.CompleteTriggerCompletion(ctx, reclaimed.Lease, TriggerCompletionResult{
		State:          TriggerCompletionSucceeded,
		ResponseStatus: &status,
	}); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListTriggerDeliveries(ctx, "workspace-a", trigger.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 ||
		items[0].CompletionState != TriggerCompletionSucceeded ||
		items[0].CompletionResponseStatus == nil ||
		*items[0].CompletionResponseStatus != status {
		t.Fatalf("deliveries = %#v", items)
	}
}

func TestLocalTriggerCompletionPollBecomesAvailableWithoutExternalClaim(t *testing.T) {
	ctx := context.Background()
	store := NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	store.ConfigureInputCrypto("test-completion-secret", "")
	now := time.Date(2026, 7, 28, 5, 0, 0, 0, time.UTC)
	store.leaseNow = func() time.Time { return now }
	if _, err := store.UpsertTriggerDelivery(ctx, TriggerDelivery{
		WorkspaceID: "workspace-a",
		TriggerID:   "trigger-a",
		DeliveryID:  "delivery-a",
		State:       "admitted",
		RunID:       "run-a",
		Completion:  TriggerCompletionPolicy{Mode: TriggerCompletionModePoll},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.update(ctx, func(snapshot *Snapshot, _ time.Time) error {
		snapshot.Runs["run-a"] = Run{
			ID:         "run-a",
			State:      RunFailed,
			Deployment: contract.Deployment{Workspace: "workspace-a"},
			UpdatedAt:  now,
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if claimed, err := store.ClaimTriggerCompletion(ctx, "completion-a", time.Minute); claimed != nil || !errors.Is(err, ErrNoCompletion) {
		t.Fatalf("claim = %#v, error = %v", claimed, err)
	}
	items, err := store.ListTriggerDeliveries(ctx, "workspace-a", "trigger-a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].CompletionState != TriggerCompletionAvailable || items[0].CompletionCompletedAt == nil {
		t.Fatalf("deliveries = %#v", items)
	}
}
