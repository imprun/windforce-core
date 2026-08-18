package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	wfcrypto "github.com/imprun/windforce-core/internal/crypto"
)

type countingResultKeyProvider struct {
	calls int
}

func (p *countingResultKeyProvider) GetWorkspaceKeyVersioned(context.Context, string) (string, int32, error) {
	p.calls++
	return "", 0, errors.New("unexpected result key lookup")
}

func TestLocalStoreListJobsDoesNotDecryptLargeEncryptedResults(t *testing.T) {
	ctx := context.Background()
	statePath := filepath.Join(t.TempDir(), "state.json")
	store := NewLocalStore(statePath)
	store.ConfigureInputCrypto("test-secret-key", "")

	const privateResult = `{"name":"PrivateError","message":"private-result-must-not-be-listed"}`
	encryptedResult, err := store.encryptResult(ctx, "default", json.RawMessage(privateResult))
	if err != nil {
		t.Fatalf("encryptResult returned error: %v", err)
	}
	if !wfcrypto.IsEnc(encryptedResult) {
		t.Fatalf("encrypted result = %s, want encrypted envelope", encryptedResult)
	}
	var envelope struct {
		Ciphertext string `json:"ct"`
	}
	if err := json.Unmarshal(encryptedResult, &envelope); err != nil || envelope.Ciphertext == "" {
		t.Fatalf("encrypted result envelope is invalid: %v", err)
	}

	const jobCount = 102
	padding := strings.Repeat("x", 52*1024)
	if err := store.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		for index := range jobCount {
			runID := fmt.Sprintf("run-%03d", index)
			jobID := fmt.Sprintf("job-%03d", index)
			deployment := contract.Deployment{
				Workspace: "default",
				App:       "echo",
				Commit:    "commit-a",
				Actions: map[string]contract.Action{
					"echo": {Action: "echo"},
				},
			}
			run := NewRun("windforce", runID, "echo", "echo", deployment, json.RawMessage(`{}`))
			run.State = RunFailed
			run.Env = []string{"PADDING=" + padding}
			run.Result = &contract.JobResult{
				Output:     append(json.RawMessage(nil), encryptedResult...),
				DurationMs: int64(index + 1),
				Error:      "public failure",
			}
			run.Error = mustRaw(map[string]any{"message": "public failure", "exitCode": 1})
			run.UpdatedAt = now.Add(time.Duration(index) * time.Nanosecond)

			job := NewActionJob(run, nil)
			job.ID = jobID
			job.RunID = runID
			job.State = JobFailed
			job.UpdatedAt = run.UpdatedAt
			snapshot.Runs[runID] = run
			snapshot.Jobs[jobID] = job
		}
		return nil
	}); err != nil {
		t.Fatalf("populate large snapshot: %v", err)
	}

	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("stat state file: %v", err)
	}
	if info.Size() < 5<<20 {
		t.Fatalf("state file size = %d, want at least 5 MiB", info.Size())
	}

	keyProvider := &countingResultKeyProvider{}
	store.resultKeyProvider = keyProvider
	items, err := store.ListJobs(ctx, JobListQuery{WorkspaceID: "default", Status: "all", Limit: jobCount})
	if err != nil {
		t.Fatalf("ListJobs returned error: %v", err)
	}
	if len(items) != jobCount {
		t.Fatalf("ListJobs returned %d items, want %d", len(items), jobCount)
	}
	if keyProvider.calls != 0 {
		t.Fatalf("ListJobs made %d workspace key lookups, want 0", keyProvider.calls)
	}
	for _, item := range items {
		if item.ErrorSnippet == nil || *item.ErrorSnippet != "public failure" {
			t.Fatalf("job %q error snippet = %v, want public failure", item.ID, item.ErrorSnippet)
		}
		if item.DurationMs < 1 || item.DurationMs > jobCount {
			t.Fatalf("job %q duration = %d, want stored duration", item.ID, item.DurationMs)
		}
	}

	encoded, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal job list: %v", err)
	}
	if bytes.Contains(encoded, []byte("private-result-must-not-be-listed")) || bytes.Contains(encoded, []byte(envelope.Ciphertext)) {
		t.Fatalf("job list exposed encrypted result content: %s", encoded)
	}
}
