package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/state"
)

func TestHumanTaskDeadlineCycleExpiresDisconnectedHold(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	store.ConfigureInputCrypto("deadline-test-secret", "")
	deployment := contract.Deployment{
		Workspace: "default",
		App:       "deadline-app",
		Commit:    "deadline-commit",
		Actions: map[string]contract.Action{
			"wait": {Action: "wait", Command: []string{"helper"}},
		},
	}
	run := state.NewRun("api", state.NewID("run"), deployment.App, "wait", deployment, json.RawMessage(`{}`))
	job := state.NewActionJob(run, json.RawMessage(`{}`))
	if err := store.CreateRunAndEnqueue(ctx, run, job); err != nil {
		t.Fatal(err)
	}
	claimed, _, err := store.ClaimJob(ctx, "worker-deadline", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	expiresAt := now.Add(10 * time.Millisecond)
	task, _, err := store.CreateHeldHumanTask(ctx, state.HumanTask{
		WorkspaceID: "default", RunID: run.ID, JobID: claimed.ID, Attempt: claimed.Attempt,
		Key: "disconnected", RequestFingerprint: "deadline-request", Mode: state.HumanTaskModeHold,
		Kind: "form", Title: "Wait", Schema: json.RawMessage(`{"type":"object"}`), ExpiresAt: &expiresAt,
	})
	if err != nil {
		t.Fatal(err)
	}

	if expired := runHumanTaskDeadlineCycle(ctx, store, now.Add(time.Second), 100); expired != 1 {
		t.Fatalf("expired = %d, want 1", expired)
	}
	stored, err := store.GetHumanTaskForWorkspace(ctx, "default", task.ID)
	if err != nil || stored.State != state.HumanTaskExpired || stored.TerminalCause != state.HumanTaskCauseDeadline {
		t.Fatalf("task = %#v, err=%v", stored, err)
	}
}
