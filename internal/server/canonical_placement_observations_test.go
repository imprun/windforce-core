package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/state"
)

func TestCanonicalPlacementObservationsAuthorizationAndRedaction(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	if _, err := store.CreateWorkspace(ctx, "team-a", "Team A", "test"); err != nil {
		t.Fatal(err)
	}
	workspaceToken := "wfw_workspace-projection-token"
	if _, err := store.CreateWorkspaceToken(ctx, "team-a", "Console", state.HashWorkspaceToken(workspaceToken), "test"); err != nil {
		t.Fatal(err)
	}
	deployment := contract.Deployment{
		Workspace: "team-a", GitSourceID: "source-a", App: "echo", Commit: "commit-a", Tag: "ready",
		Actions: map[string]contract.Action{"run": {Action: "run"}},
	}
	if _, err := store.PublishRelease(ctx, deployment, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	allowed := createServerPlacementCredential(t, store, "group-allowed", []string{"team-a"}, "allowed-token")
	hidden := createServerPlacementCredential(t, store, "group-hidden", []string{"team-b"}, "hidden-token")
	for _, worker := range []state.WorkerRecord{
		{ID: "physical-worker-allowed", Group: allowed.Group, Tags: []string{"ready"}, Slots: 2, Status: state.WorkerStatusActive, CredentialID: allowed.ID, CredentialGeneration: allowed.Generation},
		{ID: "physical-worker-hidden", Group: hidden.Group, Tags: []string{"ready"}, Slots: 3, Status: state.WorkerStatusActive, CredentialID: hidden.ID, CredentialGeneration: hidden.Generation},
	} {
		if err := store.RegisterWorker(ctx, worker); err != nil {
			t.Fatal(err)
		}
	}
	queuedRun := state.NewRun("api", "run-demand-queued", "echo", "run", deployment, json.RawMessage(`{}`))
	queuedJob := state.NewActionJob(queuedRun, nil)
	queuedJob.ID = "job-demand-queued"
	if err := store.CreateRunAndEnqueue(ctx, queuedRun, queuedJob); err != nil {
		t.Fatal(err)
	}
	runningRun := state.NewRun("api", "run-demand-running", "echo", "run", deployment, json.RawMessage(`{}`))
	runningJob := state.NewActionJob(runningRun, nil)
	runningJob.ID = "job-demand-running"
	runningJob.State = state.JobRunning
	runningJob.LeaseOwner = "physical-worker-allowed"
	leaseExpiresAt := time.Now().UTC().Add(10 * time.Minute)
	runningJob.LeaseExpiresAt = &leaseExpiresAt
	if err := store.CreateRunAndEnqueue(ctx, runningRun, runningJob); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(New(Config{
		Store: store, Catalog: store, ManagedWorkspaces: true, AdminToken: "instance-admin",
	}))
	defer server.Close()

	workspaceInventoryResponse := workspaceRequest(t, server.URL, http.MethodGet, "/api/w/team-a/worker-groups", workspaceToken, "")
	if workspaceInventoryResponse.StatusCode != http.StatusOK {
		t.Fatalf("workspace inventory status = %d: %s", workspaceInventoryResponse.StatusCode, readResponse(t, workspaceInventoryResponse))
	}
	var workspaceInventory state.WorkerGroupInventory
	decodeResponse(t, workspaceInventoryResponse, &workspaceInventory)
	if len(workspaceInventory.Groups) != 1 || workspaceInventory.Groups[0].Group != "group-allowed" {
		t.Fatalf("workspace inventory = %#v", workspaceInventory)
	}
	if group := workspaceInventory.Groups[0]; group.TotalSlots != 2 || group.OccupiedSlots != 1 || group.AvailableSlots != 1 {
		t.Fatalf("workspace inventory capacity = %#v", group)
	}

	adminInventoryResponse := workspaceRequest(t, server.URL, http.MethodGet, "/api/w/team-a/worker-groups", "instance-admin", "")
	if adminInventoryResponse.StatusCode != http.StatusOK {
		t.Fatalf("admin inventory status = %d: %s", adminInventoryResponse.StatusCode, readResponse(t, adminInventoryResponse))
	}
	var adminInventory state.WorkerGroupInventory
	decodeResponse(t, adminInventoryResponse, &adminInventory)
	if len(adminInventory.Groups) != 2 || adminInventory.Groups[1].Group != "group-hidden" || adminInventory.Groups[1].WorkspaceAllowed {
		t.Fatalf("admin inventory = %#v", adminInventory)
	}

	placementsResponse := workspaceRequest(t, server.URL, http.MethodGet, "/api/w/team-a/apps/echo/actions/run/placement-candidates", workspaceToken, "")
	if placementsResponse.StatusCode != http.StatusOK {
		t.Fatalf("placement candidates status = %d: %s", placementsResponse.StatusCode, readResponse(t, placementsResponse))
	}
	var placements state.PlacementCandidates
	decodeResponse(t, placementsResponse, &placements)
	if len(placements.Targets) != 1 || placements.Targets[0].MatchingWorkers != 1 || placements.Targets[0].MatchingSlots != 2 || len(placements.Targets[0].Candidates) != 1 {
		t.Fatalf("workspace placement candidates = %#v", placements)
	}
	encoded, err := json.Marshal(placements)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"physical-worker-allowed", allowed.ID, "allowed-token"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("placement response exposed %q: %s", forbidden, encoded)
		}
	}

	unauthorizedDemand := workspaceRequest(t, server.URL, http.MethodGet, "/api/w/team-a/execution-demand", "", "")
	if unauthorizedDemand.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized execution demand status = %d: %s", unauthorizedDemand.StatusCode, readResponse(t, unauthorizedDemand))
	}
	demandResponse := workspaceRequest(t, server.URL, http.MethodGet, "/api/w/team-a/execution-demand", workspaceToken, "")
	if demandResponse.StatusCode != http.StatusOK {
		t.Fatalf("execution demand status = %d: %s", demandResponse.StatusCode, readResponse(t, demandResponse))
	}
	var demand state.ExecutionDemand
	decodeResponse(t, demandResponse, &demand)
	if demand.Workspace != "team-a" || demand.QueuedJobs != 1 || demand.OldestQueuedAt == nil || len(demand.Targets) != 1 {
		t.Fatalf("workspace execution demand = %#v", demand)
	}
	demandTarget := demand.Targets[0]
	if demandTarget.App != "echo" || demandTarget.Action != "run" || demandTarget.MatchingWorkers != 1 || demandTarget.TotalSlots != 2 || demandTarget.OccupiedSlots != 1 || demandTarget.AvailableSlots != 1 || demandTarget.Saturated || len(demandTarget.Candidates) != 1 {
		t.Fatalf("execution demand target = %#v", demandTarget)
	}
	encodedDemand, err := json.Marshal(demand)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"physical-worker-allowed", allowed.ID, "allowed-token", "job-demand-queued", "run-demand-queued"} {
		if strings.Contains(string(encodedDemand), forbidden) {
			t.Fatalf("execution demand response exposed %q: %s", forbidden, encodedDemand)
		}
	}
	adminDemandResponse := workspaceRequest(t, server.URL, http.MethodGet, "/api/w/team-a/execution-demand", "instance-admin", "")
	if adminDemandResponse.StatusCode != http.StatusOK {
		t.Fatalf("admin execution demand status = %d: %s", adminDemandResponse.StatusCode, readResponse(t, adminDemandResponse))
	}
	var adminDemand state.ExecutionDemand
	decodeResponse(t, adminDemandResponse, &adminDemand)
	if len(adminDemand.Targets) != 1 || len(adminDemand.Targets[0].Candidates) != 2 {
		t.Fatalf("admin execution demand = %#v", adminDemand)
	}
	if hiddenCandidate := adminDemand.Targets[0].Candidates[1]; hiddenCandidate.Group != "group-hidden" || hiddenCandidate.WorkspaceAllowed || hiddenCandidate.Eligible || hiddenCandidate.MatchingSlots != 0 {
		t.Fatalf("admin hidden demand candidate = %#v", hiddenCandidate)
	}
	for _, path := range []string{
		"/api/w/team-a/apps/echo/execution-demand",
		"/api/w/team-a/apps/echo/actions/run/execution-demand",
	} {
		response := workspaceRequest(t, server.URL, http.MethodGet, path, workspaceToken, "")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("filtered execution demand %s status = %d: %s", path, response.StatusCode, readResponse(t, response))
		}
		var filtered state.ExecutionDemand
		decodeResponse(t, response, &filtered)
		if filtered.QueuedJobs != 1 || len(filtered.Targets) != 1 {
			t.Fatalf("filtered execution demand %s = %#v", path, filtered)
		}
	}
	emptyDemand := workspaceRequest(t, server.URL, http.MethodGet, "/api/w/team-a/apps/deleted/execution-demand", workspaceToken, "")
	if emptyDemand.StatusCode != http.StatusOK {
		t.Fatalf("deleted app execution demand status = %d: %s", emptyDemand.StatusCode, readResponse(t, emptyDemand))
	}
	var empty state.ExecutionDemand
	decodeResponse(t, emptyDemand, &empty)
	if empty.QueuedJobs != 0 || len(empty.Targets) != 0 {
		t.Fatalf("deleted app execution demand = %#v", empty)
	}

	missingAction := workspaceRequest(t, server.URL, http.MethodGet, "/api/w/team-a/apps/echo/actions/missing/placement-candidates", workspaceToken, "")
	if missingAction.StatusCode != http.StatusNotFound {
		t.Fatalf("missing action status = %d: %s", missingAction.StatusCode, readResponse(t, missingAction))
	}

	rawWorkspace := workspaceRequest(t, server.URL, http.MethodGet, "/api/w/team-a/workers", workspaceToken, "")
	if rawWorkspace.StatusCode != http.StatusForbidden {
		t.Fatalf("raw workspace worker status = %d: %s", rawWorkspace.StatusCode, readResponse(t, rawWorkspace))
	}
	rawAdmin := workspaceRequest(t, server.URL, http.MethodGet, "/api/w/team-a/workers", "instance-admin", "")
	if rawAdmin.StatusCode != http.StatusOK {
		t.Fatalf("raw admin worker status = %d: %s", rawAdmin.StatusCode, readResponse(t, rawAdmin))
	}
}

func TestPlacementObservationsAppearInControlPlaneOpenAPI(t *testing.T) {
	document := buildControlPlaneOpenAPI("http://core.example", "team-a")
	paths := document["paths"].(map[string]any)
	for _, path := range []string{
		"/api/w/{workspace}/worker-groups",
		"/api/w/{workspace}/execution-demand",
		"/api/w/{workspace}/apps/{app}/execution-demand",
		"/api/w/{workspace}/apps/{app}/actions/{action}/execution-demand",
		"/api/w/{workspace}/apps/{app}/placement-candidates",
		"/api/w/{workspace}/apps/{app}/actions/{action}/placement-candidates",
	} {
		if paths[path] == nil {
			t.Fatalf("placement observation path %q missing from control-plane OpenAPI", path)
		}
	}
	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	for _, name := range []string{
		"WorkerGroupInventory", "WorkerGroupInventoryItem", "PlacementCandidates",
		"PlacementTargetCandidates", "WorkerGroupPlacementCandidate", "ExecutionDemand", "ExecutionDemandTarget",
	} {
		if schemas[name] == nil {
			t.Fatalf("placement observation schema %q missing from control-plane OpenAPI", name)
		}
	}
}

func createServerPlacementCredential(t *testing.T, store state.WorkerControlStore, group string, workspaceIDs []string, token string) state.WorkerCredential {
	t.Helper()
	credential, replayed, err := store.CreateWorkerCredential(context.Background(), state.CreateWorkerCredentialRequest{
		Group: group, ExpectedGeneration: 0, WorkspaceIDs: workspaceIDs, TokenHash: state.HashBearerToken(token),
		OperationID: "server-placement-" + group, RequestFingerprint: "server-placement-fingerprint-" + group, Actor: "test",
	})
	if err != nil || replayed {
		t.Fatalf("CreateWorkerCredential(%s) replayed=%t, err=%v", group, replayed, err)
	}
	return credential
}
