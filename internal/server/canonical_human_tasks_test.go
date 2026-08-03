package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/state"
	"github.com/imprun/windforce-core/internal/token"
)

func TestHumanTaskHoldWaitDecisionResumesOriginalJob(t *testing.T) {
	ctx := context.Background()
	statePath := t.TempDir() + "/state.json"
	store := state.NewLocalStore(statePath)
	store.ConfigureInputCrypto("human-task-encryption-secret", "")
	deployment := contract.Deployment{
		Workspace: "default",
		App:       "hold-app",
		Commit:    "hold-commit",
		Actions: map[string]contract.Action{
			"wait": {Action: "wait", Command: []string{"helper"}},
		},
	}
	run := state.NewRun("api", state.NewID("run"), deployment.App, "wait", deployment, json.RawMessage(`{}`))
	job := state.NewActionJob(run, json.RawMessage(`{}`))
	if err := store.CreateRunAndEnqueue(ctx, run, job); err != nil {
		t.Fatalf("CreateRunAndEnqueue returned error: %v", err)
	}
	claimed, _, err := store.ClaimJob(ctx, "worker-hold", time.Minute)
	if err != nil {
		t.Fatalf("ClaimJob returned error: %v", err)
	}
	const jobSecret = "human-task-job-secret"
	server := httptest.NewServer(New(Config{
		Store:          store,
		AdminToken:     "admin-token",
		JobTokenSecret: jobSecret,
		SecretKey:      "human-task-encryption-secret",
	}))
	defer server.Close()
	jobToken := token.MintJob(jobSecret, token.JobClaims{
		Workspace: "default",
		JobID:     claimed.ID,
		Subject:   "app:hold-app",
		Attempt:   claimed.Attempt,
		Exp:       time.Now().Add(time.Minute).Unix(),
	})

	type response struct {
		status int
		body   []byte
		err    error
	}
	waitResponse := make(chan response, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/w/default/human-tasks/wait", bytes.NewBufferString(`{
  "key":"login-otp",
  "kind":"form",
  "title":"Enter code",
  "input_schema":{"type":"object","required":["otp"],"properties":{"otp":{"type":"string"}}},
  "private_context":{"callback":"callback-secret"},
  "timeout_ms":30000
}`))
		req.Header.Set("Authorization", "Bearer "+jobToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			waitResponse <- response{err: err}
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		waitResponse <- response{status: resp.StatusCode, body: body, err: err}
	}()

	var task state.HumanTask
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tasks, err := store.ListHumanTasks(ctx, state.HumanTaskListQuery{WorkspaceID: "default", State: state.HumanTaskPending})
		if err != nil {
			t.Fatalf("ListHumanTasks returned error: %v", err)
		}
		if len(tasks) == 1 {
			task = tasks[0]
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if task.ID == "" {
		t.Fatal("HumanTask was not created while the Action request remained open")
	}

	decisionBody := `{"outcome":"submit","value":{"otp":"otp-secret"}}`
	decision := humanTaskTestRequest(t, http.MethodPost, server.URL+"/api/w/default/human-tasks/"+task.ID+"/decision", "admin-token", "decision-1", decisionBody)
	if decision.StatusCode != http.StatusOK {
		t.Fatalf("decision status = %d: %s", decision.StatusCode, readResponse(t, decision))
	}
	decision.Body.Close()

	select {
	case result := <-waitResponse:
		if result.err != nil || result.status != http.StatusOK || !bytes.Contains(result.body, []byte(`"otp":"otp-secret"`)) {
			t.Fatalf("wait response = status:%d body:%s err:%v", result.status, result.body, result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("HumanTask decision did not resume the held request")
	}

	jobs, err := store.ListJobs(ctx, state.JobListQuery{WorkspaceID: "default", Limit: 10})
	if err != nil || len(jobs) != 1 || jobs[0].ID != claimed.ID || !jobs[0].Running {
		t.Fatalf("decision created another Job or released the original lease: jobs=%#v err=%v", jobs, err)
	}
	replay := humanTaskTestRequest(t, http.MethodPost, server.URL+"/api/w/default/human-tasks/"+task.ID+"/decision", "admin-token", "decision-1", decisionBody)
	if replay.StatusCode != http.StatusOK {
		t.Fatalf("idempotent replay status = %d: %s", replay.StatusCode, readResponse(t, replay))
	}
	replayBody := readResponse(t, replay)
	if !bytes.Contains([]byte(replayBody), []byte(`"replayed":true`)) {
		t.Fatalf("idempotent replay body = %s", replayBody)
	}
	conflict := humanTaskTestRequest(t, http.MethodPost, server.URL+"/api/w/default/human-tasks/"+task.ID+"/decision", "admin-token", "decision-2", `{"outcome":"cancel"}`)
	if conflict.StatusCode != http.StatusConflict {
		t.Fatalf("conflicting decision status = %d: %s", conflict.StatusCode, readResponse(t, conflict))
	}
	conflict.Body.Close()
	crossWorkspace := humanTaskTestRequest(t, http.MethodGet, server.URL+"/api/w/other/human-tasks/"+task.ID, "admin-token", "", "")
	if crossWorkspace.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-workspace task status = %d: %s", crossWorkspace.StatusCode, readResponse(t, crossWorkspace))
	}
	crossWorkspace.Body.Close()
	stored, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read local state: %v", err)
	}
	for _, secret := range [][]byte{[]byte("callback-secret"), []byte("otp-secret")} {
		if bytes.Contains(stored, secret) {
			t.Fatalf("local state contains HumanTask plaintext %q", secret)
		}
	}
}

func TestHumanTaskControlPlaneHonorsServicePrincipalScopesAndTargets(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(t.TempDir() + "/state.json")
	store.ConfigureInputCrypto("human-task-service-secret", "")
	if _, err := store.CreateWorkspace(ctx, "ws-a", "Workspace A", "test"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	deployment := contract.Deployment{
		Workspace: "ws-a",
		App:       "allowed-app",
		Commit:    "hold-commit",
		Actions: map[string]contract.Action{
			"wait": {Action: "wait", Command: []string{"helper"}},
		},
	}
	run := state.NewRun("api", state.NewID("run"), deployment.App, "wait", deployment, json.RawMessage(`{}`))
	job := state.NewActionJob(run, json.RawMessage(`{}`))
	if err := store.CreateRunAndEnqueue(ctx, run, job); err != nil {
		t.Fatalf("CreateRunAndEnqueue returned error: %v", err)
	}
	claimed, _, err := store.ClaimJob(ctx, "worker-hold", time.Minute)
	if err != nil {
		t.Fatalf("ClaimJob returned error: %v", err)
	}
	expiresAt := time.Now().Add(time.Minute)
	task, _, err := store.CreateHeldHumanTask(ctx, state.HumanTask{
		WorkspaceID: "ws-a", RunID: run.ID, JobID: claimed.ID, Attempt: claimed.Attempt,
		Key: "task-a", RequestFingerprint: "request-a", Mode: state.HumanTaskModeHold,
		Kind: "form", Title: "Choose", Schema: json.RawMessage(`{"type":"object"}`), ExpiresAt: &expiresAt,
	})
	if err != nil {
		t.Fatalf("CreateHeldHumanTask returned error: %v", err)
	}
	const serviceToken = "wfs_human-task-service"
	service, err := store.CreateServicePrincipal(ctx, state.ServicePrincipal{
		WorkspaceID: "ws-a",
		Name:        "Interaction adapter",
		Scopes:      []string{"human_tasks:read", "human_tasks:decide"},
		AllowedTargets: []string{
			"allowed-app/wait",
		},
	}, state.HashBearerToken(serviceToken), "test")
	if err != nil {
		t.Fatalf("CreateServicePrincipal returned error: %v", err)
	}
	const readOnlyToken = "wfs_human-task-read-only"
	if _, err := store.CreateServicePrincipal(ctx, state.ServicePrincipal{
		WorkspaceID: "ws-a", Name: "Observer", Scopes: []string{"human_tasks:read"}, AllowedTargets: []string{"allowed-app"},
	}, state.HashBearerToken(readOnlyToken), "test"); err != nil {
		t.Fatalf("CreateServicePrincipal read-only returned error: %v", err)
	}
	const wrongTargetToken = "wfs_human-task-wrong-target"
	if _, err := store.CreateServicePrincipal(ctx, state.ServicePrincipal{
		WorkspaceID: "ws-a", Name: "Wrong target", Scopes: []string{"human_tasks:read"}, AllowedTargets: []string{"other-app"},
	}, state.HashBearerToken(wrongTargetToken), "test"); err != nil {
		t.Fatalf("CreateServicePrincipal wrong-target returned error: %v", err)
	}
	server := httptest.NewServer(New(Config{Store: store, SecretKey: "human-task-service-secret", AdminToken: "admin-token"}))
	defer server.Close()

	list := humanTaskTestRequest(t, http.MethodGet, server.URL+"/api/w/ws-a/human-tasks", serviceToken, "", "")
	if list.StatusCode != http.StatusOK {
		t.Fatalf("service principal list status = %d: %s", list.StatusCode, readResponse(t, list))
	}
	list.Body.Close()
	wrongTargetList := humanTaskTestRequest(t, http.MethodGet, server.URL+"/api/w/ws-a/human-tasks", wrongTargetToken, "", "")
	if wrongTargetList.StatusCode != http.StatusOK {
		t.Fatalf("wrong-target list status = %d: %s", wrongTargetList.StatusCode, readResponse(t, wrongTargetList))
	}
	var wrongTargetItems struct {
		Items []humanTaskView `json:"items"`
	}
	if err := json.NewDecoder(wrongTargetList.Body).Decode(&wrongTargetItems); err != nil {
		t.Fatalf("decode wrong-target list: %v", err)
	}
	wrongTargetList.Body.Close()
	if len(wrongTargetItems.Items) != 0 {
		t.Fatalf("wrong-target list exposed tasks: %#v", wrongTargetItems.Items)
	}
	readOnlyDecision := humanTaskTestRequest(t, http.MethodPost, server.URL+"/api/w/ws-a/human-tasks/"+task.ID+"/decision", readOnlyToken, "decision-read-only", `{"outcome":"cancel"}`)
	if readOnlyDecision.StatusCode != http.StatusForbidden {
		t.Fatalf("read-only decision status = %d: %s", readOnlyDecision.StatusCode, readResponse(t, readOnlyDecision))
	}
	readOnlyDecision.Body.Close()
	wrongTarget := humanTaskTestRequest(t, http.MethodGet, server.URL+"/api/w/ws-a/human-tasks/"+task.ID, wrongTargetToken, "", "")
	if wrongTarget.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong-target read status = %d: %s", wrongTarget.StatusCode, readResponse(t, wrongTarget))
	}
	wrongTarget.Body.Close()
	decision := humanTaskTestRequest(t, http.MethodPost, server.URL+"/api/w/ws-a/human-tasks/"+task.ID+"/decision", serviceToken, "decision-service", `{"outcome":"cancel"}`)
	if decision.StatusCode != http.StatusOK {
		t.Fatalf("service decision status = %d: %s", decision.StatusCode, readResponse(t, decision))
	}
	decision.Body.Close()
	decided, err := store.GetHumanTask(ctx, task.ID)
	if err != nil || decided.DecidedBy != "service:"+service.ID {
		t.Fatalf("service decision actor = %#v, err=%v", decided, err)
	}
}

func TestHumanTaskAPIAppearsInControlPlaneOpenAPI(t *testing.T) {
	document := buildControlPlaneOpenAPI("http://core.example", "ws-a")
	paths := document["paths"].(map[string]any)
	for _, path := range []string{
		"/api/w/{workspace}/human-tasks",
		"/api/w/{workspace}/human-tasks/{taskId}",
		"/api/w/{workspace}/human-tasks/{taskId}/decision",
		"/api/w/{workspace}/human-tasks/wait",
	} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("HumanTask path %q missing from control-plane OpenAPI", path)
		}
	}
	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	for _, name := range []string{"HumanTask", "HumanTaskList", "HumanTaskDecisionRequest", "HumanTaskWaitRequest"} {
		if _, ok := schemas[name]; !ok {
			t.Fatalf("HumanTask schema %q missing from control-plane OpenAPI", name)
		}
	}
	service := schemas["ServicePrincipal"].(map[string]any)
	scopeItems := service["properties"].(map[string]any)["scopes"].(map[string]any)["items"].(map[string]any)["enum"].([]any)
	foundRead, foundDecide := false, false
	for _, scope := range scopeItems {
		foundRead = foundRead || scope == "human_tasks:read"
		foundDecide = foundDecide || scope == "human_tasks:decide"
	}
	if !foundRead || !foundDecide {
		t.Fatalf("HumanTask service principal scopes missing: %#v", scopeItems)
	}
}

func humanTaskTestRequest(t *testing.T, method string, url string, bearer string, idempotencyKey string, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
