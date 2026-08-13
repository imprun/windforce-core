package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	executionpkg "github.com/imprun/windforce-core/internal/execution"
	"github.com/imprun/windforce-core/internal/state"
	"github.com/imprun/windforce-core/internal/telemetry"
)

func TestInvocationAPIPrincipalAuthorizationAndIdempotency(t *testing.T) {
	ctx := context.Background()
	telemetrySDK := telemetry.InitSDK(ctx, "server", "test")
	t.Cleanup(func() { _ = telemetrySDK.Shutdown(context.Background()) })
	statePath := filepath.Join(t.TempDir(), "state.json")
	store := state.NewLocalStore(statePath)
	if _, err := store.CreateWorkspace(ctx, "ws-a", "Workspace A", "admin"); err != nil {
		t.Fatal(err)
	}
	deployment := contract.Deployment{
		Workspace:    "ws-a",
		GitSourceID:  "source-a",
		App:          "echo",
		Commit:       "commit-a",
		BundleDigest: testExecutionBundleDigest,
		Actions: map[string]contract.Action{
			"run": {
				Action:           "run",
				Entrypoint:       "main.py",
				InputSchemaBody:  json.RawMessage(`{"type":"object","required":["message"]}`),
				OutputSchemaBody: json.RawMessage(`{"type":"object","required":["echo"]}`),
			},
			"hidden": {
				Action:           "hidden",
				Entrypoint:       "hidden.py",
				InputSchemaBody:  json.RawMessage(`{"type":"object","description":"hidden-action-marker"}`),
				OutputSchemaBody: json.RawMessage(`{"type":"object"}`),
			},
		},
	}
	if _, err := store.PublishRelease(ctx, deployment, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	firstToken := contract.ServiceTokenPrefix + "first"
	first, err := store.CreateServicePrincipal(ctx, state.ServicePrincipal{
		WorkspaceID: "ws-a",
		Name:        "first",
		Scopes: []string{
			string(executionpkg.ScopeRunsCreate),
			string(executionpkg.ScopeRunsReadOwn),
			string(executionpkg.ScopeRunsCancelOwn),
			string(executionpkg.ScopeAppsRead),
		},
		AllowedTargets: []string{"echo/run"},
	}, state.HashClientToken(firstToken), "admin")
	if err != nil {
		t.Fatal(err)
	}
	secondToken := contract.ServiceTokenPrefix + "second"
	if _, err := store.CreateServicePrincipal(ctx, state.ServicePrincipal{
		WorkspaceID: "ws-a",
		Name:        "second",
		Scopes:      []string{string(executionpkg.ScopeRunsCreate), string(executionpkg.ScopeRunsReadOwn)},
	}, state.HashClientToken(secondToken), "admin"); err != nil {
		t.Fatal(err)
	}
	clientToken := contract.ClientTokenPrefix + "client"
	client, err := store.CreateClient(ctx, "ws-a", "client", state.HashClientToken(clientToken), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetInputConfig(ctx, state.InputConfig{
		WorkspaceID: "ws-a", AppKey: "echo", ActionKey: "run", ClientID: client.ID,
		Config: json.RawMessage(`{"message":"client-only"}`), LockedKeys: []string{"message"},
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	devRequest := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-a/runs", nil)
	devRequest.Header.Set("Authorization", "Bearer "+clientToken)
	devPrincipal, status, message := (&Handler{store: store}).authenticateInvocation(devRequest, "ws-a")
	if status != 0 || message != "" || devPrincipal.Kind != executionpkg.PrincipalClient || devPrincipal.ID != client.ID {
		t.Fatalf("dev client principal = %#v, status=%d message=%q", devPrincipal, status, message)
	}
	server := httptest.NewServer(telemetry.HTTPHandler(New(Config{Store: store, Catalog: store, AdminToken: "admin-secret"}), "test.invocation"))
	defer server.Close()
	currentTraceParent := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	call := func(method string, path string, token string, idempotencyKey string, body string) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if currentTraceParent != "" {
			req.Header.Set("traceparent", currentTraceParent)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return resp, payload
	}

	createBody := `{"app":"echo","action":"run","input":{"message":"hello"},"correlation_id":"request-a"}`
	response, payload := call(http.MethodPost, "/api/v1/workspaces/ws-a/runs", firstToken, "request-1", createBody)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d: %s", response.StatusCode, payload)
	}
	var created invocationRunView
	if err := json.Unmarshal(payload, &created); err != nil {
		t.Fatal(err)
	}
	if created.RunID == "" || response.Header.Get(invocationRunIDHeader) != created.RunID ||
		response.Header.Get("Location") != "/api/v1/workspaces/ws-a/runs/"+created.RunID {
		t.Fatalf("create response = %#v headers=%v", created, response.Header)
	}
	if bytes.Contains(payload, []byte("job")) || bytes.Contains(payload, []byte("principal")) ||
		bytes.Contains(payload, []byte("client_id")) {
		t.Fatalf("Invocation response exposes an internal model: %s", payload)
	}
	run, err := store.GetRun(ctx, created.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.PrincipalKind != string(executionpkg.PrincipalService) || run.PrincipalID != first.ID ||
		run.IdempotencyHash == "" || run.RequestFingerprint == "" {
		t.Fatalf("persisted run principal/idempotency = %#v", run)
	}
	if telemetry.TraceID(run.TraceContext) != "4bf92f3577b34da6a3ce929d0e0e4736" ||
		run.TraceContext.PropagationReason != telemetry.ReasonContinued {
		t.Fatalf("persisted creation trace = %#v", run.TraceContext)
	}
	createdJob, _, found, err := store.GetJobByRunID(ctx, "ws-a", created.RunID)
	if err != nil || !found || createdJob.TraceContext != run.TraceContext {
		t.Fatalf("job creation trace = %#v, found=%v err=%v", createdJob.TraceContext, found, err)
	}
	var serviceInput map[string]any
	if err := json.Unmarshal(createdJob.Payload.Input, &serviceInput); err != nil || serviceInput["message"] != "hello" {
		t.Fatalf("service principal unexpectedly resolved client input settings: %s err=%v", createdJob.Payload.Input, err)
	}

	currentTraceParent = "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01"
	replay, replayBody := call(http.MethodPost, "/api/v1/workspaces/ws-a/runs", firstToken, "request-1", createBody)
	if replay.StatusCode != http.StatusOK {
		t.Fatalf("replay status = %d: %s", replay.StatusCode, replayBody)
	}
	var replayed invocationRunView
	if err := json.Unmarshal(replayBody, &replayed); err != nil {
		t.Fatal(err)
	}
	if replayed.RunID != created.RunID || !replayed.Replayed {
		t.Fatalf("replayed = %#v, created = %#v", replayed, created)
	}
	replayedRun, err := store.GetRun(ctx, created.RunID)
	if err != nil || replayedRun.TraceContext != run.TraceContext {
		t.Fatalf("idempotency replay overwrote creation trace: %#v err=%v", replayedRun.TraceContext, err)
	}
	reloadedRun, err := state.NewLocalStore(statePath).GetRun(ctx, created.RunID)
	if err != nil || reloadedRun.TraceContext != run.TraceContext {
		t.Fatalf("local JSON trace round-trip = %#v err=%v", reloadedRun.TraceContext, err)
	}
	operatorCreate, operatorCreateBody := call(
		http.MethodPost, "/api/v1/workspaces/ws-a/runs", "admin-secret", "request-1", createBody,
	)
	if operatorCreate.StatusCode != http.StatusCreated {
		t.Fatalf("operator create status = %d: %s", operatorCreate.StatusCode, operatorCreateBody)
	}
	var operatorCreated invocationRunView
	if err := json.Unmarshal(operatorCreateBody, &operatorCreated); err != nil {
		t.Fatal(err)
	}
	if operatorCreated.RunID == created.RunID {
		t.Fatal("idempotency key was not scoped to the authenticated principal")
	}
	clientRequestBody := `{"app":"echo","action":"run","input":{},"correlation_id":"request-a"}`
	clientCreate, clientCreateBody := call(
		http.MethodPost, "/api/v1/workspaces/ws-a/runs", clientToken, "request-1", clientRequestBody,
	)
	if clientCreate.StatusCode != http.StatusCreated {
		t.Fatalf("client create status = %d: %s", clientCreate.StatusCode, clientCreateBody)
	}
	var clientCreated invocationRunView
	if err := json.Unmarshal(clientCreateBody, &clientCreated); err != nil {
		t.Fatal(err)
	}
	clientRun, err := store.GetRun(ctx, clientCreated.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if clientRun.PrincipalKind != string(executionpkg.PrincipalClient) ||
		clientRun.PrincipalID != client.ID || clientRun.ClientID != client.ID {
		t.Fatalf("persisted client principal = %#v", clientRun)
	}
	clientJob, _, found, err := store.GetJobByRunID(ctx, "ws-a", clientCreated.RunID)
	var clientInput map[string]any
	inputErr := json.Unmarshal(clientJob.Payload.Input, &clientInput)
	if err != nil || !found || inputErr != nil || clientInput["message"] != "client-only" {
		t.Fatalf("client input settings were not resolved: found=%v err=%v inputErr=%v input=%s", found, err, inputErr, clientJob.Payload.Input)
	}
	jobsBeforePolicy, err := store.ListJobs(ctx, state.JobListQuery{WorkspaceID: "ws-a", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if _, replayed, err := store.UpdateClientInvocationPolicy(ctx, state.UpdateClientInvocationPolicyRequest{
		WorkspaceID: "ws-a", ClientID: client.ID,
		Policy:      state.TargetPolicy{Mode: state.TargetPolicyModeRestricted, AllowedTargets: []string{"echo/run"}},
		OperationID: "restrict-client", ExpectedRevision: 0, RequestFingerprint: "restrict-client-v1", Actor: "admin",
	}); err != nil || replayed {
		t.Fatalf("restrict client policy: replayed=%v err=%v", replayed, err)
	}
	clientReplay, clientReplayBody := call(
		http.MethodPost, "/api/v1/workspaces/ws-a/runs", clientToken, "request-1", clientRequestBody,
	)
	if clientReplay.StatusCode != http.StatusOK {
		t.Fatalf("client replay after policy reduction status = %d: %s", clientReplay.StatusCode, clientReplayBody)
	}
	var replayedClientRun invocationRunView
	if err := json.Unmarshal(clientReplayBody, &replayedClientRun); err != nil {
		t.Fatal(err)
	}
	if replayedClientRun.RunID != clientCreated.RunID || !replayedClientRun.Replayed {
		t.Fatalf("client replay after policy reduction = %#v", replayedClientRun)
	}
	clientReplayMismatch, clientReplayMismatchBody := call(
		http.MethodPost, "/api/v1/workspaces/ws-a/runs", clientToken, "request-1",
		`{"app":"echo","action":"hidden","input":{}}`,
	)
	if clientReplayMismatch.StatusCode != http.StatusConflict {
		t.Fatalf("client mismatched replay status = %d: %s", clientReplayMismatch.StatusCode, clientReplayMismatchBody)
	}
	clientForbidden, clientForbiddenBody := call(
		http.MethodPost, "/api/v1/workspaces/ws-a/runs", clientToken, "request-2",
		`{"app":"echo","action":"hidden","input":{}}`,
	)
	if clientForbidden.StatusCode != http.StatusForbidden {
		t.Fatalf("client new forbidden target status = %d: %s", clientForbidden.StatusCode, clientForbiddenBody)
	}
	jobsAfterPolicy, err := store.ListJobs(ctx, state.JobListQuery{WorkspaceID: "ws-a", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobsAfterPolicy) != len(jobsBeforePolicy) {
		t.Fatalf("policy replay/denial created work: before=%d after=%d", len(jobsBeforePolicy), len(jobsAfterPolicy))
	}
	clientApp, clientAppBody := call(http.MethodGet, "/api/v1/workspaces/ws-a/apps/echo", clientToken, "", "")
	if clientApp.StatusCode != http.StatusOK || !bytes.Contains(clientAppBody, []byte(`"run"`)) ||
		bytes.Contains(clientAppBody, []byte(`"hidden"`)) || bytes.Contains(clientAppBody, []byte("hidden-action-marker")) {
		t.Fatalf("client action-filtered discovery status = %d: %s", clientApp.StatusCode, clientAppBody)
	}
	clientRead, clientReadBody := call(
		http.MethodGet, "/api/v1/workspaces/ws-a/runs/"+clientCreated.RunID, clientToken, "", "",
	)
	if clientRead.StatusCode != http.StatusOK {
		t.Fatalf("client own read status = %d: %s", clientRead.StatusCode, clientReadBody)
	}

	mismatch, mismatchBody := call(
		http.MethodPost, "/api/v1/workspaces/ws-a/runs", firstToken, "request-1",
		`{"app":"echo","action":"run","input":{"message":"different"}}`,
	)
	if mismatch.StatusCode != http.StatusConflict {
		t.Fatalf("mismatch status = %d: %s", mismatch.StatusCode, mismatchBody)
	}
	invalid, invalidBody := call(
		http.MethodPost, "/api/v1/workspaces/ws-a/runs", firstToken, "",
		`{"app":"echo","action":"run","input":{},"env":["SECRET=value"]}`,
	)
	if invalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d: %s", invalid.StatusCode, invalidBody)
	}
	disallowed, disallowedBody := call(
		http.MethodPost, "/api/v1/workspaces/ws-a/runs", firstToken, "",
		`{"app":"echo","action":"other","input":{}}`,
	)
	if disallowed.StatusCode != http.StatusForbidden {
		t.Fatalf("disallowed target status = %d: %s", disallowed.StatusCode, disallowedBody)
	}
	missingInput, missingInputBody := call(
		http.MethodPost, "/api/v1/workspaces/ws-a/runs", firstToken, "",
		`{"app":"echo","action":"run"}`,
	)
	if missingInput.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing input status = %d: %s", missingInput.StatusCode, missingInputBody)
	}
	longKey, longKeyBody := call(
		http.MethodPost, "/api/v1/workspaces/ws-a/runs", firstToken, strings.Repeat("a", maxInvocationIdempotencyKeyBytes+1),
		createBody,
	)
	if longKey.StatusCode != http.StatusBadRequest {
		t.Fatalf("long idempotency key status = %d: %s", longKey.StatusCode, longKeyBody)
	}

	own, ownBody := call(http.MethodGet, "/api/v1/workspaces/ws-a/runs/"+created.RunID, firstToken, "", "")
	if own.StatusCode != http.StatusOK {
		t.Fatalf("own read status = %d: %s", own.StatusCode, ownBody)
	}
	foreign, foreignBody := call(http.MethodGet, "/api/v1/workspaces/ws-a/runs/"+created.RunID, secondToken, "", "")
	if foreign.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign read status = %d: %s", foreign.StatusCode, foreignBody)
	}
	operator, operatorBody := call(http.MethodGet, "/api/v1/workspaces/ws-a/runs/"+created.RunID, "admin-secret", "", "")
	if operator.StatusCode != http.StatusOK {
		t.Fatalf("operator read status = %d: %s", operator.StatusCode, operatorBody)
	}
	missing, missingBody := call(http.MethodGet, "/api/v1/workspaces/ws-a/runs/"+created.RunID, "", "", "")
	if missing.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing auth status = %d: %s", missing.StatusCode, missingBody)
	}

	appResponse, appBody := call(http.MethodGet, "/api/v1/workspaces/ws-a/apps/echo", firstToken, "", "")
	if appResponse.StatusCode != http.StatusOK || !bytes.Contains(appBody, []byte(`"run"`)) {
		t.Fatalf("describe app status = %d: %s", appResponse.StatusCode, appBody)
	}
	appForbidden, appForbiddenBody := call(http.MethodGet, "/api/v1/workspaces/ws-a/apps/echo", secondToken, "", "")
	if appForbidden.StatusCode != http.StatusForbidden {
		t.Fatalf("describe app without scope status = %d: %s", appForbidden.StatusCode, appForbiddenBody)
	}
	cancel, cancelBody := call(
		http.MethodPost, "/api/v1/workspaces/ws-a/runs/"+created.RunID+"/cancel", firstToken, "", `{"reason":"test"}`,
	)
	if cancel.StatusCode != http.StatusOK || !bytes.Contains(cancelBody, []byte(`"state":"canceled"`)) {
		t.Fatalf("cancel status = %d: %s", cancel.StatusCode, cancelBody)
	}
}

func TestInvocationOpenAPIContainsOnlyRunBoundary(t *testing.T) {
	document := buildInvocationOpenAPI("http://example.test")
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`job_id`, `"env"`, `client_id`, `permissioned_as`, `created_by`, `/execution/v1`} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Errorf("Invocation OpenAPI contains forbidden internal or legacy field %q", forbidden)
		}
	}
	paths, ok := document["paths"].(map[string]any)
	if !ok {
		t.Fatal("paths is not an object")
	}
	for _, path := range []string{
		"/api/v1/workspaces/{workspace}/runs",
		"/api/v1/workspaces/{workspace}/runs/wait",
		"/api/v1/workspaces/{workspace}/runs/{run_id}",
		"/api/v1/workspaces/{workspace}/runs/{run_id}/result",
		"/api/v1/workspaces/{workspace}/runs/{run_id}/cancel",
		"/api/v1/workspaces/{workspace}/apps/{app}",
	} {
		if paths[path] == nil {
			t.Errorf("Invocation OpenAPI path %q is missing", path)
		}
	}
	if !strings.Contains(string(encoded), contract.ServiceTokenPrefix) {
		t.Fatal("Invocation OpenAPI does not document service principal authentication")
	}
	for _, name := range []string{
		"invocation/v1/examples/create-run.request.json",
		"invocation/v1/examples/create-run.response.json",
	} {
		example, err := invocationExamples.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(example) {
			t.Errorf("Invocation example %q is not valid JSON", name)
		}
		for _, forbidden := range []string{`job_id`, `"env"`, `client_id`, `permissioned_as`} {
			if bytes.Contains(example, []byte(forbidden)) {
				t.Errorf("Invocation example %q contains forbidden field %q", name, forbidden)
			}
		}
	}
}
