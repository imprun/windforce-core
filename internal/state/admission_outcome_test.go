package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

func TestLocalAdmissionOutcomeFence(t *testing.T) {
	store := NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	exerciseAdmissionOutcomeFence(t, store, "admission-local")
	exerciseAdmissionOutcomeRace(t, store, "admission-local", 32)
}

func TestPostgresAdmissionOutcomeFence(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	store := openIsolatedPostgresCatalogStore(t, dsn)
	exerciseAdmissionOutcomeFence(t, store, "admission-postgres")
	exerciseAdmissionOutcomeRace(t, store, "admission-postgres", 16)
}

func TestLocalAdmissionOutcomeSurvivesLegacySnapshotMigrationAndRunPrune(t *testing.T) {
	store := NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	exerciseAdmissionOutcomeSurvivesRunPrune(t, store, "admission-local-prune", func(ctx context.Context, workspaceID string, admissionID string) {
		t.Helper()
		if err := store.update(ctx, func(snapshot *Snapshot, _ time.Time) error {
			delete(snapshot.AdmissionOutcomes, admissionOutcomeKey(workspaceID, admissionID))
			return nil
		}); err != nil {
			t.Fatalf("prepare legacy local snapshot: %v", err)
		}
	})
}

func TestPostgresAdmissionOutcomeSurvivesLegacyForeignKeyMigrationAndRunPrune(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	store := openIsolatedPostgresCatalogStore(t, dsn)
	exerciseAdmissionOutcomeSurvivesRunPrune(t, store, "admission-postgres-prune", func(ctx context.Context, workspaceID string, admissionID string) {
		t.Helper()
		if _, err := store.pool.Exec(ctx, `DELETE FROM admission_outcome WHERE workspace_id=$1 AND admission_id=$2`, workspaceID, admissionID); err != nil {
			t.Fatalf("remove outcome to prepare legacy schema: %v", err)
		}
		if _, err := store.pool.Exec(ctx, `
ALTER TABLE admission_outcome
ADD CONSTRAINT admission_outcome_run_id_fkey
FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
`); err != nil {
			t.Fatalf("prepare legacy admission foreign key: %v", err)
		}
		if err := store.Migrate(ctx); err != nil {
			t.Fatalf("Migrate legacy admission outcome schema: %v", err)
		}
		var foreignKeys int
		if err := store.pool.QueryRow(ctx, `
SELECT count(*)
FROM pg_constraint
WHERE conrelid = 'admission_outcome'::regclass
  AND conname = 'admission_outcome_run_id_fkey'
  AND contype = 'f'
`).Scan(&foreignKeys); err != nil {
			t.Fatalf("inspect migrated admission foreign key: %v", err)
		}
		if foreignKeys != 0 {
			t.Fatalf("legacy admission foreign keys after migration = %d, want 0", foreignKeys)
		}
	})
}

func exerciseAdmissionOutcomeSurvivesRunPrune(
	t *testing.T,
	store Store,
	workspaceID string,
	prepareLegacy func(context.Context, string, string),
) {
	t.Helper()
	ctx := context.Background()
	admissionID := "admission-prune-tombstone"
	fingerprint := "request-fingerprint-prune-tombstone"
	run, job := newAdmissionOutcomeTestRun(workspaceID, admissionID, fingerprint, true)
	if err := store.CreateRunAndEnqueue(ctx, run, job); err != nil {
		t.Fatalf("CreateRunAndEnqueue: %v", err)
	}
	if prepareLegacy != nil {
		prepareLegacy(ctx, workspaceID, admissionID)
	}

	reconciled, err := store.ResolveAdmissionOutcome(ctx, workspaceID, admissionID, fingerprint, "operator:before-prune")
	if err != nil || reconciled.State != AdmissionOutcomeAdmitted || reconciled.RunID != run.ID {
		t.Fatalf("reconciled outcome = %#v, err=%v", reconciled, err)
	}
	claimed, lease, err := store.ClaimJob(ctx, "worker-prune", time.Minute)
	if err != nil {
		t.Fatalf("ClaimJob: %v", err)
	}
	if claimed.ID != job.ID {
		t.Fatalf("claimed job = %s, want %s", claimed.ID, job.ID)
	}
	if err := store.CompleteJobSucceeded(ctx, lease, contract.JobResult{
		App: "echo", Action: "run", Output: json.RawMessage(`{"ok":true}`),
	}); err != nil {
		t.Fatalf("CompleteJobSucceeded: %v", err)
	}
	pruned, err := store.PruneSettledJobs(ctx, time.Now().UTC().Add(time.Hour), time.Time{})
	if err != nil {
		t.Fatalf("PruneSettledJobs: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("pruned jobs = %d, want 1", pruned)
	}
	if _, err := store.GetRun(ctx, run.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pruned Run lookup error = %v, want ErrNotFound", err)
	}

	retained, found, err := store.GetAdmissionOutcome(ctx, workspaceID, admissionID)
	if err != nil || !found || retained.State != AdmissionOutcomeAdmitted ||
		retained.RunID != run.ID || retained.RequestFingerprint != fingerprint {
		t.Fatalf("retained outcome = %#v, found=%t, err=%v", retained, found, err)
	}
	delayed, err := store.ResolveAdmissionOutcome(ctx, workspaceID, admissionID, fingerprint, "operator:after-prune")
	if err != nil || delayed.State != AdmissionOutcomeAdmitted || delayed.RunID != run.ID ||
		delayed.RequestFingerprint != fingerprint {
		t.Fatalf("delayed resolve = %#v, err=%v", delayed, err)
	}
	if _, err := store.ResolveAdmissionOutcome(ctx, workspaceID, admissionID, "different-fingerprint", "operator:after-prune"); !errors.Is(err, ErrConflict) {
		t.Fatalf("delayed fingerprint mismatch error = %v, want ErrConflict", err)
	}
	replayRun, replayJob := newAdmissionOutcomeTestRun(workspaceID, admissionID, fingerprint, true)
	if err := store.CreateRunAndEnqueue(ctx, replayRun, replayJob); !errors.Is(err, ErrConflict) {
		t.Fatalf("create after admitted tombstone error = %v, want ErrConflict", err)
	}
	if _, err := store.GetRun(ctx, run.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("admitted tombstone allowed Run recreation: %v", err)
	}
}

func exerciseAdmissionOutcomeFence(t *testing.T, store Store, workspaceID string) {
	t.Helper()
	ctx := context.Background()

	if outcome, found, err := store.GetAdmissionOutcome(ctx, workspaceID, "admission-unknown"); err != nil || found || outcome.AdmissionID != "" {
		t.Fatalf("unknown outcome = %#v, found=%t, err=%v", outcome, found, err)
	}
	if _, err := store.ResolveAdmissionOutcome(ctx, workspaceID, "admission-invalid", "", "operator:test"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("empty fingerprint error = %v, want ErrInvalidState", err)
	}

	createID := "admission-create-wins"
	createFingerprint := "request-fingerprint-create"
	createdRun, createdJob := newAdmissionOutcomeTestRun(workspaceID, createID, createFingerprint, true)
	if err := store.CreateRunAndEnqueue(ctx, createdRun, createdJob); err != nil {
		t.Fatalf("CreateRunAndEnqueue(create wins): %v", err)
	}
	createdOutcome, found, err := store.GetAdmissionOutcome(ctx, workspaceID, createID)
	if err != nil || !found || createdOutcome.State != AdmissionOutcomeAdmitted ||
		createdOutcome.AdmissionID != createID || createdOutcome.RunID != createdRun.ID ||
		createdOutcome.RequestFingerprint != createFingerprint {
		t.Fatalf("created outcome = %#v, found=%t, err=%v", createdOutcome, found, err)
	}
	resolvedCreated, err := store.ResolveAdmissionOutcome(ctx, workspaceID, createID, createFingerprint, "operator:reconcile")
	if err != nil || resolvedCreated.State != AdmissionOutcomeAdmitted || resolvedCreated.RunID != createdRun.ID {
		t.Fatalf("resolve created = %#v, err=%v", resolvedCreated, err)
	}
	if _, err := store.ResolveAdmissionOutcome(ctx, workspaceID, createID, "different-fingerprint", "operator:reconcile"); !errors.Is(err, ErrConflict) {
		t.Fatalf("created fingerprint mismatch error = %v, want ErrConflict", err)
	}

	abortID := "admission-resolve-wins"
	abortFingerprint := "request-fingerprint-abort"
	aborted, err := store.ResolveAdmissionOutcome(ctx, workspaceID, abortID, abortFingerprint, "operator:reconcile")
	if err != nil || aborted.State != AdmissionOutcomeAborted || aborted.AdmissionID != abortID || aborted.RunID != "" {
		t.Fatalf("resolve absent = %#v, err=%v", aborted, err)
	}
	replayedAbort, err := store.ResolveAdmissionOutcome(ctx, workspaceID, abortID, abortFingerprint, "operator:other")
	if err != nil || canonicalStoredAdmissionOutcome(replayedAbort) != canonicalStoredAdmissionOutcome(aborted) {
		t.Fatalf("replayed abort = %#v, err=%v, want %#v", replayedAbort, err, aborted)
	}
	abortedRun, abortedJob := newAdmissionOutcomeTestRun(workspaceID, abortID, abortFingerprint, true)
	if err := store.CreateRunAndEnqueue(ctx, abortedRun, abortedJob); !errors.Is(err, ErrAdmissionAborted) {
		t.Fatalf("create after abort error = %v, want ErrAdmissionAborted", err)
	}
	if _, err := store.GetRun(ctx, abortID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("aborted Run exists or lookup failed: %v", err)
	}
	conflictingRun, conflictingJob := newAdmissionOutcomeTestRun(workspaceID, abortID, "different-fingerprint", true)
	if err := store.CreateRunAndEnqueue(ctx, conflictingRun, conflictingJob); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting create after abort error = %v, want ErrConflict", err)
	}

	nonIdempotentID := "admission-untracked"
	nonIdempotentRun, nonIdempotentJob := newAdmissionOutcomeTestRun(workspaceID, nonIdempotentID, "request-fingerprint-untracked", false)
	if err := store.CreateRunAndEnqueue(ctx, nonIdempotentRun, nonIdempotentJob); err != nil {
		t.Fatalf("non-idempotent CreateRunAndEnqueue: %v", err)
	}
	if outcome, found, err := store.GetAdmissionOutcome(ctx, workspaceID, nonIdempotentID); err != nil || found || outcome.AdmissionID != "" {
		t.Fatalf("non-idempotent outcome = %#v, found=%t, err=%v", outcome, found, err)
	}
}

func canonicalStoredAdmissionOutcome(outcome AdmissionOutcome) AdmissionOutcome {
	outcome.CreatedAt = outcome.CreatedAt.UTC().Truncate(time.Microsecond)
	outcome.UpdatedAt = outcome.UpdatedAt.UTC().Truncate(time.Microsecond)
	return outcome
}

func exerciseAdmissionOutcomeRace(t *testing.T, store Store, workspaceID string, iterations int) {
	t.Helper()
	ctx := context.Background()
	for index := 0; index < iterations; index++ {
		admissionID := fmt.Sprintf("admission-race-%02d", index)
		fingerprint := fmt.Sprintf("request-fingerprint-race-%02d", index)
		run, job := newAdmissionOutcomeTestRun(workspaceID, admissionID, fingerprint, true)
		start := make(chan struct{})
		createResult := make(chan error, 1)
		resolveResult := make(chan error, 1)
		go func() {
			<-start
			createResult <- store.CreateRunAndEnqueue(ctx, run, job)
		}()
		go func() {
			<-start
			_, err := store.ResolveAdmissionOutcome(ctx, workspaceID, admissionID, fingerprint, "operator:race")
			resolveResult <- err
		}()
		close(start)
		createErr := <-createResult
		resolveErr := <-resolveResult
		if resolveErr != nil {
			t.Fatalf("race %d resolve error: %v", index, resolveErr)
		}
		outcome, found, err := store.GetAdmissionOutcome(ctx, workspaceID, admissionID)
		if err != nil || !found {
			t.Fatalf("race %d outcome = %#v, found=%t, err=%v", index, outcome, found, err)
		}
		_, runErr := store.GetRun(ctx, admissionID)
		switch outcome.State {
		case AdmissionOutcomeAdmitted:
			if createErr != nil || runErr != nil || outcome.RunID != admissionID {
				t.Fatalf("race %d admitted: createErr=%v runErr=%v outcome=%#v", index, createErr, runErr, outcome)
			}
		case AdmissionOutcomeAborted:
			if !errors.Is(createErr, ErrAdmissionAborted) || !errors.Is(runErr, ErrNotFound) || outcome.RunID != "" {
				t.Fatalf("race %d aborted: createErr=%v runErr=%v outcome=%#v", index, createErr, runErr, outcome)
			}
		default:
			t.Fatalf("race %d invalid terminal outcome: %#v", index, outcome)
		}
	}
}

func newAdmissionOutcomeTestRun(
	workspaceID string,
	admissionID string,
	fingerprint string,
	idempotent bool,
) (Run, Job) {
	deployment := contract.Deployment{
		Workspace: workspaceID,
		App:       "echo",
		Actions:   map[string]contract.Action{"run": {Action: "run"}},
	}
	run := NewRun("http", admissionID, "echo", "run", deployment, []byte(`{}`))
	if idempotent {
		run.IdempotencyHash = "idempotency-" + admissionID
		run.RequestFingerprint = fingerprint
	}
	return run, NewActionJob(run, run.Input)
}
