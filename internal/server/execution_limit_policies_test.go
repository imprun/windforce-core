package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/executionlimit"
	"github.com/imprun/windforce-core/internal/state"
)

func TestCanonicalExecutionLimitPolicyDesiredObservedEnforced(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	if _, err := store.CreateWorkspace(ctx, "team-a", "Team A", "test"); err != nil {
		t.Fatal(err)
	}
	deployment := executionLimitPolicyTestDeployment("team-a", 60)
	if _, err := store.PublishRelease(ctx, deployment, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(Config{Store: store, Catalog: store, AdminToken: "instance-admin"}))
	defer server.Close()

	get := func() canonicalExecutionLimitPolicyReadback {
		response := executionPolicyRequest(t, server.URL, http.MethodGet, "/api/w/team-a/apps/orders/execution-limit-policies", "instance-admin", nil)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET status=%d body=%s", response.StatusCode, readResponse(t, response))
		}
		var view canonicalExecutionLimitPolicyReadback
		decodeResponse(t, response, &view)
		return view
	}
	initial := get()
	if len(initial.Observed.Items) != 3 || len(initial.Desired.Items) != 0 || len(initial.Enforced.ActiveRelease) != 3 {
		t.Fatalf("initial readback = %#v", initial)
	}
	implicit := initial.Observed.Items[0]
	if implicit.PolicyID != executionlimit.ImplicitAppConcurrencyPolicyID || implicit.ReleaseCeiling == nil || *implicit.ReleaseCeiling != 4 {
		t.Fatalf("implicit shape = %#v", implicit)
	}
	body := map[string]any{
		"scope": implicit.Scope, "policy_id": implicit.PolicyID, "kind": implicit.Kind,
		"shape_fingerprint": implicit.ShapeFingerprint, "allowance": 2,
		"expected_revision": 0, "operation_id": "cloud-project-1", "reason": "protect shared browser capacity",
	}
	put := executionPolicyRequest(t, server.URL, http.MethodPut, "/api/w/team-a/apps/orders/execution-limit-policies", "instance-admin", body)
	if put.StatusCode != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", put.StatusCode, readResponse(t, put))
	}
	var mutation struct {
		Policy   canonicalExecutionLimitPolicy `json:"policy"`
		Replayed bool                          `json:"replayed"`
	}
	decodeResponse(t, put, &mutation)
	if mutation.Replayed || mutation.Policy.Revision != 1 || mutation.Policy.OperationID != "cloud-project-1" {
		t.Fatalf("mutation = %#v", mutation)
	}
	replay := executionPolicyRequest(t, server.URL, http.MethodPut, "/api/w/team-a/apps/orders/execution-limit-policies", "instance-admin", body)
	if replay.StatusCode != http.StatusOK {
		t.Fatalf("replay status=%d body=%s", replay.StatusCode, readResponse(t, replay))
	}
	decodeResponse(t, replay, &mutation)
	if !mutation.Replayed || mutation.Policy.Revision != 1 {
		t.Fatalf("replay = %#v", mutation)
	}
	conflictBody := cloneJSONMap(body)
	conflictBody["allowance"] = 1
	conflict := executionPolicyRequest(t, server.URL, http.MethodPut, "/api/w/team-a/apps/orders/execution-limit-policies", "instance-admin", conflictBody)
	if conflict.StatusCode != http.StatusConflict {
		t.Fatalf("conflict status=%d body=%s", conflict.StatusCode, readResponse(t, conflict))
	}
	var conflictPayload map[string]any
	decodeResponse(t, conflict, &conflictPayload)
	if conflictPayload["code"] != "execution_limit_policy_conflict" || conflictPayload["current_shape_fingerprint"] != implicit.ShapeFingerprint ||
		conflictPayload["compatibility"] != "applied" || conflictPayload["current_operator_allowance"] != float64(2) || conflictPayload["current_effective_limit"] != float64(2) {
		t.Fatalf("conflict payload = %#v", conflictPayload)
	}

	run := state.NewRun("api", "run-residual", "orders", "collect", deployment, json.RawMessage(`{}`))
	run.ExecutionLimits = state.ExecutionLimitPins{AppConcurrency: &state.AppConcurrencyLimitPin{
		PolicyID: executionlimit.ImplicitAppConcurrencyPolicyID, ShapeFingerprint: implicit.ShapeFingerprint, MaxConcurrent: int32TestPointer(4),
	}}
	job := state.NewActionJob(run, nil)
	job.ID = "job-residual"
	if err := store.CreateRunAndEnqueue(ctx, run, job); err != nil {
		t.Fatal(err)
	}
	readback := get()
	if len(readback.Desired.Items) != 1 || readback.Desired.Items[0].Status != "applied" ||
		len(readback.Enforced.ResidualCohorts) != 1 || readback.Enforced.ResidualCohorts[0].Queued != 1 ||
		readback.Enforced.ResidualCohorts[0].EffectiveLimit == nil || *readback.Enforced.ResidualCohorts[0].EffectiveLimit != 2 {
		t.Fatalf("readback after policy = %#v", readback)
	}
	if readback.Enforced.ActiveRelease[0].EffectiveLimit == nil || *readback.Enforced.ActiveRelease[0].EffectiveLimit != 2 {
		t.Fatalf("active effective limit = %#v", readback.Enforced.ActiveRelease[0])
	}
	secondRun := state.NewRun("api", "run-residual-two", "orders", "collect", deployment, json.RawMessage(`{}`))
	secondRun.ExecutionLimits = state.ExecutionLimitPins{AppConcurrency: &state.AppConcurrencyLimitPin{
		PolicyID: executionlimit.ImplicitAppConcurrencyPolicyID, ShapeFingerprint: implicit.ShapeFingerprint, MaxConcurrent: int32TestPointer(4),
	}}
	secondJob := state.NewActionJob(secondRun, nil)
	secondJob.ID = "job-residual-two"
	if err := store.CreateRunAndEnqueue(ctx, secondRun, secondJob); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ClaimJob(ctx, "residual-worker-one", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ClaimJob(ctx, "residual-worker-two", time.Minute); err != nil {
		t.Fatal(err)
	}
	tightenBody := cloneJSONMap(body)
	tightenBody["allowance"] = 1
	tightenBody["expected_revision"] = 1
	tightenBody["operation_id"] = "cloud-project-tighten-1"
	tighten := executionPolicyRequest(t, server.URL, http.MethodPut, "/api/w/team-a/apps/orders/execution-limit-policies", "instance-admin", tightenBody)
	if tighten.StatusCode != http.StatusOK {
		t.Fatalf("tighten status=%d body=%s", tighten.StatusCode, readResponse(t, tighten))
	}
	draining := get()
	if !draining.Enforced.ActiveRelease[0].OverAllowanceDrain || len(draining.Enforced.ResidualCohorts) != 1 ||
		!draining.Enforced.ResidualCohorts[0].OverAllowanceDrain || draining.Enforced.ResidualCohorts[0].Running != 2 {
		t.Fatalf("draining readback = %#v", draining)
	}
	auditResponse := executionPolicyRequest(t, server.URL, http.MethodGet, "/api/w/team-a/apps/orders/execution-limit-policy-audit", "instance-admin", nil)
	var audits struct {
		Items []state.ExecutionLimitPolicyAudit `json:"items"`
	}
	decodeResponse(t, auditResponse, &audits)
	if len(audits.Items) != 2 || audits.Items[0].OperationID != "cloud-project-1" || audits.Items[1].OperationID != "cloud-project-tighten-1" {
		t.Fatalf("audits = %#v", audits)
	}
	deleteBody := map[string]any{
		"scope": implicit.Scope, "policy_id": implicit.PolicyID, "kind": implicit.Kind,
		"shape_fingerprint": implicit.ShapeFingerprint,
		"expected_revision": 2, "operation_id": "cloud-project-delete-1", "reason": "return to Release default",
	}
	deleted := executionPolicyRequest(t, server.URL, http.MethodDelete, "/api/w/team-a/apps/orders/execution-limit-policies", "instance-admin", deleteBody)
	if deleted.StatusCode != http.StatusOK {
		t.Fatalf("DELETE status=%d body=%s", deleted.StatusCode, readResponse(t, deleted))
	}
	var deletedPayload struct {
		Policy   canonicalExecutionLimitPolicy `json:"policy"`
		Replayed bool                          `json:"replayed"`
	}
	decodeResponse(t, deleted, &deletedPayload)
	if deletedPayload.Replayed || deletedPayload.Policy.Status != "deleted" || deletedPayload.Policy.OperatorAllowance != nil || deletedPayload.Policy.Revision != 3 {
		t.Fatalf("deleted mutation = %#v", deletedPayload)
	}
}

func TestExecutionLimitPolicyPreflightRejectsForwardShapeChangeAndAllowsCapacityChange(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	deployment := executionLimitPolicyTestDeployment("team-a", 60)
	deployment.Commit = "release-original"
	if _, err := store.PublishRelease(ctx, deployment, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	shapes, err := observedExecutionLimitShapes(deployment)
	if err != nil {
		t.Fatal(err)
	}
	rateShape := shapes[2]
	policy := state.ExecutionLimitPolicy{
		ExecutionLimitPolicyKey: state.ExecutionLimitPolicyKey{
			WorkspaceID: rateShape.WorkspaceID, AppKey: rateShape.AppKey, ActionKey: rateShape.ActionKey,
			Scope: rateShape.Scope, PolicyID: rateShape.PolicyID, Kind: rateShape.Kind,
		},
		ShapeFingerprint: rateShape.ShapeFingerprint, Allowance: 3, WindowSeconds: 60,
	}
	if _, _, err := store.MutateExecutionLimitPolicy(ctx, state.MutateExecutionLimitPolicyRequest{
		Policy: policy, ExpectedRevision: 0, OperationID: "op-create", RequestFingerprint: "request-create", Actor: "test",
	}); err != nil {
		t.Fatal(err)
	}
	handler := &Handler{store: store}
	capacityOnly := deployment
	capacityOnly.Commit = "release-capacity-only"
	action := capacityOnly.Actions["collect"]
	action.ExecutionLimits.Rate[0].MaxAttempts = 8
	capacityOnly.Actions["collect"] = action
	if err := handler.validateExecutionLimitPolicyCompatibility(ctx, capacityOnly); err != nil {
		t.Fatalf("capacity-only change rejected: %v", err)
	}
	if _, err := store.PublishRelease(ctx, capacityOnly, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	capacityReadback, err := handler.executionLimitPolicyReadback(ctx, capacityOnly)
	if err != nil || len(capacityReadback.Desired.Items) != 1 || capacityReadback.Desired.Items[0].Status != "applied" {
		t.Fatalf("capacity-only readback=%#v err=%v", capacityReadback, err)
	}
	incompatible := deployment
	incompatible.Commit = "release-incompatible"
	action = incompatible.Actions["collect"]
	action.ExecutionLimits.Rate[0].WindowSeconds = 120
	incompatible.Actions["collect"] = action
	err = handler.validateExecutionLimitPolicyCompatibility(ctx, incompatible)
	var preflight *executionLimitPreflightError
	if !errors.As(err, &preflight) || preflight.PolicyFingerprint != rateShape.ShapeFingerprint || preflight.ObservedFingerprint == rateShape.ShapeFingerprint {
		t.Fatalf("preflight err = %#v", err)
	}
	active, err := store.GetDeploymentForWorkspace(ctx, "team-a", deployment.App)
	if err != nil || active.Commit != capacityOnly.Commit {
		t.Fatalf("active Release after rejected preflight=%#v err=%v", active, err)
	}
	storedPolicy, err := store.GetExecutionLimitPolicy(ctx, policy.ExecutionLimitPolicyKey)
	if err != nil || storedPolicy.Revision != 1 || storedPolicy.ShapeFingerprint != rateShape.ShapeFingerprint {
		t.Fatalf("policy after rejected preflight=%#v err=%v", storedPolicy, err)
	}
}

func executionLimitPolicyTestDeployment(workspaceID string, windowSeconds int32) contract.Deployment {
	return contract.Deployment{
		Workspace: workspaceID, App: "orders", Commit: "commit-a", MaxConcurrent: int32TestPointer(4),
		ExecutionLimits: contract.ExecutionLimits{Concurrency: []contract.KeyedConcurrencyLimit{{ID: "account", MaxConcurrent: 3, InputPointers: []string{"/account"}}}},
		Actions: map[string]contract.Action{"collect": {
			Action: "collect", ExecutionLimits: contract.ExecutionLimits{Rate: []contract.KeyedRateLimit{{ID: "vendor", MaxAttempts: 10, WindowSeconds: windowSeconds, InputPointers: []string{"/account"}}}},
		}},
	}
}

func executionPolicyRequest(t *testing.T, baseURL string, method string, path string, token string, body any) *http.Response {
	t.Helper()
	var payload *bytes.Reader
	if body == nil {
		payload = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, baseURL+path, payload)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func cloneJSONMap(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func int32TestPointer(value int32) *int32 { return &value }

func TestExecutionLimitPolicyResponsesDoNotExposeRawInputPointers(t *testing.T) {
	deployment := executionLimitPolicyTestDeployment("team-a", 60)
	shapes, err := observedExecutionLimitShapes(deployment)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(shapes)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "/account") {
		t.Fatalf("shape response leaked input pointers: %s", encoded)
	}
}
