package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/imprun/windforce-core/internal/state"
)

const testExecutionBundleDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func admitTestRun(t *testing.T, store *state.LocalStore, baseURL string, workspace string, app string, action string, input string) invocationRunView {
	t.Helper()
	if _, err := store.GetWorkspace(context.Background(), workspace); err != nil {
		if _, createErr := store.CreateWorkspace(context.Background(), workspace, workspace, "test"); createErr != nil {
			t.Fatal(createErr)
		}
	}
	body, err := json.Marshal(map[string]any{
		"app":    app,
		"action": action,
		"input":  json.RawMessage(input),
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/workspaces/"+workspace+"/runs", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer test-operator")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("admit Run status = %d, want 201", response.StatusCode)
	}
	var run invocationRunView
	if err := json.NewDecoder(response.Body).Decode(&run); err != nil {
		t.Fatal(err)
	}
	return run
}

func testJobForRun(t *testing.T, store *state.LocalStore, runID string) state.Job {
	t.Helper()
	snapshot, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range snapshot.Jobs {
		if job.RunID == runID {
			return job
		}
	}
	t.Fatalf("job for Run %q not found", runID)
	return state.Job{}
}
