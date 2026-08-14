package state

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
)

type placementPreconditionStore interface {
	Store
	routingPolicyCatalog
	UpdateRoutingPolicyWithPrecondition(context.Context, PlacementPolicyMutationRequest) (PlacementPolicyMutationResult, error)
	GetRoutingPolicy(context.Context, string, string) (catalog.RoutingPolicy, error)
	AuditTrail(context.Context, string, string) ([]catalog.AuditRecord, error)
}

func TestLocalPlacementPolicyPreconditionContract(t *testing.T) {
	exercisePlacementPolicyPreconditionContract(t, NewLocalStore(t.TempDir()+"/state.json"))
}

func TestPostgresPlacementPolicyPreconditionContract(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	exercisePlacementPolicyPreconditionContract(t, openIsolatedPostgresCatalogStore(t, dsn))
}

func TestActionPlacementPreconditionChecksOnlyExactAction(t *testing.T) {
	ctx := context.Background()
	store := NewLocalStore(t.TempDir() + "/state.json")
	deployment := routingPolicyDeployment("commit-action", "app-tag", "run-tag", nil, nil)
	siblingTag := "sibling-without-worker"
	deployment.Actions["sibling"] = contract.Action{Action: "sibling", Tag: &siblingTag}
	if _, err := store.PublishRelease(ctx, deployment, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterWorker(ctx, WorkerRecord{
		ID: "worker-run", Tags: []string{"run-ready"}, Slots: 1, Status: WorkerStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	runReady := "run-ready"
	result, err := store.UpdateRoutingPolicyWithPrecondition(ctx, PlacementPolicyMutationRequest{
		WorkspaceID: "workspace-a", App: "echo", Action: "run",
		Patch:       catalog.RoutingPolicyPatch{RouteTagSet: true, RouteTagOverride: &runReady},
		OperationID: "op-action-run", ExpectedRevision: 0, MinimumMatchingSlots: 1,
		RequestFingerprint: "fingerprint-action-run", Actor: "operator@example.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Check.Targets) != 1 || result.Check.Targets[0].Action != "run" || result.Check.Targets[0].MatchingSlots != 1 {
		t.Fatalf("action placement result = %#v", result.Check)
	}
}

func TestPlacementPreconditionExcludesManagedWorkerInDrainingGroup(t *testing.T) {
	ctx := context.Background()
	store := NewLocalStore(t.TempDir() + "/state.json")
	deployment := routingPolicyDeployment("commit-managed", "ready", "ready", nil, nil)
	if _, err := store.PublishRelease(ctx, deployment, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	credential, replayed, err := store.CreateWorkerCredential(ctx, CreateWorkerCredentialRequest{
		Group: "group-a", ExpectedGeneration: 0, WorkspaceIDs: []string{"workspace-a"},
		TokenHash: HashBearerToken("managed-placement-test-value"), OperationID: "op-managed-credential",
		RequestFingerprint: "fingerprint-managed-credential", Actor: "operator@example.test",
	})
	if err != nil || replayed {
		t.Fatalf("credential = %#v, replayed=%t, err=%v", credential, replayed, err)
	}
	if err := store.RegisterWorker(ctx, WorkerRecord{
		ID: "managed-worker", Group: credential.Group, Tags: []string{"ready"}, Slots: 1, Status: WorkerStatusActive,
		CredentialID: credential.ID, CredentialGeneration: credential.Generation,
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().UTC().Add(10 * time.Minute)
	if _, _, err := store.PutWorkerGroupRunState(ctx, PutWorkerGroupRunStateRequest{
		Group: "group-a", State: WorkerGroupDraining, OperationID: "op-managed-drain", ExpectedRevision: 0,
		DeadlineAt: &deadline, RequestFingerprint: "fingerprint-managed-drain", Actor: "operator@example.test",
	}); err != nil {
		t.Fatal(err)
	}
	ready := "ready"
	request := PlacementPolicyMutationRequest{
		WorkspaceID: "workspace-a", App: "echo", Patch: catalog.RoutingPolicyPatch{RouteTagSet: true, RouteTagOverride: &ready},
		OperationID: "op-managed-placement", ExpectedRevision: 0, MinimumMatchingSlots: 1,
		RequestFingerprint: "fingerprint-managed-placement", Actor: "operator@example.test",
	}
	failed, err := store.UpdateRoutingPolicyWithPrecondition(ctx, request)
	if !errors.Is(err, ErrInsufficientPlacementCapacity) || failed.Check.Targets[0].MatchingSlots != 0 {
		t.Fatalf("draining group result = %#v, err=%v", failed, err)
	}
	if _, _, err := store.PutWorkerGroupRunState(ctx, PutWorkerGroupRunStateRequest{
		Group: "group-a", State: WorkerGroupRunning, OperationID: "op-managed-run", ExpectedRevision: 1,
		RequestFingerprint: "fingerprint-managed-run", Actor: "operator@example.test",
	}); err != nil {
		t.Fatal(err)
	}
	result, err := store.UpdateRoutingPolicyWithPrecondition(ctx, request)
	if err != nil || result.Check.Targets[0].MatchingSlots != 1 {
		t.Fatalf("running group result = %#v, err=%v", result, err)
	}
}

func exercisePlacementPolicyPreconditionContract(t *testing.T, store placementPreconditionStore) {
	t.Helper()
	ctx := context.Background()
	profile, err := contract.NewExecutionProfile("image-a", "linux", "amd64", "bun", "1.2.3", "glibc-2.39")
	if err != nil {
		t.Fatal(err)
	}
	deployment := routingPolicyDeployment("commit-capacity", "manifest-app", "manifest-action", []string{"linux"}, []string{"browser"})
	deployment.ExecutionProfile = profile
	siblingTag := "manifest-sibling"
	siblingLabels := []string{"sibling"}
	deployment.Actions["sibling"] = contract.Action{Action: "sibling", Tag: &siblingTag, RunsOn: &siblingLabels}
	withProfile, err := contract.WithExecutionProfileLabel(deployment.RequiredLabels, profile)
	if err != nil {
		t.Fatal(err)
	}
	deployment.RequiredLabels = withProfile
	if _, err := store.PublishRelease(ctx, deployment, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterWorker(ctx, WorkerRecord{
		ID: "worker-capacity", Tags: []string{"ready"}, Labels: []string{"gpu"},
		ExecutionProfiles: []contract.ExecutionProfile{profile}, Slots: 3, Status: WorkerStatusActive,
	}); err != nil {
		t.Fatal(err)
	}

	readyTag := "ready"
	gpuLabels := []string{"gpu"}
	request := PlacementPolicyMutationRequest{
		WorkspaceID: "workspace-a", App: "echo",
		Patch: catalog.RoutingPolicyPatch{
			RouteTagSet: true, RouteTagOverride: &readyTag,
			RequiredLabelsSet: true, RequiredLabelsOverride: &gpuLabels,
		},
		OperationID: "op-capacity-1", ExpectedRevision: 0, MinimumMatchingSlots: 2,
		RequestFingerprint: "fingerprint-capacity-1", Actor: "operator@example.test",
	}
	result, err := store.UpdateRoutingPolicyWithPrecondition(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Replayed || result.Policy.Revision != 1 || result.Check.AppliedRevision != 1 || len(result.Check.Targets) != 3 {
		t.Fatalf("placement result = %#v", result)
	}
	for _, target := range result.Check.Targets {
		if target.EffectiveTag != readyTag || target.MatchingWorkers != 1 || target.MatchingSlots != 3 {
			t.Fatalf("target observation = %#v", target)
		}
		profileLabel, err := contract.ExecutionProfileLabel(profile)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(target.EffectiveRequiredLabels, []string{"gpu", profileLabel}) {
			t.Fatalf("effective labels = %#v", target.EffectiveRequiredLabels)
		}
	}
	auditsAfterCommit := placementAuditCount(t, store)

	if err := store.DeregisterWorker(ctx, "worker-capacity"); err != nil {
		t.Fatal(err)
	}
	replay, err := store.UpdateRoutingPolicyWithPrecondition(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replayed || !reflect.DeepEqual(replay.Check, result.Check) || replay.Policy.Revision != 1 {
		t.Fatalf("replay = %#v, original = %#v", replay, result)
	}
	if got := placementAuditCount(t, store); got != auditsAfterCommit {
		t.Fatalf("replay audit count = %d, want %d", got, auditsAfterCommit)
	}

	conflictingReplay := request
	conflictingReplay.RequestFingerprint = "different-fingerprint"
	if _, err := store.UpdateRoutingPolicyWithPrecondition(ctx, conflictingReplay); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting replay error = %v", err)
	}
	missingTag := "missing"
	insufficient := request
	insufficient.OperationID = "op-capacity-2"
	insufficient.ExpectedRevision = 1
	insufficient.RequestFingerprint = "fingerprint-capacity-2"
	insufficient.Patch.RouteTagOverride = &missingTag
	failed, err := store.UpdateRoutingPolicyWithPrecondition(ctx, insufficient)
	if !errors.Is(err, ErrInsufficientPlacementCapacity) || len(failed.Check.Targets) != 3 {
		t.Fatalf("insufficient result = %#v, error = %v", failed, err)
	}
	for _, target := range failed.Check.Targets {
		if target.MatchingWorkers != 0 || target.MatchingSlots != 0 {
			t.Fatalf("insufficient target = %#v", target)
		}
	}
	stale := request
	stale.OperationID = "op-capacity-stale"
	stale.RequestFingerprint = "fingerprint-capacity-stale"
	if _, err := store.UpdateRoutingPolicyWithPrecondition(ctx, stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision error = %v", err)
	}
	policy, err := store.GetRoutingPolicy(ctx, "workspace-a", "echo")
	if err != nil {
		t.Fatal(err)
	}
	if policy.Revision != 1 || policy.LastOperationID != request.OperationID || policy.LastRequestFingerprint != request.RequestFingerprint {
		t.Fatalf("policy changed after rejected requests: %#v", policy)
	}
	if got := placementAuditCount(t, store); got != auditsAfterCommit {
		t.Fatalf("rejected request audit count = %d, want %d", got, auditsAfterCommit)
	}

	run := NewRun("workspace-a", "queued-after-capacity", "echo", "run", result.Deployment, []byte(`{}`))
	job := NewActionJob(run, nil)
	if err := store.CreateRunAndEnqueue(ctx, run, job); err != nil {
		t.Fatal(err)
	}
	queued, _, ok, err := store.GetJob(ctx, "workspace-a", job.ID)
	if err != nil || !ok || queued.State != JobQueued {
		t.Fatalf("job after Worker disappearance = %#v, ok=%t, err=%v", queued, ok, err)
	}
}

func placementAuditCount(t *testing.T, store placementPreconditionStore) int {
	t.Helper()
	records, err := store.AuditTrail(context.Background(), "workspace-a", "source-a")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, record := range records {
		if record.Kind == "execution_placement_updated" {
			count++
		}
	}
	return count
}

func TestLocalRoutingPolicyMigrationDefaultsRevisionToZero(t *testing.T) {
	path := t.TempDir() + "/state.json"
	if err := os.WriteFile(path, []byte(`{"releaseCatalog":{"routingPolicies":{"workspace-a/echo":{"workspace":"workspace-a","app":"echo","routeTagOverride":"browser","actions":{}}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := NewLocalStore(path).GetRoutingPolicy(context.Background(), "workspace-a", "echo")
	if err != nil {
		t.Fatal(err)
	}
	if policy.Revision != 0 || policy.RouteTagOverride == nil || *policy.RouteTagOverride != "browser" {
		t.Fatalf("migrated local policy = %#v", policy)
	}
}

func TestPlacementWorkerEligibilityTreatsLegacyEmptyStatusAsActive(t *testing.T) {
	now := time.Now().UTC()
	if !placementWorkerEligible(WorkerRecord{ID: "legacy", LastHeartbeatAt: now, Slots: 1}, "workspace-a", now, nil, nil) {
		t.Fatal("legacy Worker status must retain the active default")
	}
}

func TestPostgresRoutingPolicyMigrationPersistsRevisionZero(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	store := openIsolatedPostgresCatalogStore(t, dsn)
	ctx := context.Background()
	if _, err := store.pool.Exec(ctx, `
INSERT INTO control_routing_policy (workspace_id, app_key, policy)
VALUES ('workspace-a', 'echo', '{"workspace":"workspace-a","app":"echo","actions":{}}'::jsonb)
`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var hasRevision bool
	if err := store.pool.QueryRow(ctx, `
SELECT policy ? 'revision' FROM control_routing_policy WHERE workspace_id='workspace-a' AND app_key='echo'
`).Scan(&hasRevision); err != nil {
		t.Fatal(err)
	}
	if !hasRevision {
		t.Fatal("PostgreSQL routing policy migration did not persist revision")
	}
}
