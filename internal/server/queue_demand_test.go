package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/state"
)

func TestQueueDemandSnapshotHTTPContractAndAuthorization(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(t.TempDir() + "/state.json")
	if _, err := store.CreateWorkspace(ctx, "ws-a", "Workspace A", "operator:test"); err != nil {
		t.Fatal(err)
	}
	workspaceToken := "wfw_workspace_test_token"
	if _, err := store.CreateWorkspaceToken(ctx, "ws-a", "test", state.HashWorkspaceToken(workspaceToken), "operator:test"); err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	runID := state.NewID("run")
	jobID := state.NewID("job")
	if err := store.CreateRunAndEnqueue(ctx,
		state.Run{ID: runID, App: "orders", Action: "run", State: state.RunQueued, CreatedAt: createdAt, UpdatedAt: createdAt},
		state.Job{
			ID: jobID, RunID: runID, State: state.JobQueued, Kind: "action", Priority: 100,
			Payload:   state.JobPayload{Workspace: "ws-a", App: "orders", Action: "run", Tag: "managed", RequiredLabels: []string{"arm64"}},
			CreatedAt: createdAt, UpdatedAt: createdAt,
		}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(New(Config{Store: store, AdminToken: "admin-secret"}))
	defer server.Close()
	body := []byte(`{"selectors":[{"key":"pool-a","workspace_id":"ws-a","tags":["managed"],"labels":["arm64"]}]}`)

	unauthorized := postQueueDemandSnapshot(t, server.URL, "", body)
	if unauthorized.StatusCode != http.StatusUnauthorized {
		defer unauthorized.Body.Close()
		t.Fatalf("unauthenticated status = %d, want 401", unauthorized.StatusCode)
	}
	unauthorized.Body.Close()

	workspaceScoped := postQueueDemandSnapshot(t, server.URL, workspaceToken, body)
	if workspaceScoped.StatusCode != http.StatusUnauthorized {
		defer workspaceScoped.Body.Close()
		t.Fatalf("workspace token status = %d, want 401", workspaceScoped.StatusCode)
	}
	workspaceScoped.Body.Close()

	response := postQueueDemandSnapshot(t, server.URL, "admin-secret", body)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("admin status = %d, want 200", response.StatusCode)
	}
	var snapshot state.QueueDemandSnapshot
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.StoreEpoch == "" || snapshot.SnapshotRevision <= 0 || len(snapshot.Items) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if item := snapshot.Items[0]; item.Selector.Key != "pool-a" || item.Eligible != 1 || item.Queued != 1 {
		t.Fatalf("snapshot item = %#v", item)
	}

	duplicate := postQueueDemandSnapshot(t, server.URL, "admin-secret", []byte(`{"selectors":[{"key":"same","workspace_id":"ws-a"},{"key":"same","workspace_id":"ws-a"}]}`))
	defer duplicate.Body.Close()
	if duplicate.StatusCode != http.StatusBadRequest {
		t.Fatalf("duplicate selector status = %d, want 400", duplicate.StatusCode)
	}
}

func TestQueueDemandSnapshotAppearsInControlPlaneOpenAPI(t *testing.T) {
	document := buildControlPlaneOpenAPI("http://core.example", "ws-a")
	paths := document["paths"].(map[string]any)
	if _, ok := paths["/api/queue-demand-snapshots"]; !ok {
		t.Fatal("queue demand snapshot path missing from control-plane OpenAPI")
	}
	components := document["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	for _, name := range []string{"QueueDemandSelector", "QueueDemandSnapshotRequest", "QueueDemand", "QueueDemandSnapshot"} {
		if _, ok := schemas[name]; !ok {
			t.Fatalf("schema %q missing from control-plane OpenAPI", name)
		}
	}
}

func postQueueDemandSnapshot(t *testing.T, baseURL string, bearerToken string, body []byte) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, baseURL+"/api/queue-demand-snapshots", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if bearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
