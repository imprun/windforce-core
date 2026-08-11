package state

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLocalStoreQueueDemandSnapshotContract(t *testing.T) {
	path := t.TempDir() + "/state.json"
	store := NewLocalStore(path)
	store.leaseNow = func() time.Time { return time.Date(2026, 8, 1, 1, 2, 3, 0, time.UTC) }

	first := exerciseQueueDemandSnapshotContract(t, store, func(jobID string) {
		t.Helper()
		if err := store.updateLease(context.Background(), func(snapshot *Snapshot, now time.Time) error {
			job := snapshot.Jobs[jobID]
			expiresAt := now.Add(-time.Second)
			job.LeaseExpiresAt = &expiresAt
			snapshot.Jobs[jobID] = job
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})

	reopened := NewLocalStore(path)
	reopened.leaseNow = store.leaseNow
	second, err := reopened.QueueDemandSnapshot(context.Background(), queueDemandContractSelectors())
	if err != nil {
		t.Fatal(err)
	}
	if second.StoreEpoch != first.StoreEpoch {
		t.Fatalf("store epoch after reopen = %q, want %q", second.StoreEpoch, first.StoreEpoch)
	}
	if second.SnapshotRevision != first.SnapshotRevision {
		t.Fatalf("revision after read-only reopen = %d, want %d", second.SnapshotRevision, first.SnapshotRevision)
	}
}

func TestPostgresStoreQueueDemandSnapshotContract(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	store := openIsolatedPostgresCatalogStore(t, dsn)
	exerciseQueueDemandSnapshotContract(t, store, func(jobID string) {
		t.Helper()
		if _, err := store.pool.Exec(context.Background(), `UPDATE jobs SET lease_expires_at=now() - interval '1 second' WHERE id=$1`, jobID); err != nil {
			t.Fatal(err)
		}
	})
}

type queueDemandContractStore interface {
	CreateRunAndEnqueue(context.Context, Run, Job) error
	ClaimJobForWorker(context.Context, string, []string, []string, time.Duration) (Job, Lease, error)
	CancelJob(context.Context, string, string, string, string) (CancelResult, error)
	QueueDemandSnapshot(context.Context, []QueueDemandSelector) (QueueDemandSnapshot, error)
}

func exerciseQueueDemandSnapshotContract(t *testing.T, store queueDemandContractStore, expire func(string)) QueueDemandSnapshot {
	t.Helper()
	ctx := context.Background()
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	createQueueDemandJob(t, store, "normal", "managed", []string{"arm64"}, nil, base)
	createQueueDemandJob(t, store, "other", "other", []string{"arm64"}, nil, base.Add(time.Second))
	createQueueDemandJob(t, store, "gpu", "managed", []string{"gpu"}, nil, base.Add(2*time.Second))

	activeID := createQueueDemandJob(t, store, "active", "active", []string{"arm64"}, nil, base.Add(3*time.Second))
	claimQueueDemandJob(t, store, "worker-active", "active", activeID)

	expiredID := createQueueDemandJob(t, store, "expired", "expired", []string{"arm64"}, nil, base.Add(4*time.Second))
	claimQueueDemandJob(t, store, "worker-expired", "expired", expiredID)

	limit := int32(1)
	limitedRunningID := createQueueDemandJob(t, store, "limited", "limited-running", []string{"arm64"}, &limit, base.Add(5*time.Second))
	claimQueueDemandJob(t, store, "worker-limited", "limited-running", limitedRunningID)
	createQueueDemandJob(t, store, "limited", "limited", []string{"arm64"}, &limit, base.Add(6*time.Second))

	canceledID := createQueueDemandJob(t, store, "canceled", "managed", []string{"arm64"}, nil, base.Add(7*time.Second))
	if _, err := store.CancelJob(ctx, "ws-a", canceledID, "operator:test", "fixture cancel"); err != nil {
		t.Fatal(err)
	}
	expire(expiredID)

	snapshot, err := store.QueueDemandSnapshot(ctx, queueDemandContractSelectors())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.StoreEpoch == "" || snapshot.SnapshotRevision <= 0 || snapshot.ObservedAt.IsZero() {
		t.Fatalf("invalid snapshot fence: %#v", snapshot)
	}
	if len(snapshot.Items) != 3 {
		t.Fatalf("snapshot items = %d, want 3", len(snapshot.Items))
	}
	managed := snapshot.Items[0]
	if managed.Selector.Key != "managed-arm64" || managed.Eligible != 2 || managed.Queued != 1 || managed.ExpiredReacquirable != 1 || managed.Claimed != 2 || managed.BusyWorkers != 2 {
		t.Fatalf("managed demand = %#v", managed)
	}
	if managed.OldestEligibleAt == nil || !managed.OldestEligibleAt.Equal(base) {
		t.Fatalf("managed oldest eligible = %v, want %v", managed.OldestEligibleAt, base)
	}
	other := snapshot.Items[1]
	if other.Eligible != 1 || other.Queued != 1 || other.Claimed != 0 {
		t.Fatalf("other demand = %#v", other)
	}
	gpu := snapshot.Items[2]
	if gpu.Eligible != 1 || gpu.Queued != 1 || gpu.Claimed != 0 {
		t.Fatalf("gpu demand = %#v", gpu)
	}

	createQueueDemandJob(t, store, "revision", "unobserved", nil, nil, base.Add(8*time.Second))
	next, err := store.QueueDemandSnapshot(ctx, queueDemandContractSelectors())
	if err != nil {
		t.Fatal(err)
	}
	if next.StoreEpoch != snapshot.StoreEpoch || next.SnapshotRevision <= snapshot.SnapshotRevision {
		t.Fatalf("next fence = %s/%d, previous = %s/%d", next.StoreEpoch, next.SnapshotRevision, snapshot.StoreEpoch, snapshot.SnapshotRevision)
	}
	return next
}

func queueDemandContractSelectors() []QueueDemandSelector {
	return []QueueDemandSelector{
		{Key: "managed-arm64", WorkspaceID: "ws-a", Tags: []string{"managed", "active", "expired", "limited", "limited-running"}, Labels: []string{"arm64"}},
		{Key: "other-arm64", WorkspaceID: "ws-a", Tags: []string{"other"}, Labels: []string{"arm64"}},
		{Key: "managed-gpu", WorkspaceID: "ws-a", Tags: []string{"managed"}, Labels: []string{"gpu"}},
	}
}

func createQueueDemandJob(t *testing.T, store queueDemandContractStore, app string, tag string, labels []string, maxConcurrent *int32, createdAt time.Time) string {
	t.Helper()
	runID := NewID("run")
	jobID := NewID("job")
	run := Run{ID: runID, App: app, Action: "run", State: RunQueued, CreatedAt: createdAt, UpdatedAt: createdAt}
	job := Job{
		ID: jobID, RunID: runID, State: JobQueued, Kind: "action", Priority: 100,
		Payload:   JobPayload{Workspace: "ws-a", App: app, Action: "run", Tag: tag, RequiredLabels: labels, MaxConcurrent: maxConcurrent},
		CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	if err := store.CreateRunAndEnqueue(context.Background(), run, job); err != nil {
		t.Fatal(err)
	}
	return jobID
}

func claimQueueDemandJob(t *testing.T, store queueDemandContractStore, workerID string, tag string, expectedJobID string) {
	t.Helper()
	job, _, err := store.ClaimJobForWorker(context.Background(), workerID, []string{tag}, []string{"arm64"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != expectedJobID {
		t.Fatalf("claimed job = %q, want %q", job.ID, expectedJobID)
	}
}

func TestQueueDemandExcludesJobsBlockedByKeyedConcurrency(t *testing.T) {
	observedAt := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	shared := keyedConcurrencyPin("account", strings.Repeat("d", 64), 1)
	other := keyedConcurrencyPin("account", strings.Repeat("e", 64), 1)
	job := func(id string, state JobState, pin KeyedConcurrencyLimitPin, createdAt time.Time) Job {
		var leaseExpiresAt *time.Time
		if state == JobRunning {
			value := observedAt.Add(time.Minute)
			leaseExpiresAt = &value
		}
		return Job{
			ID: id, State: state, CreatedAt: createdAt, LeaseExpiresAt: leaseExpiresAt,
			Payload: JobPayload{
				Workspace: "ws-a", App: "echo", Action: "run", Tag: "default",
				ExecutionLimits: ExecutionLimitPins{Concurrency: []KeyedConcurrencyLimitPin{pin}},
			},
		}
	}
	snapshot := buildQueueDemandSnapshot("epoch", 1, observedAt, []Job{
		job("running-shared", JobRunning, shared, observedAt.Add(-time.Minute)),
		job("queued-shared", JobQueued, shared, observedAt),
		job("queued-other", JobQueued, other, observedAt.Add(time.Second)),
	}, []QueueDemandSelector{{Key: "default", WorkspaceID: "ws-a", Tags: []string{"default"}}})
	if len(snapshot.Items) != 1 || snapshot.Items[0].Eligible != 1 || snapshot.Items[0].Queued != 1 {
		t.Fatalf("keyed queue demand = %#v, want only the different key eligible", snapshot.Items)
	}
}
