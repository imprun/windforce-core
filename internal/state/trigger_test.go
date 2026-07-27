package state

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalTriggerLifecycleEncryptsSecretsAndAudits(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewLocalStore(path)
	store.SecretKey = "test-trigger-secret-key"
	if _, err := store.CreateWorkspace(ctx, "ws", "Workspace", "tester"); err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateTrigger(ctx, TriggerDefinition{
		WorkspaceID: "ws",
		Name:        "incoming",
		Kind:        "webhook",
		Enabled:     false,
		AppKey:      "demo",
		ActionKey:   "run",
		Config:      json.RawMessage(`{}`),
		SecretConfig: json.RawMessage(
			`{"secret":"must-not-appear-at-rest"}`,
		),
	}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(created.SecretConfig), "must-not-appear-at-rest") {
		t.Fatalf("decrypted trigger = %#v", created)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "must-not-appear-at-rest") {
		t.Fatal("trigger secret was persisted in plaintext")
	}

	enabled, err := store.SetTriggerEnabled(ctx, "ws", created.ID, true, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.Enabled {
		t.Fatal("trigger was not enabled")
	}
	audit, err := store.ListTriggerAudit(ctx, "ws", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 2 || audit[0].Kind != "created" || audit[1].Kind != "enabled" {
		t.Fatalf("audit = %#v", audit)
	}
	for _, item := range audit {
		if strings.Contains(item.Detail, "must-not-appear-at-rest") {
			t.Fatalf("secret leaked into audit: %#v", item)
		}
	}
}

func TestLocalTriggerNameIsUniquePerWorkspace(t *testing.T) {
	ctx := context.Background()
	store := NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	store.SecretKey = "test-trigger-secret-key"
	if _, err := store.CreateWorkspace(ctx, "ws", "Workspace", "tester"); err != nil {
		t.Fatal(err)
	}
	base := TriggerDefinition{
		WorkspaceID: "ws",
		Name:        "Nightly",
		Kind:        "schedule",
		AppKey:      "demo",
		ActionKey:   "run",
		Config:      json.RawMessage(`{}`),
	}
	if _, err := store.CreateTrigger(ctx, base, "tester"); err != nil {
		t.Fatal(err)
	}
	base.ID = ""
	base.Name = "nightly"
	if _, err := store.CreateTrigger(ctx, base, "tester"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate name error = %v", err)
	}
}
