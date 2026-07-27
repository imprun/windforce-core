package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

func TestPostgresTriggerStoreContract(t *testing.T) {
	dsn := os.Getenv("WINDFORCE_LITE_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("WINDFORCE_LITE_POSTGRES_TEST_DSN is not set")
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
