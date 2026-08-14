package state

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

func TestLocalRegisteredWorkerClaimIdentityContract(t *testing.T) {
	store := NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	exerciseRegisteredWorkerClaimIdentity(t, store, func(now time.Time) {
		store.leaseNow = func() time.Time { return now }
	})

	reloaded := NewLocalStore(store.Path)
	job, _, ok, err := reloaded.GetJob(context.Background(), "workspace-a", "job-registered")
	if err != nil || !ok || job.LeaseIdentity == nil || job.LeaseIdentity.Group != "group-b" {
		t.Fatalf("reloaded job identity = %#v, ok=%v, err=%v", job.LeaseIdentity, ok, err)
	}
}

func TestPostgresRegisteredWorkerClaimIdentityContract(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	store := openIsolatedPostgresCatalogStore(t, dsn)
	exerciseRegisteredWorkerClaimIdentity(t, store, func(now time.Time) {
		store.leaseNow = func() time.Time { return now }
	})

	var dataType string
	if err := store.pool.QueryRow(context.Background(), `
SELECT data_type
FROM information_schema.columns
WHERE table_schema=current_schema() AND table_name='jobs' AND column_name='lease_identity'`).Scan(&dataType); err != nil {
		t.Fatalf("inspect lease_identity migration: %v", err)
	}
	if dataType != "jsonb" {
		t.Fatalf("jobs.lease_identity data type = %q, want jsonb", dataType)
	}
}

func exerciseRegisteredWorkerClaimIdentity(t *testing.T, store Store, setNow func(time.Time)) {
	t.Helper()
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)
	setNow(base)

	profile, err := contract.NewExecutionProfile("image-a", "linux", "amd64", "bun", "1.2.3", "glibc-2.39")
	if err != nil {
		t.Fatal(err)
	}
	workerA := WorkerRecord{
		ID: "worker-reused", Group: "group-a", Tags: []string{"blue"}, Labels: []string{"linux"},
		ExecutionProfiles: []contract.ExecutionProfile{profile}, Status: WorkerStatusActive,
		CredentialID: "credential-a", CredentialGeneration: 3,
	}
	if err := store.RegisterWorker(ctx, workerA); err != nil {
		t.Fatalf("register worker A: %v", err)
	}
	tags, labels, err := WorkerClaimSelector(workerA)
	if err != nil {
		t.Fatal(err)
	}
	enqueueClaimIdentityJob(t, store, "run-registered", "job-registered", "registered", "blue", []string{"linux"}, profile)

	if _, _, err := store.ClaimJobForWorker(ctx, workerA.ID, []string{"forged"}, labels, time.Minute); !errors.Is(err, ErrForbidden) {
		t.Fatalf("selector mismatch err=%v, want ErrForbidden", err)
	}
	queued, _, ok, err := store.GetJob(ctx, "workspace-a", "job-registered")
	if err != nil || !ok || queued.State != JobQueued || queued.LeaseIdentity != nil {
		t.Fatalf("job after rejected claim = %#v, ok=%v, err=%v", queued, ok, err)
	}

	claimed, lease, err := store.ClaimJobForWorker(ctx, workerA.ID, tags, labels, time.Minute)
	if err != nil {
		t.Fatalf("registered claim: %v", err)
	}
	assertWorkerLeaseIdentity(t, claimed.LeaseIdentity, "group-a", 3)
	assertWorkerLeaseIdentity(t, lease.Identity, "group-a", 3)
	encoded, err := json.Marshal(claimed)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "LeaseIdentity") || strings.Contains(string(encoded), "leaseIdentity") {
		t.Fatalf("internal lease identity leaked into Job JSON: %s", encoded)
	}
	encoded, err = json.Marshal(lease)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "Identity") || strings.Contains(string(encoded), "identity") {
		t.Fatalf("internal lease identity leaked into Lease JSON: %s", encoded)
	}

	if err := store.DeregisterWorker(ctx, workerA.ID); err != nil {
		t.Fatalf("deregister worker A: %v", err)
	}
	setNow(base.Add(30 * time.Second))
	workerB := workerA
	workerB.Group = "group-b"
	workerB.CredentialID = "credential-b"
	workerB.CredentialGeneration = 4
	if err := store.RegisterWorker(ctx, workerB); err != nil {
		t.Fatalf("register worker B with reused ID: %v", err)
	}
	persisted, _, ok, err := store.GetJob(ctx, "workspace-a", claimed.ID)
	if err != nil || !ok {
		t.Fatalf("get claimed job: ok=%v, err=%v", ok, err)
	}
	assertWorkerLeaseIdentity(t, persisted.LeaseIdentity, "group-a", 3)

	setNow(base.Add(70 * time.Second))
	reclaimed, secondLease, err := store.ClaimJobForWorker(ctx, workerB.ID, tags, labels, time.Minute)
	if err != nil {
		t.Fatalf("claim after lease expiry: %v", err)
	}
	if reclaimed.ID != claimed.ID || reclaimed.Attempt != 2 {
		t.Fatalf("reclaimed job = %#v, want same job attempt 2", reclaimed)
	}
	assertWorkerLeaseIdentity(t, reclaimed.LeaseIdentity, "group-b", 4)
	assertWorkerLeaseIdentity(t, secondLease.Identity, "group-b", 4)
	if err := store.CompleteJobSucceeded(ctx, secondLease, contract.JobResult{
		JobID: reclaimed.ID, App: "registered", Action: "run", Output: json.RawMessage(`{"ok":true}`),
	}); err != nil {
		t.Fatalf("complete reattributed job: %v", err)
	}
	terminal, _, ok, err := store.GetJob(ctx, "workspace-a", reclaimed.ID)
	if err != nil || !ok || terminal.State != JobSucceeded {
		t.Fatalf("terminal job = %#v, ok=%v, err=%v", terminal, ok, err)
	}
	assertWorkerLeaseIdentity(t, terminal.LeaseIdentity, "group-b", 4)

	enqueueClaimIdentityJob(t, store, "run-legacy", "job-legacy", "legacy", "legacy", nil, contract.ExecutionProfile{})
	legacy, legacyLease, err := store.ClaimJobForWorker(ctx, "unregistered-legacy-worker", []string{"legacy"}, nil, time.Minute)
	if err != nil {
		t.Fatalf("legacy unregistered claim: %v", err)
	}
	if legacy.LeaseIdentity != nil || legacyLease.Identity != nil {
		t.Fatalf("legacy claim unexpectedly attributed: job=%#v lease=%#v", legacy.LeaseIdentity, legacyLease.Identity)
	}
}

func enqueueClaimIdentityJob(t *testing.T, store Store, runID string, jobID string, app string, tag string, labels []string, profile contract.ExecutionProfile) {
	t.Helper()
	deployment := contract.Deployment{
		Workspace: "workspace-a", App: app, Commit: "commit-" + app, Tag: tag,
		RequiredLabels: labels, ExecutionProfile: profile,
		Actions: map[string]contract.Action{"run": {Action: "run", Command: []string{"helper"}}},
	}
	run := NewRun("windforce", runID, app, "run", deployment, json.RawMessage(`{}`))
	job := NewActionJob(run, nil)
	job.ID = jobID
	if err := store.CreateRunAndEnqueue(context.Background(), run, job); err != nil {
		t.Fatalf("enqueue %s: %v", jobID, err)
	}
}

func assertWorkerLeaseIdentity(t *testing.T, identity *WorkerLeaseIdentity, group string, generation int64) {
	t.Helper()
	if identity == nil || identity.Group != group || identity.CredentialGeneration != generation {
		t.Fatalf("lease identity = %#v, want group=%q generation=%d", identity, group, generation)
	}
}
