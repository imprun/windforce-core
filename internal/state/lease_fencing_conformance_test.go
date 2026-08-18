package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

func TestLocalStoreLeaseFencingConformance(t *testing.T) {
	store := NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	now := time.Now().UTC().Truncate(time.Millisecond)
	store.leaseNow = func() time.Time { return now }
	exerciseLeaseFencingStoreContract(t, store, func(next time.Time) {
		now = next
	})
}

func TestPostgresStoreLeaseFencingConformance(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	store := openIsolatedPostgresCatalogStore(t, dsn)
	now := time.Now().UTC().Truncate(time.Millisecond)
	store.leaseNow = func() time.Time { return now }
	exerciseLeaseFencingStoreContract(t, store, func(next time.Time) {
		now = next
	})
}

func exerciseLeaseFencingStoreContract(t *testing.T, store Store, setNow func(time.Time)) {
	t.Helper()
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Millisecond)
	setNow(base)

	run, original := enqueueLeaseFencingJob(t, store, "reclaim")
	firstClaim, firstLease, err := store.ClaimJob(ctx, "worker-a", 10*time.Second)
	if err != nil {
		t.Fatalf("first ClaimJob: %v", err)
	}
	assertLeaseFencingPins(t, run, original, firstClaim)

	setNow(base.Add(9 * time.Second))
	if _, _, err := store.ClaimJob(ctx, "worker-before-expiry", time.Hour); !errors.Is(err, ErrNoQueuedJob) {
		t.Fatalf("claim immediately before expiry = %v, want ErrNoQueuedJob", err)
	}

	setNow(base.Add(11 * time.Second))
	secondClaim, secondLease, err := store.ClaimJob(ctx, "worker-b", time.Hour)
	if err != nil {
		t.Fatalf("reclaim after expiry: %v", err)
	}
	if secondClaim.ID != original.ID || secondLease.Attempt != firstLease.Attempt+1 {
		t.Fatalf("reclaimed job = %#v, lease = %#v", secondClaim, secondLease)
	}
	assertLeaseFencingPins(t, run, original, secondClaim)

	staleHeartbeat, err := store.HeartbeatJob(ctx, firstLease, time.Hour)
	if err != nil {
		t.Fatalf("stale HeartbeatJob: %v", err)
	}
	if staleHeartbeat.StillOwned {
		t.Fatal("stale heartbeat retained ownership after reclaim")
	}
	if err := store.CompleteJobSucceeded(ctx, firstLease, leaseFencingResult(original, "stale")); !errors.Is(err, ErrInvalidLease) {
		t.Fatalf("stale CompleteJobSucceeded error = %v, want ErrInvalidLease", err)
	}
	ownedHeartbeat, err := store.HeartbeatJob(ctx, secondLease, time.Hour)
	if err != nil || !ownedHeartbeat.StillOwned {
		t.Fatalf("current HeartbeatJob = %#v, %v", ownedHeartbeat, err)
	}
	if err := store.CompleteJobSucceeded(ctx, secondLease, leaseFencingResult(original, "current")); err != nil {
		t.Fatalf("current CompleteJobSucceeded: %v", err)
	}
	terminalJob, terminalRun, found, err := store.GetJob(ctx, "default", original.ID)
	if err != nil || !found {
		t.Fatalf("GetJob terminal = found %v, error %v", found, err)
	}
	if terminalJob.State != JobSucceeded || terminalRun.State != RunSucceeded {
		t.Fatalf("terminal state = job %s, run %s", terminalJob.State, terminalRun.State)
	}
	assertLeaseFencingPins(t, run, original, terminalJob)
	if _, _, err := store.ClaimJob(ctx, "worker-c", time.Hour); !errors.Is(err, ErrNoQueuedJob) {
		t.Fatalf("terminal Job was reclaimable: %v", err)
	}

	softRun, softJob := enqueueLeaseFencingJob(t, store, "cancel-first")
	_, softLease, err := store.ClaimJob(ctx, "worker-cancel", time.Hour)
	if err != nil {
		t.Fatalf("claim cancel-first Job: %v", err)
	}
	cancelFirst, err := store.CancelJob(ctx, "default", softJob.ID, "operator:test", "stop")
	if err != nil || !cancelFirst.SoftCanceled {
		t.Fatalf("cancel-first result = %#v, error %v", cancelFirst, err)
	}
	if err := store.CompleteJobSucceeded(ctx, softLease, leaseFencingResult(softJob, "ignored")); err != nil {
		t.Fatalf("complete soft-canceled Job: %v", err)
	}
	softTerminalJob, softTerminalRun, found, err := store.GetJob(ctx, "default", softJob.ID)
	if err != nil || !found {
		t.Fatalf("GetJob cancel-first = found %v, error %v", found, err)
	}
	if softTerminalJob.State != JobFailed || softTerminalRun.State != RunCanceled {
		t.Fatalf("cancel-first state = job %s, run %s", softTerminalJob.State, softTerminalRun.State)
	}
	assertLeaseFencingPins(t, softRun, softJob, softTerminalJob)

	completeRun, completeJob := enqueueLeaseFencingJob(t, store, "complete-first")
	_, completeLease, err := store.ClaimJob(ctx, "worker-complete", time.Hour)
	if err != nil {
		t.Fatalf("claim complete-first Job: %v", err)
	}
	if err := store.CompleteJobSucceeded(ctx, completeLease, leaseFencingResult(completeJob, "winner")); err != nil {
		t.Fatalf("complete complete-first Job: %v", err)
	}
	completeFirst, err := store.CancelJob(ctx, "default", completeJob.ID, "operator:test", "too late")
	if err != nil || !completeFirst.AlreadyCompleted {
		t.Fatalf("complete-first cancellation = %#v, error %v", completeFirst, err)
	}
	completedJob, completedRun, found, err := store.GetJob(ctx, "default", completeJob.ID)
	if err != nil || !found {
		t.Fatalf("GetJob complete-first = found %v, error %v", found, err)
	}
	if completedJob.State != JobSucceeded || completedRun.State != RunSucceeded {
		t.Fatalf("complete-first state = job %s, run %s", completedJob.State, completedRun.State)
	}
	assertLeaseFencingPins(t, completeRun, completeJob, completedJob)
}

func TestPostgresLeaseFencingUnderClaimAndCancelContention(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	ctx := context.Background()
	store := openIsolatedPostgresCatalogStore(t, dsn)
	peer, err := OpenPostgresStore(ctx, store.pool.Config().ConnString())
	if err != nil {
		t.Fatalf("open peer Postgres store: %v", err)
	}
	t.Cleanup(peer.Close)

	run, original := enqueueLeaseFencingJob(t, store, "contention")
	first := claimPostgresConcurrently(t, store, peer, "worker-a", "worker-b")
	firstWinner := assertSingleClaimWinner(t, first, 1)
	assertLeaseFencingPins(t, run, original, firstWinner.job)

	if _, err := store.pool.Exec(ctx, `UPDATE jobs SET lease_expires_at=$1 WHERE id=$2`, time.Now().UTC().Add(-time.Minute), original.ID); err != nil {
		t.Fatalf("expire first lease: %v", err)
	}
	second := claimPostgresConcurrently(t, store, peer, "worker-c", "worker-d")
	secondWinner := assertSingleClaimWinner(t, second, 2)
	assertLeaseFencingPins(t, run, original, secondWinner.job)
	if err := store.CompleteJobSucceeded(ctx, firstWinner.lease, leaseFencingResult(original, "stale")); !errors.Is(err, ErrInvalidLease) {
		t.Fatalf("first contention winner retained completion authority: %v", err)
	}
	if err := peer.CompleteJobSucceeded(ctx, secondWinner.lease, leaseFencingResult(original, "current")); err != nil {
		t.Fatalf("second contention winner completion: %v", err)
	}

	_, raceJob := enqueueLeaseFencingJob(t, store, "cancel-complete-race")
	_, raceLease, err := store.ClaimJob(ctx, "worker-race", time.Hour)
	if err != nil {
		t.Fatalf("claim race Job: %v", err)
	}
	start := make(chan struct{})
	completeResult := make(chan error, 1)
	cancelResult := make(chan struct {
		result CancelResult
		err    error
	}, 1)
	go func() {
		<-start
		completeResult <- store.CompleteJobSucceeded(ctx, raceLease, leaseFencingResult(raceJob, "race"))
	}()
	go func() {
		<-start
		result, err := peer.CancelJob(ctx, "default", raceJob.ID, "operator:test", "race")
		cancelResult <- struct {
			result CancelResult
			err    error
		}{result: result, err: err}
	}()
	close(start)
	if err := <-completeResult; err != nil {
		t.Fatalf("contended completion: %v", err)
	}
	canceled := <-cancelResult
	if canceled.err != nil || !canceled.result.Found || (!canceled.result.SoftCanceled && !canceled.result.AlreadyCompleted) {
		t.Fatalf("contended cancellation = %#v, error %v", canceled.result, canceled.err)
	}
	finalJob, finalRun, found, err := store.GetJob(ctx, "default", raceJob.ID)
	if err != nil || !found {
		t.Fatalf("GetJob race = found %v, error %v", found, err)
	}
	if canceled.result.SoftCanceled {
		if finalJob.State != JobFailed || finalRun.State != RunCanceled {
			t.Fatalf("cancel-won race state = job %s, run %s", finalJob.State, finalRun.State)
		}
	} else if finalJob.State != JobSucceeded || finalRun.State != RunSucceeded {
		t.Fatalf("complete-won race state = job %s, run %s", finalJob.State, finalRun.State)
	}
}

type postgresClaimOutcome struct {
	job    Job
	lease  Lease
	worker string
	err    error
}

func claimPostgresConcurrently(t *testing.T, first *PostgresStore, second *PostgresStore, firstWorker string, secondWorker string) []postgresClaimOutcome {
	t.Helper()
	ctx := context.Background()
	start := make(chan struct{})
	results := make(chan postgresClaimOutcome, 2)
	claim := func(store *PostgresStore, worker string) {
		<-start
		job, lease, err := store.ClaimJob(ctx, worker, time.Hour)
		results <- postgresClaimOutcome{job: job, lease: lease, worker: worker, err: err}
	}
	go claim(first, firstWorker)
	go claim(second, secondWorker)
	close(start)
	return []postgresClaimOutcome{<-results, <-results}
}

func assertSingleClaimWinner(t *testing.T, outcomes []postgresClaimOutcome, attempt int) postgresClaimOutcome {
	t.Helper()
	winners := make([]postgresClaimOutcome, 0, 1)
	losers := 0
	for _, outcome := range outcomes {
		switch {
		case outcome.err == nil:
			winners = append(winners, outcome)
		case errors.Is(outcome.err, ErrNoQueuedJob):
			losers++
		default:
			t.Fatalf("claim by %s: %v", outcome.worker, outcome.err)
		}
	}
	if len(winners) != 1 || losers != 1 {
		t.Fatalf("claim outcomes = %#v, want one winner and one ErrNoQueuedJob", outcomes)
	}
	if winners[0].lease.Attempt != attempt || winners[0].job.Attempt != attempt {
		t.Fatalf("winner attempt = job %d, lease %d; want %d", winners[0].job.Attempt, winners[0].lease.Attempt, attempt)
	}
	return winners[0]
}

func enqueueLeaseFencingJob(t *testing.T, store Store, suffix string) (Run, Job) {
	t.Helper()
	runID := "run-lease-fencing-" + suffix
	deploymentID := "deployment-lease-fencing-" + suffix
	deployment := contract.Deployment{
		Workspace:    "default",
		GitSourceID:  "source-lease-fencing",
		App:          "lease-fencing",
		Commit:       "commit-pinned-" + suffix,
		DeploymentID: &deploymentID,
		BundleDigest: "sha256:bundle-pinned-" + suffix,
		Actions: map[string]contract.Action{
			"run": {Action: "run", Command: []string{"bun", "run.ts"}},
		},
	}
	input := json.RawMessage(fmt.Sprintf(`{"fixture":%q}`, suffix))
	run := NewRun("windforce", runID, "lease-fencing", "run", deployment, input)
	job := NewActionJob(run, nil)
	if err := store.CreateRunAndEnqueue(context.Background(), run, job); err != nil {
		t.Fatalf("CreateRunAndEnqueue %s: %v", suffix, err)
	}
	return run, job
}

func assertLeaseFencingPins(t *testing.T, originalRun Run, originalJob Job, actual Job) {
	t.Helper()
	if actual.ID != originalJob.ID || actual.RunID != originalJob.RunID {
		t.Fatalf("Job identity changed: got %s/%s, want %s/%s", actual.ID, actual.RunID, originalJob.ID, originalJob.RunID)
	}
	if actual.Payload.Commit != originalJob.Payload.Commit || actual.Payload.BundleDigest != originalJob.Payload.BundleDigest {
		t.Fatalf("pinned release changed: got commit %q bundle %q, want commit %q bundle %q", actual.Payload.Commit, actual.Payload.BundleDigest, originalJob.Payload.Commit, originalJob.Payload.BundleDigest)
	}
	if !equalLeaseFencingJSON(actual.Payload.Input, originalJob.Payload.Input) {
		t.Fatalf("pinned input changed: got %s, want %s", actual.Payload.Input, originalJob.Payload.Input)
	}
	if actual.Payload.DeploymentID == nil || originalRun.Deployment.DeploymentID == nil || *actual.Payload.DeploymentID != *originalRun.Deployment.DeploymentID {
		t.Fatalf("pinned Deployment ID changed: got %v, want %v", actual.Payload.DeploymentID, originalRun.Deployment.DeploymentID)
	}
	if len(actual.Payload.ActionSpec.Command) != 2 || actual.Payload.ActionSpec.Command[0] != "bun" || actual.Payload.ActionSpec.Command[1] != "run.ts" {
		t.Fatalf("pinned Action command changed: %#v", actual.Payload.ActionSpec.Command)
	}
}

func equalLeaseFencingJSON(left json.RawMessage, right json.RawMessage) bool {
	if bytes.Equal(left, right) {
		return true
	}
	var leftValue any
	var rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func leaseFencingResult(job Job, winner string) contract.JobResult {
	return contract.JobResult{
		JobID:  job.ID,
		App:    job.Payload.App,
		Action: job.Payload.Action,
		Output: json.RawMessage(fmt.Sprintf(`{"winner":%q}`, winner)),
	}
}
