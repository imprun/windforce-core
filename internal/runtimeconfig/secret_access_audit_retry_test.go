package runtimeconfig

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/imprun/windforce-core/internal/state"
)

type transientSecretAccessAuditStore struct {
	*state.LocalStore
	failures int
	calls    int
	ids      []string
}

func (s *transientSecretAccessAuditStore) AppendSecretAccessAudit(ctx context.Context, record state.SecretAccessAudit) error {
	s.calls++
	s.ids = append(s.ids, record.ID)
	if s.calls <= s.failures {
		return state.ErrLockTimeout
	}
	return s.LocalStore.AppendSecretAccessAudit(ctx, record)
}

func TestSecretAccessAuditRetriesOneTransientLocalLockTimeout(t *testing.T) {
	store := &transientSecretAccessAuditStore{
		LocalStore: state.NewLocalStore(filepath.Join(t.TempDir(), "state.json")),
		failures:   1,
	}
	job := state.Job{
		ID:      "job-a",
		Attempt: 2,
		Payload: state.JobPayload{Workspace: "ws-a", App: "app-a", Action: "action-a"},
	}
	ctx := withSecretAccessAudit(context.Background(), job, "sdk")

	if err := (&Resolver{Audit: store}).recordSecretAccess(ctx, "secrets/token"); err != nil {
		t.Fatalf("recordSecretAccess() error = %v", err)
	}
	if store.calls != 2 {
		t.Fatalf("AppendSecretAccessAudit calls = %d, want 2", store.calls)
	}
	if store.ids[0] == "" || store.ids[0] != store.ids[1] {
		t.Fatalf("audit retry IDs = %#v, want one stable non-empty ID", store.ids)
	}
	records, err := store.ListSecretAccessAudits(context.Background(), "ws-a", "job-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ID != store.ids[0] {
		t.Fatalf("stored audits = %#v, want one retried record", records)
	}
}

func TestSecretAccessAuditStillFailsClosedAfterRetryBudget(t *testing.T) {
	store := &transientSecretAccessAuditStore{
		LocalStore: state.NewLocalStore(filepath.Join(t.TempDir(), "state.json")),
		failures:   secretAccessAuditMaxAttempts,
	}
	job := state.Job{
		ID:      "job-a",
		Attempt: 1,
		Payload: state.JobPayload{Workspace: "ws-a", App: "app-a", Action: "action-a"},
	}
	ctx := withSecretAccessAudit(context.Background(), job, "sdk")

	err := (&Resolver{Audit: store}).recordSecretAccess(ctx, "secrets/token")
	if !errors.Is(err, state.ErrLockTimeout) {
		t.Fatalf("recordSecretAccess() error = %v, want ErrLockTimeout", err)
	}
	if store.calls != secretAccessAuditMaxAttempts {
		t.Fatalf("AppendSecretAccessAudit calls = %d, want %d", store.calls, secretAccessAuditMaxAttempts)
	}
}
