package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

type workerControlTestStore interface {
	Store
	WorkerControlStore
}

func TestLocalWorkerControlStoreContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewLocalStore(path)
	exerciseWorkerControlStore(t, store)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "wfr_raw-secret") {
		t.Fatalf("raw worker token was persisted: %s", raw)
	}
	reloaded := NewLocalStore(path)
	credential, err := reloaded.GetWorkerCredentialByTokenHash(context.Background(), HashBearerToken("wfr_raw-secret"))
	if err != nil || credential.Group != "group-a" {
		t.Fatalf("reloaded credential = %#v, %v", credential, err)
	}
	runState, err := reloaded.GetWorkerGroupRunState(context.Background(), "group-a")
	if err != nil || runState.Revision != 1 || runState.State != WorkerGroupDraining {
		t.Fatalf("reloaded run state = %#v, %v", runState, err)
	}
}

func TestPostgresWorkerControlStoreContract(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	store := openIsolatedPostgresCatalogStore(t, dsn)
	exerciseWorkerControlStore(t, store)
	reopened, err := OpenPostgresStore(context.Background(), store.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	runState, err := reopened.GetWorkerGroupRunState(context.Background(), "group-a")
	if err != nil || runState.Revision != 1 || runState.State != WorkerGroupDraining {
		t.Fatalf("reopened run state = %#v, err=%v", runState, err)
	}
	credential, err := reopened.GetWorkerCredentialByTokenHash(context.Background(), HashBearerToken("wfr_raw-secret"))
	if err != nil || credential.Status != WorkerCredentialRevoked || credential.Generation != 1 {
		t.Fatalf("reopened credential = %#v, err=%v", credential, err)
	}
}

func exerciseWorkerControlStore(t *testing.T, store workerControlTestStore) {
	t.Helper()
	ctx := context.Background()
	expiresAt := time.Now().UTC().Add(time.Hour)
	if _, _, err := store.CreateWorkerCredential(ctx, CreateWorkerCredentialRequest{
		Group: "group-a", ExpectedGeneration: 0, Labels: []string{"linux"},
		TokenHash: HashBearerToken("wfr_unscoped"), OperationID: "op-unscoped", RequestFingerprint: "unscoped",
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("unscoped credential err=%v, want ErrInvalidState", err)
	}
	emptyLabels, replayed, err := store.CreateWorkerCredential(ctx, CreateWorkerCredentialRequest{
		Group: "group-empty", ExpectedGeneration: 0, WorkspaceIDs: []string{"workspace-a"},
		TokenHash: HashBearerToken("wfr_empty-labels"), OperationID: "op-empty-labels",
		RequestFingerprint: "empty-labels", Actor: "tester",
	})
	if err != nil || replayed {
		t.Fatalf("empty-label credential = %#v, replayed=%v, err=%v", emptyLabels, replayed, err)
	}
	if emptyLabels.Labels == nil || len(emptyLabels.Labels) != 0 {
		t.Fatalf("empty-label credential labels = %#v, want non-nil empty slice", emptyLabels.Labels)
	}
	created, replayed, err := store.CreateWorkerCredential(ctx, CreateWorkerCredentialRequest{
		Group: "group-a", ExpectedGeneration: 0, WorkspaceIDs: []string{"workspace-a"}, Labels: []string{"linux", "arm64"},
		ExpiresAt: &expiresAt, TokenHash: HashBearerToken("wfr_raw-secret"), OperationID: "op-create-1",
		RequestFingerprint: "create-fingerprint-1", Actor: "tester",
	})
	if err != nil || replayed || created.Generation != 1 || created.Status != WorkerCredentialActive {
		t.Fatalf("created = %#v, replayed=%v, err=%v", created, replayed, err)
	}
	replay, replayed, err := store.CreateWorkerCredential(ctx, CreateWorkerCredentialRequest{
		Group: "group-a", ExpectedGeneration: 0, WorkspaceIDs: []string{"workspace-a"}, Labels: []string{"linux", "arm64"},
		ExpiresAt: &expiresAt, TokenHash: HashBearerToken("wfr_unused-retry"), OperationID: "op-create-1",
		RequestFingerprint: "create-fingerprint-1", Actor: "tester",
	})
	if err != nil || !replayed || replay.ID != created.ID || replay.TokenHash != created.TokenHash {
		t.Fatalf("replay = %#v, replayed=%v, err=%v", replay, replayed, err)
	}
	if _, _, err := store.CreateWorkerCredential(ctx, CreateWorkerCredentialRequest{
		Group: "group-a", ExpectedGeneration: 0, WorkspaceIDs: []string{"workspace-b"}, Labels: []string{"linux"},
		TokenHash: HashBearerToken("wfr_conflict"), OperationID: "op-create-1", RequestFingerprint: "different",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting replay err=%v, want ErrConflict", err)
	}
	second, replayed, err := store.CreateWorkerCredential(ctx, CreateWorkerCredentialRequest{
		Group: "group-a", ExpectedGeneration: 1, WorkspaceIDs: []string{"workspace-a"}, Labels: []string{"linux", "arm64"},
		TokenHash: HashBearerToken("wfr_generation-2"), OperationID: "op-create-2",
		RequestFingerprint: "create-fingerprint-2", Actor: "tester",
	})
	if err != nil || replayed || second.Generation != 2 {
		t.Fatalf("second = %#v, replayed=%v, err=%v", second, replayed, err)
	}
	if _, _, err := store.CreateWorkerCredential(ctx, CreateWorkerCredentialRequest{
		Group: "group-a", ExpectedGeneration: 1, WorkspaceIDs: []string{"workspace-a"}, Labels: []string{"linux"},
		TokenHash: HashBearerToken("wfr_stale"), OperationID: "op-stale", RequestFingerprint: "stale",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale generation err=%v, want ErrConflict", err)
	}

	ownedWorker := WorkerRecord{
		ID: "owned-worker", Group: "group-a", EngineVersion: "v0.9.2", BuildRevision: "abcdef123456",
		Labels: []string{"linux", "arm64"}, Slots: 1,
		Status: WorkerStatusActive, CredentialID: created.ID, CredentialGeneration: created.Generation,
	}
	if err := store.RegisterWorker(ctx, ownedWorker); err != nil {
		t.Fatalf("register owned worker: %v", err)
	}
	if err := store.RegisterWorker(ctx, ownedWorker); err != nil {
		t.Fatalf("reregister worker with same owner: %v", err)
	}
	storedWorker, err := store.GetWorker(ctx, ownedWorker.ID)
	if err != nil || storedWorker.EngineVersion != ownedWorker.EngineVersion || storedWorker.BuildRevision != ownedWorker.BuildRevision {
		t.Fatalf("worker build identity = %#v, err=%v", storedWorker, err)
	}
	takeover := ownedWorker
	takeover.CredentialID = second.ID
	takeover.CredentialGeneration = second.Generation
	if err := store.RegisterWorker(ctx, takeover); !errors.Is(err, ErrForbidden) {
		t.Fatalf("worker ownership takeover err=%v, want ErrForbidden", err)
	}

	for _, workspace := range []string{"workspace-b", "workspace-a"} {
		deployment := contract.Deployment{
			Workspace: workspace, App: "echo", Commit: "commit-" + workspace,
			RequiredLabels: []string{"linux", "arm64"},
			Actions:        map[string]contract.Action{"run": {Action: "run", Command: []string{"helper"}}},
		}
		run := NewRun("windforce", "run-"+workspace, "echo", "run", deployment, []byte(`{}`))
		job := NewActionJob(run, nil)
		if err := store.CreateRunAndEnqueue(ctx, run, job); err != nil {
			t.Fatal(err)
		}
	}
	claimed, _, err := store.ClaimJobForWorkerScope(
		ctx, "managed-worker", nil, []string{"linux", "arm64"}, []string{"workspace-a"}, time.Minute,
	)
	if err != nil || contract.NormalizeWorkspace(claimed.Payload.Workspace) != "workspace-a" {
		t.Fatalf("scoped claim = %#v, err=%v", claimed, err)
	}

	initial, err := store.GetWorkerGroupRunState(ctx, "group-a")
	if err != nil || initial.State != WorkerGroupRunning || initial.Revision != 0 {
		t.Fatalf("initial run state = %#v, err=%v", initial, err)
	}
	if _, _, err := store.PutWorkerGroupRunState(ctx, PutWorkerGroupRunStateRequest{
		Group: "group-a", State: WorkerGroupDraining, OperationID: "op-missing-deadline", ExpectedRevision: 0,
		RequestFingerprint: "missing-deadline", Actor: "tester",
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("drain without deadline err=%v, want ErrInvalidState", err)
	}
	deadline := time.Now().UTC().Add(10 * time.Minute)
	draining, replayed, err := store.PutWorkerGroupRunState(ctx, PutWorkerGroupRunStateRequest{
		Group: "group-a", State: WorkerGroupDraining, OperationID: "op-drain", ExpectedRevision: 0,
		DeadlineAt: &deadline, RequestFingerprint: "drain-fingerprint", Actor: "tester",
	})
	if err != nil || replayed || draining.Revision != 1 || !draining.Draining() {
		t.Fatalf("draining = %#v, replayed=%v, err=%v", draining, replayed, err)
	}
	_, replayed, err = store.PutWorkerGroupRunState(ctx, PutWorkerGroupRunStateRequest{
		Group: "group-a", State: WorkerGroupDraining, OperationID: "op-drain", ExpectedRevision: 0,
		DeadlineAt: &deadline, RequestFingerprint: "drain-fingerprint", Actor: "tester",
	})
	if err != nil || !replayed {
		t.Fatalf("drain replayed=%v, err=%v", replayed, err)
	}
	if _, _, err := store.PutWorkerGroupRunState(ctx, PutWorkerGroupRunStateRequest{
		Group: "group-a", State: WorkerGroupRunning, OperationID: "op-stale-state", ExpectedRevision: 0,
		RequestFingerprint: "stale-state", Actor: "tester",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale run state err=%v, want ErrConflict", err)
	}

	drainDeadline := time.Now().UTC().Add(5 * time.Minute)
	revoked, replayed, err := store.RevokeWorkerCredential(ctx, RevokeWorkerCredentialRequest{
		Group: "group-a", CredentialID: created.ID, OperationID: "op-revoke-1",
		RequestFingerprint: "revoke-fingerprint", DrainDeadlineAt: drainDeadline, Actor: "tester",
	})
	if err != nil || replayed || revoked.Status != WorkerCredentialRevoked || revoked.AllowsNewWork(time.Now().UTC()) ||
		!revoked.AllowsLeaseContinuation(time.Now().UTC()) {
		t.Fatalf("revoked = %#v, replayed=%v, err=%v", revoked, replayed, err)
	}
	_, replayed, err = store.RevokeWorkerCredential(ctx, RevokeWorkerCredentialRequest{
		Group: "group-a", CredentialID: created.ID, OperationID: "op-revoke-1",
		RequestFingerprint: "revoke-fingerprint", DrainDeadlineAt: drainDeadline, Actor: "tester",
	})
	if err != nil || !replayed {
		t.Fatalf("revoke replayed=%v, err=%v", replayed, err)
	}
}
