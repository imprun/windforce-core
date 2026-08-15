package state

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

type placementProjectionStore interface {
	Store
	routingPolicyCatalog
	WorkerControlStore
	PlacementObservationStore
}

func TestLocalPlacementProjectionContract(t *testing.T) {
	exercisePlacementProjectionContract(t, NewLocalStore(t.TempDir()+"/state.json"))
}

func TestPostgresPlacementProjectionContract(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	exercisePlacementProjectionContract(t, openIsolatedPostgresCatalogStore(t, dsn))
}

func TestPlacementProjectionExplainsSelectorMismatches(t *testing.T) {
	now := time.Now().UTC()
	profile, err := contract.NewExecutionProfile("image-a", "linux", "amd64", "bun", "1.2.3", "glibc-2.39")
	if err != nil {
		t.Fatal(err)
	}
	wrongProfile, err := contract.NewExecutionProfile("image-b", "linux", "arm64", "bun", "1.2.3", "glibc-2.39")
	if err != nil {
		t.Fatal(err)
	}
	deployment := routingPolicyDeployment("commit-reasons", "ready", "ready", []string{"gpu"}, nil)
	deployment.ExecutionProfile = profile
	workers := []WorkerRecord{
		{ID: "wrong-tag", Group: "missing-tag", Tags: []string{"other"}, Labels: []string{"gpu"}, ExecutionProfiles: []contract.ExecutionProfile{profile}, Status: WorkerStatusActive, LastHeartbeatAt: now},
		{ID: "wrong-label", Group: "missing-label", Tags: []string{"ready"}, ExecutionProfiles: []contract.ExecutionProfile{profile}, Status: WorkerStatusActive, LastHeartbeatAt: now},
		{ID: "wrong-profile", Group: "missing-profile", Tags: []string{"ready"}, Labels: []string{"gpu"}, ExecutionProfiles: []contract.ExecutionProfile{wrongProfile}, Status: WorkerStatusActive, LastHeartbeatAt: now},
	}
	projection, err := buildPlacementCandidates("workspace-a", "run", false, now, deployment, workers, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	target := projection.Targets[0]
	for group, reason := range map[string]string{
		"missing-tag": PlacementReasonMissingTag, "missing-label": PlacementReasonMissingLabel,
		"missing-profile": PlacementReasonExecutionProfileMismatch,
	} {
		candidate := requireCandidateGroup(t, target, group)
		if candidate.Eligible || !slices.Contains(candidate.ReasonCodes, reason) {
			t.Fatalf("candidate %s = %#v, want reason %s", group, candidate, reason)
		}
	}
}

func TestExecutionDemandUsesQueuedStateAgeAndExactActiveLeases(t *testing.T) {
	observedAt := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	oldCreatedAt := observedAt.Add(-30 * time.Minute)
	firstQueuedAt := observedAt.Add(-8 * time.Minute)
	secondQueuedAt := observedAt.Add(-3 * time.Minute)
	activeLeaseExpiry := observedAt.Add(time.Minute)
	expiredLeaseExpiry := observedAt.Add(-time.Minute)
	jobs := []Job{
		{
			ID: "queued-first", State: JobQueued, CreatedAt: oldCreatedAt, UpdatedAt: firstQueuedAt,
			Payload: JobPayload{Workspace: "workspace-a", App: "echo", Action: "run", Tag: "ready"},
		},
		{
			ID: "queued-second", State: JobQueued, CreatedAt: oldCreatedAt.Add(-time.Hour), UpdatedAt: secondQueuedAt,
			Payload: JobPayload{Workspace: "workspace-a", App: "echo", Action: "run", Tag: "ready"},
		},
		{
			ID: "running-active", State: JobRunning, LeaseOwner: "worker-a", LeaseExpiresAt: &activeLeaseExpiry,
			Payload: JobPayload{Workspace: "workspace-b", App: "other", Action: "run", Tag: "ready"},
		},
		{
			ID: "running-expired", State: JobRunning, LeaseOwner: "worker-a", LeaseExpiresAt: &expiredLeaseExpiry,
			Payload: JobPayload{Workspace: "workspace-a", App: "echo", Action: "run", Tag: "ready"},
		},
	}
	workers := []WorkerRecord{{
		ID: "worker-a", Tags: []string{"ready"}, Slots: 1,
		Status: WorkerStatusActive, LastHeartbeatAt: observedAt,
	}}
	demand, err := buildExecutionDemand("workspace-a", "echo", "run", false, observedAt, workers, nil, nil, jobs)
	if err != nil {
		t.Fatal(err)
	}
	if demand.QueuedJobs != 2 || demand.OldestQueuedAt == nil || !demand.OldestQueuedAt.Equal(firstQueuedAt) || len(demand.Targets) != 1 {
		t.Fatalf("execution demand = %#v", demand)
	}
	target := demand.Targets[0]
	if target.TotalSlots != 1 || target.OccupiedSlots != 1 || target.AvailableSlots != 0 || !target.Saturated {
		t.Fatalf("active lease capacity = %#v", target)
	}

	jobs[2].LeaseExpiresAt = &expiredLeaseExpiry
	demand, err = buildExecutionDemand("workspace-a", "echo", "run", false, observedAt, workers, nil, nil, jobs)
	if err != nil {
		t.Fatal(err)
	}
	target = demand.Targets[0]
	if target.OccupiedSlots != 0 || target.AvailableSlots != 1 || target.Saturated {
		t.Fatalf("expired lease capacity = %#v", target)
	}
}

func exercisePlacementProjectionContract(t *testing.T, store placementProjectionStore) {
	t.Helper()
	ctx := context.Background()
	profile, err := contract.NewExecutionProfile("image-a", "linux", "amd64", "bun", "1.2.3", "glibc-2.39")
	if err != nil {
		t.Fatal(err)
	}
	deployment := routingPolicyDeployment("commit-projection", "ready", "ready", []string{"gpu"}, nil)
	deployment.ExecutionProfile = profile
	if _, err := store.PublishRelease(ctx, deployment, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	ready := createPlacementCredential(t, store, "group-ready", []string{"workspace-a"}, 1)
	draining := createPlacementCredential(t, store, "group-draining", []string{"workspace-a"}, 2)
	createPlacementCredential(t, store, "group-offline", []string{"workspace-a"}, 3)
	hidden := createPlacementCredential(t, store, "group-hidden", []string{"workspace-b"}, 4)

	registerPlacementWorker(t, store, WorkerRecord{
		ID: "worker-ready-a", Group: ready.Group, Tags: []string{"ready"}, Labels: []string{"gpu"},
		ExecutionProfiles: []contract.ExecutionProfile{profile}, Slots: 2, Status: WorkerStatusActive,
		CredentialID: ready.ID, CredentialGeneration: ready.Generation, EngineVersion: "0.12.0", BuildRevision: "rev-a",
	})
	registerPlacementWorker(t, store, WorkerRecord{
		ID: "worker-ready-b", Group: ready.Group, Tags: []string{"ready"}, Labels: []string{"gpu"},
		ExecutionProfiles: []contract.ExecutionProfile{profile}, Slots: 3, Status: WorkerStatusActive,
		CredentialID: ready.ID, CredentialGeneration: ready.Generation, EngineVersion: "0.12.1", BuildRevision: "rev-b",
	})
	registerPlacementWorker(t, store, WorkerRecord{
		ID: "worker-draining", Group: draining.Group, Tags: []string{"ready"}, Labels: []string{"gpu"},
		ExecutionProfiles: []contract.ExecutionProfile{profile}, Slots: 7, Status: WorkerStatusActive,
		CredentialID: draining.ID, CredentialGeneration: draining.Generation,
	})
	registerPlacementWorker(t, store, WorkerRecord{
		ID: "worker-hidden", Group: hidden.Group, Tags: []string{"ready"}, Labels: []string{"gpu"},
		ExecutionProfiles: []contract.ExecutionProfile{profile}, Slots: 11, Status: WorkerStatusActive,
		CredentialID: hidden.ID, CredentialGeneration: hidden.Generation,
	})
	registerPlacementWorker(t, store, WorkerRecord{
		ID: "worker-static", Tags: []string{"ready"}, Labels: []string{"gpu"},
		ExecutionProfiles: []contract.ExecutionProfile{profile}, Slots: 4, Status: WorkerStatusActive,
	})

	deadline := time.Now().UTC().Add(10 * time.Minute)
	if _, replayed, err := store.PutWorkerGroupRunState(ctx, PutWorkerGroupRunStateRequest{
		Group: draining.Group, State: WorkerGroupDraining, ExpectedRevision: 0, DeadlineAt: &deadline,
		OperationID: "drain-projection", RequestFingerprint: "drain-projection-fingerprint", Actor: "test",
	}); err != nil || replayed {
		t.Fatalf("PutWorkerGroupRunState replayed=%t, err=%v", replayed, err)
	}

	workspaceInventory, err := store.GetWorkerGroupInventory(ctx, "workspace-a", false)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := inventoryGroupNames(workspaceInventory), []string{"group-draining", "group-offline", "group-ready", unmanagedWorkerGroup}; !reflect.DeepEqual(got, want) {
		t.Fatalf("workspace group names = %#v, want %#v", got, want)
	}
	readyInventory := requireInventoryGroup(t, workspaceInventory, "group-ready")
	if !readyInventory.VersionOrBuildDrift || readyInventory.Status != "degraded" || readyInventory.LiveWorkers != 2 || readyInventory.TotalSlots != 5 || readyInventory.OccupiedSlots != 0 || readyInventory.AvailableSlots != 5 {
		t.Fatalf("ready inventory = %#v", readyInventory)
	}
	if static := requireInventoryGroup(t, workspaceInventory, unmanagedWorkerGroup); static.Managed || static.LiveWorkers != 1 || static.TotalSlots != 4 || static.OccupiedSlots != 0 || static.AvailableSlots != 4 {
		t.Fatalf("static inventory = %#v", static)
	}
	if drainingInventory := requireInventoryGroup(t, workspaceInventory, "group-draining"); drainingInventory.Status != WorkerGroupDraining || drainingInventory.AvailableSlots != 0 {
		t.Fatalf("draining inventory = %#v", drainingInventory)
	}
	if offline := requireInventoryGroup(t, workspaceInventory, "group-offline"); offline.Status != "offline" || offline.LiveWorkers != 0 {
		t.Fatalf("offline inventory = %#v", offline)
	}

	adminInventory, err := store.GetWorkerGroupInventory(ctx, "workspace-a", true)
	if err != nil {
		t.Fatal(err)
	}
	hiddenInventory := requireInventoryGroup(t, adminInventory, "group-hidden")
	if hiddenInventory.WorkspaceAllowed {
		t.Fatalf("hidden group was marked workspace-allowed: %#v", hiddenInventory)
	}

	workspaceCandidates, err := store.GetPlacementCandidates(ctx, "workspace-a", "echo", "run", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaceCandidates.Targets) != 1 {
		t.Fatalf("workspace targets = %#v", workspaceCandidates.Targets)
	}
	target := workspaceCandidates.Targets[0]
	if target.Action != "run" || target.MatchingWorkers != 3 || target.MatchingSlots != 9 {
		t.Fatalf("workspace placement target = %#v", target)
	}
	readyCandidate := requireCandidateGroup(t, target, "group-ready")
	if !readyCandidate.Eligible || !readyCandidate.VersionOrBuildDrift || readyCandidate.MatchingWorkers != 2 || readyCandidate.MatchingSlots != 5 {
		t.Fatalf("ready candidate = %#v", readyCandidate)
	}
	if static := requireCandidateGroup(t, target, unmanagedWorkerGroup); !static.Eligible || static.MatchingWorkers != 1 || static.MatchingSlots != 4 {
		t.Fatalf("static candidate = %#v", static)
	}
	if drainingCandidate := requireCandidateGroup(t, target, "group-draining"); drainingCandidate.Eligible || !slices.Contains(drainingCandidate.ReasonCodes, PlacementReasonDraining) {
		t.Fatalf("draining candidate = %#v", drainingCandidate)
	}
	if offline := requireCandidateGroup(t, target, "group-offline"); offline.Eligible || !slices.Contains(offline.ReasonCodes, PlacementReasonNoLiveCapacity) {
		t.Fatalf("offline candidate = %#v", offline)
	}

	adminCandidates, err := store.GetPlacementCandidates(ctx, "workspace-a", "echo", "run", true)
	if err != nil {
		t.Fatal(err)
	}
	hiddenCandidate := requireCandidateGroup(t, adminCandidates.Targets[0], "group-hidden")
	if hiddenCandidate.Eligible || !slices.Contains(hiddenCandidate.ReasonCodes, PlacementReasonWorkspaceNotAllowed) {
		t.Fatalf("hidden candidate = %#v", hiddenCandidate)
	}

	leaseExpiresAt := time.Now().UTC().Add(10 * time.Minute)
	createPlacementJob(t, store, deployment, "run-occupied", "workspace-a", "ready", JobRunning, "worker-ready-a", &leaseExpiresAt)
	createPlacementJob(t, store, deployment, "run-queued-a", "workspace-a", "ready", JobQueued, "", nil)
	createPlacementJob(t, store, deployment, "run-queued-b", "workspace-a", "ready", JobQueued, "", nil)
	createPlacementJob(t, store, deployment, "run-legacy", "workspace-a", "legacy", JobQueued, "", nil)
	createPlacementJob(t, store, deployment, "run-other-workspace", "workspace-b", "ready", JobQueued, "", nil)

	demand, err := store.GetExecutionDemand(ctx, "workspace-a", "echo", "run", false)
	if err != nil {
		t.Fatal(err)
	}
	if demand.Workspace != "workspace-a" || demand.QueuedJobs != 3 || demand.OldestQueuedAt == nil || len(demand.Targets) != 2 {
		t.Fatalf("execution demand = %#v", demand)
	}
	readyDemand := requireDemandTarget(t, demand, "ready")
	if readyDemand.QueuedJobs != 2 || readyDemand.MatchingWorkers != 3 || readyDemand.TotalSlots != 9 || readyDemand.OccupiedSlots != 1 || readyDemand.AvailableSlots != 8 || readyDemand.Saturated {
		t.Fatalf("ready execution demand = %#v", readyDemand)
	}
	if candidate := requireCandidateGroup(t, PlacementTargetCandidates{Candidates: readyDemand.Candidates}, "group-ready"); candidate.OccupiedSlots != 1 || candidate.AvailableSlots != 4 || candidate.Saturated {
		t.Fatalf("ready demand candidate = %#v", candidate)
	}
	if candidate := requireCandidateGroup(t, PlacementTargetCandidates{Candidates: readyDemand.Candidates}, unmanagedWorkerGroup); candidate.OccupiedSlots != 0 || candidate.AvailableSlots != 4 {
		t.Fatalf("static demand candidate = %#v", candidate)
	}
	legacyDemand := requireDemandTarget(t, demand, "legacy")
	if legacyDemand.QueuedJobs != 1 || legacyDemand.MatchingWorkers != 0 || legacyDemand.TotalSlots != 0 || legacyDemand.OccupiedSlots != 0 || legacyDemand.AvailableSlots != 0 || legacyDemand.Saturated {
		t.Fatalf("legacy execution demand = %#v", legacyDemand)
	}
	var counted int64
	for _, demandTarget := range demand.Targets {
		counted += demandTarget.QueuedJobs
	}
	if counted != demand.QueuedJobs {
		t.Fatalf("target demand sum = %d, workspace demand = %d", counted, demand.QueuedJobs)
	}
	adminDemand, err := store.GetExecutionDemand(ctx, "workspace-a", "echo", "run", true)
	if err != nil {
		t.Fatal(err)
	}
	hiddenDemandCandidate := requireCandidateGroup(t, PlacementTargetCandidates{Candidates: requireDemandTarget(t, adminDemand, "ready").Candidates}, "group-hidden")
	if hiddenDemandCandidate.Eligible || hiddenDemandCandidate.MatchingSlots != 0 || !slices.Contains(hiddenDemandCandidate.ReasonCodes, PlacementReasonWorkspaceNotAllowed) {
		t.Fatalf("hidden demand candidate = %#v", hiddenDemandCandidate)
	}
	updatedInventory, err := store.GetWorkerGroupInventory(ctx, "workspace-a", false)
	if err != nil {
		t.Fatal(err)
	}
	if ready := requireInventoryGroup(t, updatedInventory, "group-ready"); ready.TotalSlots != 5 || ready.OccupiedSlots != 1 || ready.AvailableSlots != 4 {
		t.Fatalf("updated ready inventory = %#v", ready)
	}

	encoded, err := json.Marshal(struct {
		Inventory  WorkerGroupInventory `json:"inventory"`
		Candidates PlacementCandidates  `json:"candidates"`
		Demand     ExecutionDemand      `json:"demand"`
	}{workspaceInventory, workspaceCandidates, demand})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"worker-ready-a", ready.ID, "placement-token-1"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("workspace projection exposed %q: %s", forbidden, encoded)
		}
	}
}

func createPlacementJob(
	t *testing.T,
	store Store,
	deployment contract.Deployment,
	runID string,
	workspace string,
	tag string,
	state JobState,
	leaseOwner string,
	leaseExpiresAt *time.Time,
) {
	t.Helper()
	run := NewRun("api", runID, "echo", "run", deployment, json.RawMessage(`{}`))
	job := NewActionJob(run, nil)
	job.ID = "job-" + runID
	job.Payload.Workspace = workspace
	job.Payload.Tag = tag
	job.State = state
	job.LeaseOwner = leaseOwner
	job.LeaseExpiresAt = leaseExpiresAt
	if err := store.CreateRunAndEnqueue(context.Background(), run, job); err != nil {
		t.Fatalf("CreateRunAndEnqueue(%s): %v", runID, err)
	}
}

func createPlacementCredential(t *testing.T, store WorkerControlStore, group string, workspaceIDs []string, index int) WorkerCredential {
	t.Helper()
	credential, replayed, err := store.CreateWorkerCredential(context.Background(), CreateWorkerCredentialRequest{
		Group: group, ExpectedGeneration: 0, WorkspaceIDs: workspaceIDs,
		TokenHash:   HashBearerToken(fmt.Sprintf("placement-token-%d", index)),
		OperationID: "placement-credential-" + group, RequestFingerprint: "placement-fingerprint-" + group, Actor: "test",
	})
	if err != nil || replayed {
		t.Fatalf("CreateWorkerCredential(%s) replayed=%t, err=%v", group, replayed, err)
	}
	return credential
}

func registerPlacementWorker(t *testing.T, store Store, worker WorkerRecord) {
	t.Helper()
	if err := store.RegisterWorker(context.Background(), worker); err != nil {
		t.Fatalf("RegisterWorker(%s): %v", worker.ID, err)
	}
}

func inventoryGroupNames(inventory WorkerGroupInventory) []string {
	names := make([]string, 0, len(inventory.Groups))
	for _, group := range inventory.Groups {
		names = append(names, group.Group)
	}
	return names
}

func requireInventoryGroup(t *testing.T, inventory WorkerGroupInventory, group string) WorkerGroupInventoryItem {
	t.Helper()
	for _, item := range inventory.Groups {
		if item.Group == group {
			return item
		}
	}
	t.Fatalf("inventory group %q not found in %#v", group, inventory.Groups)
	return WorkerGroupInventoryItem{}
}

func requireCandidateGroup(t *testing.T, target PlacementTargetCandidates, group string) WorkerGroupPlacementCandidate {
	t.Helper()
	for _, candidate := range target.Candidates {
		if candidate.Group == group {
			return candidate
		}
	}
	t.Fatalf("candidate group %q not found in %#v", group, target.Candidates)
	return WorkerGroupPlacementCandidate{}
}

func requireDemandTarget(t *testing.T, demand ExecutionDemand, tag string) ExecutionDemandTarget {
	t.Helper()
	for _, target := range demand.Targets {
		if target.EffectiveTag == tag {
			return target
		}
	}
	t.Fatalf("execution demand target %q not found in %#v", tag, demand.Targets)
	return ExecutionDemandTarget{}
}
