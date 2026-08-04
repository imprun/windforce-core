package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

func TestPostgresTriggerStoreContract(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	ctx := context.Background()
	store := openIsolatedPostgresCatalogStore(t, dsn)
	store.ConfigureInputCrypto("postgres-trigger-test-secret-key", "")
	created, err := store.CreateTrigger(ctx, TriggerDefinition{
		WorkspaceID:  "workspace-a",
		Name:         "Incoming",
		Kind:         "webhook",
		AppKey:       "demo",
		ActionKey:    "run",
		Config:       json.RawMessage(`{}`),
		SecretConfig: json.RawMessage(`{"secret":"postgres-trigger-secret"}`),
	}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	var encrypted []byte
	if err := store.pool.QueryRow(ctx,
		`SELECT secret_config_encrypted FROM trigger_definition WHERE id=$1`,
		created.ID,
	).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("postgres-trigger-secret")) {
		t.Fatalf("secret stored in plaintext: %s", encrypted)
	}
	loaded, err := store.GetTrigger(ctx, "workspace-a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(loaded.SecretConfig, []byte("postgres-trigger-secret")) {
		t.Fatalf("loaded trigger = %#v", loaded)
	}
	duplicate := loaded
	duplicate.ID = ""
	duplicate.Name = "incoming"
	duplicate.SecretConfig = json.RawMessage(`{"secret":"different"}`)
	if _, err := store.CreateTrigger(ctx, duplicate, "tester"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate name error = %v", err)
	}

	enabled, err := store.SetTriggerEnabled(ctx, "workspace-a", created.ID, true, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.Enabled {
		t.Fatal("trigger was not enabled")
	}
	scheduledFor := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	first, err := store.UpsertTriggerDelivery(ctx, TriggerDelivery{
		WorkspaceID:   "workspace-a",
		TriggerID:     created.ID,
		DeliveryID:    "delivery-1",
		CorrelationID: "correlation-1",
		State:         "retryable",
		ScheduledFor:  &scheduledFor,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.UpsertTriggerDelivery(ctx, TriggerDelivery{
		WorkspaceID:   "workspace-a",
		TriggerID:     created.ID,
		DeliveryID:    "delivery-1",
		CorrelationID: "correlation-1",
		State:         "admitted",
		RunID:         "run-1",
		ScheduledFor:  &scheduledFor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.Attempt != first.Attempt+1 || second.RunID != "run-1" {
		t.Fatalf("delivery update = first:%#v second:%#v", first, second)
	}
	deliveries, err := store.ListTriggerDeliveries(ctx, "workspace-a", created.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 1 ||
		deliveries[0].CorrelationID != "correlation-1" ||
		deliveries[0].ScheduledFor == nil ||
		!deliveries[0].ScheduledFor.Equal(scheduledFor) {
		t.Fatalf("deliveries = %#v", deliveries)
	}
	audit, err := store.ListTriggerAudit(ctx, "workspace-a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 2 || audit[0].Kind != "created" || audit[1].Kind != "enabled" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestPostgresTriggerCompletionContract(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	ctx := context.Background()
	store := openIsolatedPostgresCatalogStore(t, dsn)
	store.ConfigureInputCrypto("postgres-trigger-completion-secret-key", "")
	now := time.Now().UTC().Truncate(time.Microsecond)
	store.leaseNow = func() time.Time { return now }
	trigger, err := store.CreateTrigger(ctx, TriggerDefinition{
		WorkspaceID: "workspace-a",
		Name:        "Completion",
		Kind:        "webhook",
		AppKey:      "demo",
		ActionKey:   "run",
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
	if err := store.CreateRunAndEnqueue(ctx, Run{
		ID:         "run-completion-1",
		App:        "demo",
		Action:     "run",
		State:      RunSucceeded,
		Deployment: contract.Deployment{Workspace: "workspace-a"},
		Output:     json.RawMessage(`{"ok":true}`),
	}, Job{
		ID:    "job-completion-1",
		RunID: "run-completion-1",
		State: JobSucceeded,
		Payload: JobPayload{
			Workspace: "workspace-a",
			App:       "demo",
			Action:    "run",
		},
	}); err != nil {
		t.Fatal(err)
	}
	delivery, err := store.UpsertTriggerDelivery(ctx, TriggerDelivery{
		WorkspaceID: "workspace-a",
		TriggerID:   trigger.ID,
		DeliveryID:  "delivery-completion-1",
		State:       "admitted",
		RunID:       "run-completion-1",
		Completion:  trigger.Completion,
	})
	if err != nil {
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
		claimed.Trigger.DeletedAt == nil ||
		claimed.Run.ID != "run-completion-1" ||
		!bytes.Contains(claimed.Trigger.SecretConfig, []byte("callback-secret")) {
		t.Fatalf("claim = %#v", claimed)
	}
	status := 204
	if err := store.CompleteTriggerCompletion(ctx, claimed.Lease, TriggerCompletionResult{
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
