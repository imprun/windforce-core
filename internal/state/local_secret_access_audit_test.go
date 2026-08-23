package state

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalSecretAccessAuditAppendIsIdempotentByID(t *testing.T) {
	store := NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	record := SecretAccessAudit{
		ID:          "secret-access-stable",
		WorkspaceID: "ws-a",
		JobID:       "job-a",
		Attempt:     1,
		AppKey:      "app-a",
		ActionKey:   "action-a",
		Path:        "secrets/token",
		Source:      "sdk",
		CreatedAt:   time.Date(2026, time.August, 23, 0, 0, 0, 0, time.UTC),
	}
	if err := store.AppendSecretAccessAudit(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendSecretAccessAudit(context.Background(), record); err != nil {
		t.Fatalf("idempotent append error = %v", err)
	}
	records, err := store.ListSecretAccessAudits(context.Background(), "ws-a", "job-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("stored audits = %d, want 1", len(records))
	}

	conflicting := record
	conflicting.Path = "secrets/other"
	if err := store.AppendSecretAccessAudit(context.Background(), conflicting); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting append error = %v, want ErrConflict", err)
	}
}
