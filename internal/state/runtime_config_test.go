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

func TestLocalRuntimeVariableMutationIsAttemptBoundIdempotentAndCASProtected(t *testing.T) {
	exerciseRuntimeVariableMutationIsAttemptBoundIdempotentAndCASProtected(t, NewLocalStore(filepath.Join(t.TempDir(), "state.json")))
}

func TestRuntimeSecretStoredEnvelopeUsesPlaintextLimit(t *testing.T) {
	request := RuntimeVariableMutationRequest{
		Value:          strings.Repeat("x", RuntimeConfigMaxValueBytes+1),
		IsSecret:       true,
		PlaintextBytes: RuntimeConfigMaxValueBytes,
	}
	if err := validateRuntimeVariableValue(request); err != nil {
		t.Fatalf("valid encrypted Secret envelope rejected: %v", err)
	}
	request.PlaintextBytes++
	if runtimeConfigCode(validateRuntimeVariableValue(request)) != RuntimeConfigCodeLimitExceeded {
		t.Fatal("oversized Secret plaintext was accepted")
	}
	request.PlaintextBytes = RuntimeConfigMaxValueBytes
	request.Value = strings.Repeat("x", RuntimeConfigMaxStoredSecretBytes+1)
	if runtimeConfigCode(validateRuntimeVariableValue(request)) != RuntimeConfigCodeLimitExceeded {
		t.Fatal("oversized Secret backend value was accepted")
	}
}

func TestPostgresRuntimeVariableMutationIsAttemptBoundIdempotentAndCASProtected(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	exerciseRuntimeVariableMutationIsAttemptBoundIdempotentAndCASProtected(t, openIsolatedPostgresCatalogStore(t, dsn))
}

func exerciseRuntimeVariableMutationIsAttemptBoundIdempotentAndCASProtected(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	job := enqueueRuntimeConfigJob(t, store, contract.RuntimeAccess{
		VariableTargets: []contract.RuntimeConfigTarget{{Scope: contract.RuntimeConfigScopeApp, Path: "session/token"}},
		WriteVariables: []contract.RuntimeVariableWriteTarget{{
			RuntimeConfigTarget: contract.RuntimeConfigTarget{Scope: contract.RuntimeConfigScopeApp, Path: "session/token"},
			Storage:             contract.RuntimeVariableStorageSecret,
		}},
	})

	request := RuntimeVariableMutationRequest{
		WorkspaceID: "ws-a", AppKey: "publisher", Path: "session/token",
		Value: "encrypted-value", IsSecret: true, OperationID: "op-1",
		RequestFingerprint: "sha256:same", JobID: job.ID, Attempt: job.Attempt,
	}
	created, err := store.MutateRuntimeVariable(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if created.Revision != 1 || created.Replayed {
		t.Fatalf("created = %#v", created)
	}
	replayed, err := store.MutateRuntimeVariable(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Revision != 1 || !replayed.Replayed {
		t.Fatalf("replayed = %#v", replayed)
	}

	conflicting := request
	conflicting.RequestFingerprint = "sha256:different"
	if _, err := store.MutateRuntimeVariable(ctx, conflicting); runtimeConfigCode(err) != RuntimeConfigCodeOperationConflict {
		t.Fatalf("operation conflict error = %v", err)
	}

	stale := request
	stale.OperationID = "op-2"
	stale.RequestFingerprint = "sha256:stale"
	stale.ExpectedRevision = int64Pointer(0)
	if _, err := store.MutateRuntimeVariable(ctx, stale); runtimeConfigCode(err) != RuntimeConfigCodeRevisionConflict {
		t.Fatalf("revision conflict error = %v", err)
	}

	variable, found, err := store.GetVariableScoped(ctx, "ws-a", contract.RuntimeConfigScopeApp, "publisher", "session/token")
	if err != nil || !found || variable.Revision != 1 || !variable.IsSecret {
		t.Fatalf("variable = %#v, found=%v, err=%v", variable, found, err)
	}
	audits, err := store.ListRuntimeConfigAudit(ctx, "ws-a", "publisher")
	if err != nil {
		t.Fatal(err)
	}
	if len(audits) != 1 || audits[0].OperationID != "op-1" || audits[0].Storage != "secret" {
		t.Fatalf("audits = %#v", audits)
	}
}

func TestLocalRuntimeMutationRejectsStorageDowngradeAndReferencePlanting(t *testing.T) {
	exerciseRuntimeMutationRejectsStorageDowngradeAndReferencePlanting(t, NewLocalStore(filepath.Join(t.TempDir(), "state.json")))
}

func TestPostgresRuntimeMutationRejectsStorageDowngradeAndReferencePlanting(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	exerciseRuntimeMutationRejectsStorageDowngradeAndReferencePlanting(t, openIsolatedPostgresCatalogStore(t, dsn))
}

func exerciseRuntimeMutationRejectsStorageDowngradeAndReferencePlanting(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	if err := store.SetResourceType(ctx, "ws-a", ResourceType{
		Name: "connection", Version: "1", Schema: json.RawMessage(`{"type":"object"}`),
	}); err != nil {
		t.Fatal(err)
	}
	job := enqueueRuntimeConfigJob(t, store, contract.RuntimeAccess{
		VariableTargets: []contract.RuntimeConfigTarget{{Scope: contract.RuntimeConfigScopeApp, Path: "session/allowed"}},
		ResourceTargets: []contract.RuntimeConfigTarget{{Scope: contract.RuntimeConfigScopeApp, Path: "session/profile"}},
		WriteVariables: []contract.RuntimeVariableWriteTarget{{
			RuntimeConfigTarget: contract.RuntimeConfigTarget{Scope: contract.RuntimeConfigScopeApp, Path: "session/secret"},
			Storage:             contract.RuntimeVariableStorageSecret,
		}},
		WriteResources: []contract.RuntimeConfigTarget{{Scope: contract.RuntimeConfigScopeApp, Path: "session/profile"}},
	})

	_, err := store.MutateRuntimeVariable(ctx, RuntimeVariableMutationRequest{
		WorkspaceID: "ws-a", AppKey: "publisher", Path: "session/secret",
		Value: "plain", IsSecret: false, OperationID: "plain-downgrade",
		RequestFingerprint: "sha256:plain", JobID: job.ID, Attempt: job.Attempt,
	})
	if runtimeConfigCode(err) != RuntimeConfigCodeStorageClassMismatch {
		t.Fatalf("storage downgrade error = %v", err)
	}

	_, err = store.MutateRuntimeResource(ctx, RuntimeResourceMutationRequest{
		WorkspaceID: "ws-a", AppKey: "publisher", Path: "session/profile",
		Value:        json.RawMessage(`{"token":"$var@app:session/not-allowed"}`),
		ResourceType: "connection@1", OperationID: "plant-reference",
		RequestFingerprint: "sha256:plant", JobID: job.ID, Attempt: job.Attempt,
	})
	if runtimeConfigCode(err) != RuntimeConfigCodeReferenceForbidden {
		t.Fatalf("reference planting error = %v", err)
	}
	if _, found, err := store.GetResourceScoped(ctx, "ws-a", contract.RuntimeConfigScopeApp, "publisher", "session/profile"); err != nil || found {
		t.Fatalf("resource found=%v err=%v after rejected planting", found, err)
	}
}

func TestEnsureSnapshotMigratesLegacyRuntimeConfigurationOwnership(t *testing.T) {
	snapshot := Snapshot{
		Variables: map[string]map[string]Variable{"ws-a": {
			"legacy": {AppKey: "", Path: "workspace/token", Revision: 0},
			"app":    {AppKey: "publisher", Path: "app/token", Revision: 0},
		}},
		Resources: map[string]map[string]Resource{"ws-a": {
			"legacy": {Path: "database/main", Value: json.RawMessage(`{}`)},
		}},
	}
	ensureSnapshot(&snapshot)
	workspaceVariable := snapshot.Variables["ws-a"][runtimeConfigObjectKey(contract.RuntimeConfigScopeWorkspace, "", "workspace/token")]
	appVariable := snapshot.Variables["ws-a"][runtimeConfigObjectKey(contract.RuntimeConfigScopeApp, "publisher", "app/token")]
	workspaceResource := snapshot.Resources["ws-a"][runtimeConfigObjectKey(contract.RuntimeConfigScopeWorkspace, "", "database/main")]
	if workspaceVariable.OwnerScope != contract.RuntimeConfigScopeWorkspace || workspaceVariable.Revision != 1 {
		t.Fatalf("workspace variable = %#v", workspaceVariable)
	}
	if appVariable.OwnerScope != contract.RuntimeConfigScopeApp || appVariable.Revision != 1 {
		t.Fatalf("app variable = %#v", appVariable)
	}
	if workspaceResource.OwnerScope != contract.RuntimeConfigScopeWorkspace || workspaceResource.Revision != 1 {
		t.Fatalf("workspace resource = %#v", workspaceResource)
	}
}

func TestLocalRuntimeConfigProvisioningBatchIsAtomicAndPreservesRevisions(t *testing.T) {
	exerciseRuntimeConfigProvisioningBatchIsAtomicAndPreservesRevisions(t, NewLocalStore(filepath.Join(t.TempDir(), "state.json")))
}

func TestPostgresRuntimeConfigProvisioningBatchIsAtomicAndPreservesRevisions(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	exerciseRuntimeConfigProvisioningBatchIsAtomicAndPreservesRevisions(t, openIsolatedPostgresCatalogStore(t, dsn))
}

func exerciseRuntimeConfigProvisioningBatchIsAtomicAndPreservesRevisions(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	if err := store.SetResourceType(ctx, "ws-a", ResourceType{
		Name: "connection", Version: "1", Schema: json.RawMessage(`{"type":"object","required":["url"],"properties":{"url":{"type":"string"}}}`),
	}); err != nil {
		t.Fatal(err)
	}
	invalid := RuntimeConfigProvisioningBatch{
		WorkspaceID: "ws-a", Actor: "provisioner",
		Variables: []ProvisionedRuntimeVariable{{AppKey: "publisher", Path: "session/cursor", Value: "cursor", Revision: 3}},
		Resources: []ProvisionedRuntimeResource{{AppKey: "publisher", Path: "session/untyped", Value: json.RawMessage(`{"ok":true}`)}},
	}
	if err := store.ApplyRuntimeConfigProvisioningBatch(ctx, invalid); err == nil {
		t.Fatal("invalid provisioning batch succeeded")
	}
	if _, found, err := store.GetVariableScoped(ctx, "ws-a", contract.RuntimeConfigScopeApp, "publisher", "session/cursor"); err != nil || found {
		t.Fatalf("failed batch partially wrote Variable: found=%v err=%v", found, err)
	}

	valid := RuntimeConfigProvisioningBatch{
		WorkspaceID: "ws-a", Actor: "provisioner",
		Variables:  []ProvisionedRuntimeVariable{{AppKey: "publisher", Path: "session/cursor", Value: "cursor", Revision: 3}},
		Resources:  []ProvisionedRuntimeResource{{AppKey: "publisher", Path: "session/profile", Value: json.RawMessage(`{"url":"https://app.invalid"}`), ResourceType: "connection@1", Revision: 4}},
		Lifecycles: []ProvisionedAppRuntimeLifecycle{{AppKey: "publisher", State: AppRuntimeTombstoned, Reason: "retiring", Revision: 2}},
	}
	dryRun := valid
	dryRun.DryRun = true
	if err := store.ApplyRuntimeConfigProvisioningBatch(ctx, dryRun); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.GetResourceScoped(ctx, "ws-a", contract.RuntimeConfigScopeApp, "publisher", "session/profile"); err != nil || found {
		t.Fatalf("dry-run wrote Resource: found=%v err=%v", found, err)
	}
	if lifecycle, err := store.GetAppRuntimeLifecycle(ctx, "ws-a", "publisher"); err != nil || lifecycle.Revision != 0 || lifecycle.State != AppRuntimeActive {
		t.Fatalf("dry-run wrote lifecycle: %#v err=%v", lifecycle, err)
	}

	if err := store.ApplyRuntimeConfigProvisioningBatch(ctx, valid); err != nil {
		t.Fatal(err)
	}
	variable, found, err := store.GetVariableScoped(ctx, "ws-a", contract.RuntimeConfigScopeApp, "publisher", "session/cursor")
	if err != nil || !found || variable.Revision != 3 || variable.Value != "cursor" {
		t.Fatalf("provisioned Variable = %#v found=%v err=%v", variable, found, err)
	}
	resource, found, err := store.GetResourceScoped(ctx, "ws-a", contract.RuntimeConfigScopeApp, "publisher", "session/profile")
	if err != nil || !found || resource.Revision != 4 || resource.ResourceType != "connection@1" {
		t.Fatalf("provisioned Resource = %#v found=%v err=%v", resource, found, err)
	}
	lifecycle, err := store.GetAppRuntimeLifecycle(ctx, "ws-a", "publisher")
	if err != nil || lifecycle.Revision != 2 || lifecycle.State != AppRuntimeTombstoned || lifecycle.Reason != "retiring" {
		t.Fatalf("provisioned lifecycle = %#v err=%v", lifecycle, err)
	}
	for name, reapply := range map[string]RuntimeConfigProvisioningBatch{
		"variable":  {WorkspaceID: valid.WorkspaceID, Actor: valid.Actor, Variables: valid.Variables},
		"resource":  {WorkspaceID: valid.WorkspaceID, Actor: valid.Actor, Resources: valid.Resources},
		"lifecycle": {WorkspaceID: valid.WorkspaceID, Actor: valid.Actor, Lifecycles: valid.Lifecycles},
	} {
		if err := store.ApplyRuntimeConfigProvisioningBatch(ctx, reapply); err != nil {
			t.Fatalf("idempotent %s reapply failed: %v", name, err)
		}
	}
	audits, err := store.ListRuntimeConfigAudit(ctx, "ws-a", "publisher")
	if err != nil || len(audits) != 2 {
		t.Fatalf("runtime provisioning audits = %#v err=%v", audits, err)
	}
	lifecycleAudits, err := store.ListAppRuntimeLifecycleAudit(ctx, "ws-a", "publisher")
	if err != nil || len(lifecycleAudits) != 1 {
		t.Fatalf("lifecycle provisioning audits = %#v err=%v", lifecycleAudits, err)
	}
}

func enqueueRuntimeConfigJob(t *testing.T, store Store, access contract.RuntimeAccess) Job {
	t.Helper()
	ctx := context.Background()
	deployment := contract.Deployment{Workspace: "ws-a", App: "publisher", Actions: map[string]contract.Action{
		"run": {RuntimeAccess: access},
	}}
	run := NewRun("windforce", "runtime-config-run-"+NewID("test"), "publisher", "run", deployment, json.RawMessage(`{}`))
	job := NewActionJob(run, json.RawMessage(`{}`))
	job.Payload.RuntimeAccess = contract.CloneRuntimeAccess(access)
	if err := store.CreateRunAndEnqueue(ctx, run, job); err != nil {
		t.Fatal(err)
	}
	claimed, _, err := store.ClaimJob(ctx, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return claimed
}

func runtimeConfigCode(err error) string {
	var typed *RuntimeConfigError
	if errors.As(err, &typed) {
		return typed.Code
	}
	return ""
}

func int64Pointer(value int64) *int64 { return &value }
