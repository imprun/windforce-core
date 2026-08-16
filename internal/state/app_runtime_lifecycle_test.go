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

func TestAppRuntimeTombstoneBlocksClaimButAllowsExistingAttemptUntilRevoke(t *testing.T) {
	exerciseAppRuntimeTombstoneBlocksClaimButAllowsExistingAttemptUntilRevoke(t, NewLocalStore(filepath.Join(t.TempDir(), "state.json")))
}

func TestPostgresAppRuntimeTombstoneBlocksClaimButAllowsExistingAttemptUntilRevoke(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	exerciseAppRuntimeTombstoneBlocksClaimButAllowsExistingAttemptUntilRevoke(t, openIsolatedPostgresCatalogStore(t, dsn))
}

func exerciseAppRuntimeTombstoneBlocksClaimButAllowsExistingAttemptUntilRevoke(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	access := contract.RuntimeAccess{WriteVariables: []contract.RuntimeVariableWriteTarget{{
		RuntimeConfigTarget: contract.RuntimeConfigTarget{Scope: contract.RuntimeConfigScopeApp, Path: "session"},
		Storage:             contract.RuntimeVariableStoragePlain,
	}}}
	running := enqueueRuntimeConfigJob(t, store, access)
	if _, err := store.SetAppRuntimeLifecycle(ctx, SetAppRuntimeLifecycleRequest{
		WorkspaceID: "ws-a", AppKey: "publisher", State: AppRuntimeTombstoned, Actor: "operator", Reason: "retiring",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MutateRuntimeVariable(ctx, RuntimeVariableMutationRequest{
		WorkspaceID: "ws-a", AppKey: "publisher", Path: "session", Value: "still-running",
		OperationID: "tombstone-write", RequestFingerprint: "fp-1", JobID: running.ID, Attempt: running.Attempt,
	}); err != nil {
		t.Fatalf("existing attempt write after tombstone: %v", err)
	}

	queuedRun := NewRun("windforce", NewID("run"), "publisher", "run", running.Payload.PinnedDeployment(), json.RawMessage(`{}`))
	queuedJob := NewActionJob(queuedRun, json.RawMessage(`{}`))
	if err := store.CreateRunAndEnqueue(ctx, queuedRun, queuedJob); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ClaimJob(ctx, "worker-b", time.Minute); !errors.Is(err, ErrNoQueuedJob) {
		t.Fatalf("claim after tombstone error = %v, want no queued job", err)
	}

	if _, err := store.SetAppRuntimeLifecycle(ctx, SetAppRuntimeLifecycleRequest{
		WorkspaceID: "ws-a", AppKey: "publisher", State: AppRuntimeRevoked, Actor: "security", Reason: "incident",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := store.MutateRuntimeVariable(ctx, RuntimeVariableMutationRequest{
		WorkspaceID: "ws-a", AppKey: "publisher", Path: "session", Value: "blocked",
		OperationID: "revoked-write", RequestFingerprint: "fp-2", JobID: running.ID, Attempt: running.Attempt,
	})
	var runtimeErr *RuntimeConfigError
	if !errors.As(err, &runtimeErr) || runtimeErr.Code != RuntimeConfigCodeForbidden {
		t.Fatalf("write after revoke error = %v", err)
	}
	job, _, found, err := store.GetJob(ctx, "ws-a", running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !found || job.CanceledBy == nil || *job.CanceledBy != "security" {
		t.Fatalf("running Job cancellation = %#v, found=%v", job, found)
	}
}

func TestPurgeAppRuntimeConfigRequiresLifecycleAndLeaseSafety(t *testing.T) {
	exercisePurgeAppRuntimeConfigRequiresLifecycleAndLeaseSafety(t, NewLocalStore(filepath.Join(t.TempDir(), "state.json")))
}

func TestPostgresPurgeAppRuntimeConfigRequiresLifecycleAndLeaseSafety(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	exercisePurgeAppRuntimeConfigRequiresLifecycleAndLeaseSafety(t, openIsolatedPostgresCatalogStore(t, dsn))
}

func exercisePurgeAppRuntimeConfigRequiresLifecycleAndLeaseSafety(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	running := enqueueRuntimeConfigJob(t, store, contract.RuntimeAccess{})
	if err := store.SetVariable(ctx, "ws-a", "publisher", "plain", "value", false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetAppRuntimeLifecycle(ctx, SetAppRuntimeLifecycleRequest{
		WorkspaceID: "ws-a", AppKey: "publisher", State: AppRuntimeTombstoned, Actor: "operator",
	}); err != nil {
		t.Fatal(err)
	}
	request := PurgeAppRuntimeConfigRequest{WorkspaceID: "ws-a", AppKey: "publisher", Actor: "operator", Reason: "retired"}
	if err := store.PurgeAppRuntimeConfig(ctx, request); !errors.Is(err, ErrConflict) {
		t.Fatalf("purge with valid lease error = %v", err)
	}
	request.Force = true
	if err := store.PurgeAppRuntimeConfig(ctx, request); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.GetVariableScoped(ctx, "ws-a", contract.RuntimeConfigScopeApp, "publisher", "plain"); err != nil || found {
		t.Fatalf("purged Variable found=%v err=%v", found, err)
	}
	audits, err := store.ListAppRuntimeLifecycleAudit(ctx, "ws-a", "publisher")
	if err != nil || len(audits) != 2 || !audits[1].Purged || !audits[1].Forced {
		t.Fatalf("purge audits = %#v err=%v running=%s", audits, err, running.ID)
	}
}

func TestListAppRuntimeLifecyclesReturnsOnlyPersistedRowsInStableOrder(t *testing.T) {
	exerciseListAppRuntimeLifecyclesReturnsOnlyPersistedRowsInStableOrder(t, NewLocalStore(filepath.Join(t.TempDir(), "state.json")))
}

func TestPostgresListAppRuntimeLifecyclesReturnsOnlyPersistedRowsInStableOrder(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	exerciseListAppRuntimeLifecyclesReturnsOnlyPersistedRowsInStableOrder(t, openIsolatedPostgresCatalogStore(t, dsn))
}

func exerciseListAppRuntimeLifecyclesReturnsOnlyPersistedRowsInStableOrder(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	for _, appKey := range []string{"zeta", "alpha"} {
		if _, err := store.SetAppRuntimeLifecycle(ctx, SetAppRuntimeLifecycleRequest{
			WorkspaceID: "default", AppKey: appKey, State: AppRuntimeTombstoned, Actor: "operator",
		}); err != nil {
			t.Fatal(err)
		}
	}
	lifecycles, err := store.ListAppRuntimeLifecycles(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	if len(lifecycles) != 2 || lifecycles[0].AppKey != "alpha" || lifecycles[1].AppKey != "zeta" {
		t.Fatalf("lifecycles = %#v", lifecycles)
	}
}
