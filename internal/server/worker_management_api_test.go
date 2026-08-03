package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/executionbundle"
	"github.com/imprun/windforce-core/internal/state"
)

type issuedWorkerCredential struct {
	Credential  workerCredentialResponse `json:"credential"`
	WorkerToken string                   `json:"worker_token"`
	Replayed    bool                     `json:"replayed"`
}

func TestWorkerManagementAppearsInControlPlaneOpenAPI(t *testing.T) {
	document := buildControlPlaneOpenAPI("http://core.example", "ws-a")
	paths := document["paths"].(map[string]any)
	for _, path := range []string{
		"/api/worker-groups/{group}/credentials",
		"/api/worker-groups/{group}/credentials/{credential_id}/revoke",
		"/api/worker-groups/{group}/run-state",
	} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("worker management path %q missing from control-plane OpenAPI", path)
		}
	}
	components := document["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	for _, name := range []string{
		"WorkerCredential", "CreateWorkerCredentialRequest", "WorkerCredentialIssueResponse",
		"RevokeWorkerCredentialRequest", "WorkerGroupRunState", "PutWorkerGroupRunStateRequest",
	} {
		if _, ok := schemas[name]; !ok {
			t.Fatalf("worker management schema %q missing from control-plane OpenAPI", name)
		}
	}
}

func TestManagedWorkerCredentialAndDrainContract(t *testing.T) {
	server, store := newWorkerPlaneServer(t)
	createBody := `{"operation_id":"op-create-a1","expected_generation":0,"workspace_ids":["ws-a"],"labels":["linux","arm64"]}`
	status, body := workerManagementRequest(t, http.MethodPost, server.URL+"/api/worker-groups/group-a/credentials", "admin-secret", createBody)
	if status != http.StatusCreated {
		t.Fatalf("create credential = %d: %s", status, body)
	}
	var first issuedWorkerCredential
	if err := json.Unmarshal(body, &first); err != nil {
		t.Fatal(err)
	}
	if first.Credential.Generation != 1 || !strings.HasPrefix(first.WorkerToken, contract.RemoteWorkerTokenPrefix) || first.Replayed {
		t.Fatalf("issued credential = %#v", first)
	}
	if bytes.Contains(body, []byte("token_hash")) || bytes.Contains(body, []byte("request_fingerprint")) {
		t.Fatalf("credential response exposed persistence fields: %s", body)
	}

	status, replayBody := workerManagementRequest(t, http.MethodPost, server.URL+"/api/worker-groups/group-a/credentials", "admin-secret", createBody)
	if status != http.StatusOK || bytes.Contains(replayBody, []byte("worker_token")) {
		t.Fatalf("create replay = %d: %s", status, replayBody)
	}
	var replay issuedWorkerCredential
	if err := json.Unmarshal(replayBody, &replay); err != nil || !replay.Replayed || replay.Credential.ID != first.Credential.ID {
		t.Fatalf("replayed credential = %#v, err=%v", replay, err)
	}
	status, _ = workerManagementRequest(t, http.MethodPost, server.URL+"/api/worker-groups/group-a/credentials", "admin-secret",
		`{"operation_id":"op-create-a1","expected_generation":0,"workspace_ids":["ws-a"],"labels":["gpu"]}`)
	if status != http.StatusConflict {
		t.Fatalf("conflicting credential replay = %d, want 409", status)
	}

	status, body = workerManagementRequest(t, http.MethodPost, server.URL+"/api/worker-groups/group-a/credentials", "admin-secret",
		`{"operation_id":"op-create-a2","expected_generation":1,"workspace_ids":["ws-a"],"labels":["linux","arm64"]}`)
	if status != http.StatusCreated {
		t.Fatalf("rotate credential = %d: %s", status, body)
	}
	var second issuedWorkerCredential
	if err := json.Unmarshal(body, &second); err != nil || second.Credential.Generation != 2 {
		t.Fatalf("rotated credential = %#v, err=%v", second, err)
	}

	status, _ = workerManagementRequest(t, http.MethodPost, server.URL+"/worker/v1/workers", first.WorkerToken,
		`{"id":"worker-a","group":"group-b","labels":["linux","arm64"]}`)
	if status != http.StatusForbidden {
		t.Fatalf("cross-group registration = %d, want 403", status)
	}
	status, _ = workerManagementRequest(t, http.MethodPost, server.URL+"/worker/v1/workers", first.WorkerToken,
		`{"id":"worker-a","group":"group-a","labels":["linux"]}`)
	if status != http.StatusForbidden {
		t.Fatalf("narrow-label registration = %d, want 403 exact match", status)
	}
	status, body = workerManagementRequest(t, http.MethodPost, server.URL+"/worker/v1/workers", first.WorkerToken,
		`{"id":"worker-a","group":"group-a","labels":["arm64","linux"],"slots":1}`)
	if status != http.StatusCreated {
		t.Fatalf("managed registration = %d: %s", status, body)
	}
	status, body = workerManagementRequest(t, http.MethodPost, server.URL+"/api/worker-groups/group-b/credentials", "admin-secret",
		`{"operation_id":"op-create-b1","expected_generation":0,"workspace_ids":["ws-b"],"labels":["gpu"]}`)
	if status != http.StatusCreated {
		t.Fatalf("create second group credential = %d: %s", status, body)
	}
	var otherGroup issuedWorkerCredential
	if err := json.Unmarshal(body, &otherGroup); err != nil {
		t.Fatal(err)
	}
	status, _ = workerManagementRequest(t, http.MethodPost, server.URL+"/worker/v1/workers", otherGroup.WorkerToken,
		`{"id":"worker-a","group":"group-b","labels":["gpu"]}`)
	if status != http.StatusForbidden {
		t.Fatalf("cross-credential worker id takeover = %d, want 403", status)
	}
	status, _ = workerManagementRequest(t, http.MethodPost, server.URL+"/worker/v1/workers", otherGroup.WorkerToken,
		`{"id":"worker-b","group":"group-a","labels":["gpu"]}`)
	if status != http.StatusForbidden {
		t.Fatalf("second credential cross-group registration = %d, want 403", status)
	}

	enqueueManagedWorkerJob(t, store, "ws-b", "denied")
	firstJob := enqueueManagedWorkerJob(t, store, "ws-a", "leased")
	status, body = workerManagementRequest(t, http.MethodPost, server.URL+"/worker/v1/claims", first.WorkerToken,
		`{"worker_id":"worker-a","labels":["linux","arm64"]}`)
	if status != http.StatusOK {
		t.Fatalf("scoped claim = %d: %s", status, body)
	}
	var claim struct {
		Job   state.Job       `json:"job"`
		Lease workerLeaseWire `json:"lease"`
	}
	if err := json.Unmarshal(body, &claim); err != nil {
		t.Fatal(err)
	}
	if claim.Job.ID != firstJob.ID || contract.NormalizeWorkspace(claim.Job.Payload.Workspace) != "ws-a" {
		t.Fatalf("managed claim crossed workspace scope: %#v", claim.Job)
	}

	deadline := time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339Nano)
	drainBody := fmt.Sprintf(`{"operation_id":"op-drain-a","expected_revision":0,"state":"draining","deadline_at":%q}`, deadline)
	status, body = workerManagementRequest(t, http.MethodPut, server.URL+"/api/worker-groups/group-a/run-state", "admin-secret", drainBody)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"revision":1`)) {
		t.Fatalf("drain = %d: %s", status, body)
	}
	status, replayBody = workerManagementRequest(t, http.MethodPut, server.URL+"/api/worker-groups/group-a/run-state", "admin-secret", drainBody)
	if status != http.StatusOK || !bytes.Contains(replayBody, []byte(`"replayed":true`)) {
		t.Fatalf("drain replay = %d: %s", status, replayBody)
	}
	status, _ = workerManagementRequest(t, http.MethodPut, server.URL+"/api/worker-groups/group-a/run-state", "admin-secret",
		`{"operation_id":"op-stale","expected_revision":0,"state":"running"}`)
	if status != http.StatusConflict {
		t.Fatalf("stale run-state update = %d, want 409", status)
	}
	enqueueManagedWorkerJob(t, store, "ws-a", "blocked-by-drain")
	status, _ = workerManagementRequest(t, http.MethodPost, server.URL+"/worker/v1/claims", first.WorkerToken,
		`{"worker_id":"worker-a","labels":["linux","arm64"]}`)
	if status != http.StatusNoContent {
		t.Fatalf("claim while draining = %d, want 204", status)
	}
	status, body = workerManagementRequest(t, http.MethodPost, server.URL+"/api/queue-demand-snapshots", "admin-secret",
		`{"selectors":[{"key":"group-a","workspace_id":"ws-a","labels":["linux","arm64"]}]}`)
	if status != http.StatusOK {
		t.Fatalf("demand snapshot while draining = %d: %s", status, body)
	}
	var demand state.QueueDemandSnapshot
	if err := json.Unmarshal(body, &demand); err != nil {
		t.Fatal(err)
	}
	if len(demand.Items) != 1 || demand.Items[0].Claimed != 1 || demand.Items[0].BusyWorkers != 1 {
		t.Fatalf("draining lost active lease demand: %#v", demand.Items)
	}

	revokeBody := fmt.Sprintf(`{"operation_id":"op-revoke-a1","drain_deadline_at":%q}`, deadline)
	status, body = workerManagementRequest(t, http.MethodPost,
		server.URL+"/api/worker-groups/group-a/credentials/"+first.Credential.ID+"/revoke", "admin-secret", revokeBody)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"status":"revoked"`)) {
		t.Fatalf("revoke = %d: %s", status, body)
	}
	status, _ = workerManagementRequest(t, http.MethodPost, server.URL+"/worker/v1/claims", first.WorkerToken,
		`{"worker_id":"worker-a","labels":["linux","arm64"]}`)
	if status != http.StatusForbidden {
		t.Fatalf("claim after revoke = %d, want 403", status)
	}

	leaseJSON, _ := json.Marshal(claim.Lease)
	status, body = workerManagementRequest(t, http.MethodPost,
		server.URL+"/worker/v1/jobs/"+claim.Job.ID+"/heartbeat", first.WorkerToken,
		`{"lease":`+string(leaseJSON)+`}`)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"still_owned":true`)) {
		t.Fatalf("heartbeat during revoked drain = %d: %s", status, body)
	}
	status, body = workerManagementRequest(t, http.MethodPost,
		server.URL+"/worker/v1/jobs/"+claim.Job.ID+"/logs", first.WorkerToken,
		`{"workspace":"ws-a","chunk":"managed drain log\n"}`)
	if status != http.StatusNoContent {
		t.Fatalf("logs during revoked drain = %d: %s", status, body)
	}
	status, body = workerManagementRequest(t, http.MethodPost,
		server.URL+"/worker/v1/jobs/"+claim.Job.ID+"/complete", first.WorkerToken,
		`{"lease":`+string(leaseJSON)+`,"outcome":"succeeded","result":{"app":"echo","action":"run","output":{"ok":true},"exitCode":0}}`)
	if status != http.StatusNoContent {
		t.Fatalf("completion during revoked drain = %d: %s", status, body)
	}

	status, body = workerManagementRequest(t, http.MethodPut, server.URL+"/api/worker-groups/group-a/run-state", "admin-secret",
		`{"operation_id":"op-resume-a","expected_revision":1,"state":"running"}`)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"revision":2`)) {
		t.Fatalf("resume = %d: %s", status, body)
	}
	status, body = workerManagementRequest(t, http.MethodPost, server.URL+"/worker/v1/workers", second.WorkerToken,
		`{"id":"worker-a2","group":"group-a","labels":["linux","arm64"],"slots":1}`)
	if status != http.StatusCreated {
		t.Fatalf("second generation registration = %d: %s", status, body)
	}
	status, body = workerManagementRequest(t, http.MethodPost, server.URL+"/worker/v1/claims", second.WorkerToken,
		`{"worker_id":"worker-a2","labels":["linux","arm64"]}`)
	if status != http.StatusOK {
		t.Fatalf("claim after resume = %d: %s", status, body)
	}

	status, listBody := workerManagementRequest(t, http.MethodGet, server.URL+"/api/worker-groups/group-a/credentials", "admin-secret", "")
	if status != http.StatusOK || bytes.Contains(listBody, []byte(first.WorkerToken)) || bytes.Contains(listBody, []byte("token_hash")) ||
		bytes.Contains(listBody, []byte("requestFingerprint")) {
		t.Fatalf("credential list = %d: %s", status, listBody)
	}
}

func TestManagedWorkerArtifactRequiresOwnedLease(t *testing.T) {
	tempDir := t.TempDir()
	store := state.NewLocalStore(filepath.Join(tempDir, "state.json"))
	artifacts := executionbundle.NewLocalStore(filepath.Join(tempDir, "artifacts"))
	sourceDir := filepath.Join(tempDir, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "main.py"), []byte("print('managed')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	descriptor, err := artifacts.Publish(context.Background(), sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(Config{
		Store: store, Catalog: catalog.NewFileCatalog(filepath.Join(tempDir, "catalog.json")),
		AdminToken: "admin-secret", ArtifactStore: artifacts,
	}))
	defer server.Close()

	status, body := workerManagementRequest(t, http.MethodPost, server.URL+"/api/worker-groups/group-artifact/credentials", "admin-secret",
		`{"operation_id":"op-artifact-credential","expected_generation":0,"workspace_ids":["ws-a"],"labels":["linux"]}`)
	if status != http.StatusCreated {
		t.Fatalf("create artifact credential = %d: %s", status, body)
	}
	var issued issuedWorkerCredential
	if err := json.Unmarshal(body, &issued); err != nil {
		t.Fatal(err)
	}
	status, body = workerManagementRequest(t, http.MethodPost, server.URL+"/worker/v1/workers", issued.WorkerToken,
		`{"id":"worker-artifact","group":"group-artifact","labels":["linux"]}`)
	if status != http.StatusCreated {
		t.Fatalf("register artifact worker = %d: %s", status, body)
	}
	deployment := contract.Deployment{
		Workspace: "ws-a", App: "artifact-app", Commit: "commit-a", BundleDigest: descriptor.Digest,
		RequiredLabels: []string{"linux"}, Actions: map[string]contract.Action{"run": {Action: "run"}},
	}
	run := state.NewRun("windforce", "run-artifact", "artifact-app", "run", deployment, json.RawMessage(`{}`))
	job := state.NewActionJob(run, nil)
	if err := store.CreateRunAndEnqueue(context.Background(), run, job); err != nil {
		t.Fatal(err)
	}
	status, body = workerManagementRequest(t, http.MethodPost, server.URL+"/worker/v1/claims", issued.WorkerToken,
		`{"worker_id":"worker-artifact","labels":["linux"]}`)
	if status != http.StatusOK {
		t.Fatalf("claim artifact job = %d: %s", status, body)
	}

	artifactURL := server.URL + "/worker/v1/artifacts/" + descriptor.Digest
	status, _ = workerManagementRequest(t, http.MethodGet, artifactURL, issued.WorkerToken, "")
	if status != http.StatusForbidden {
		t.Fatalf("unscoped artifact fetch = %d, want 403", status)
	}
	query := url.Values{"job_id": {job.ID}, "workspace": {"ws-a"}, "worker_id": {"worker-artifact"}}
	status, body = workerManagementRequest(t, http.MethodGet, artifactURL+"?"+query.Encode(), issued.WorkerToken, "")
	if status != http.StatusOK || len(body) == 0 {
		t.Fatalf("lease-scoped artifact fetch = %d, bytes=%d", status, len(body))
	}
	query.Set("workspace", "ws-b")
	status, _ = workerManagementRequest(t, http.MethodGet, artifactURL+"?"+query.Encode(), issued.WorkerToken, "")
	if status != http.StatusForbidden {
		t.Fatalf("cross-workspace artifact fetch = %d, want 403", status)
	}
}

func workerManagementRequest(t *testing.T, method string, url string, token string, body string) (int, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, payload
}

func enqueueManagedWorkerJob(t *testing.T, store *state.LocalStore, workspace string, suffix string) state.Job {
	t.Helper()
	deployment := contract.Deployment{
		Workspace: workspace, App: "echo", Commit: "commit-" + suffix,
		RequiredLabels: []string{"linux", "arm64"},
		Actions:        map[string]contract.Action{"run": {Action: "run", Command: []string{"helper"}}},
	}
	run := state.NewRun("windforce", "run-"+suffix, "echo", "run", deployment, json.RawMessage(`{}`))
	job := state.NewActionJob(run, nil)
	if err := store.CreateRunAndEnqueue(context.Background(), run, job); err != nil {
		t.Fatal(err)
	}
	return job
}
