package execution

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/state"
)

func TestProbeRunReturnsStableIdentityWithoutCreatingRun(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	if _, err := store.CreateWorkspace(ctx, "ws", "Workspace", "tester"); err != nil {
		t.Fatal(err)
	}
	service := NewAdmissionService(store, nil, nil)
	request := CreateRunRequest{
		Workspace:      "ws",
		App:            "demo",
		Action:         "run",
		Input:          json.RawMessage(`{"value":1}`),
		IdempotencyKey: "delivery-1",
		Principal:      OperatorPrincipal("ws", "operator:test"),
	}

	first, err := service.ProbeRun(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.ProbeRun(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.AdmissionID == "" || first.AdmissionID != second.AdmissionID || first.State != "ready" || second.State != "ready" || first.Replayed || second.Replayed {
		t.Fatalf("unexpected probes: first=%#v second=%#v", first, second)
	}
	if _, err := store.GetRun(ctx, first.AdmissionID); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("probe created a Run: %v", err)
	}
}

func TestProbeRunRecognizesExactReplayAndRejectsMismatch(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	if _, err := store.CreateWorkspace(ctx, "ws", "Workspace", "tester"); err != nil {
		t.Fatal(err)
	}
	service := NewAdmissionService(store, nil, nil)
	request := CreateRunRequest{
		Workspace:      "ws",
		App:            "demo",
		Action:         "run",
		Input:          json.RawMessage(`{"value":1}`),
		IdempotencyKey: "delivery-1",
		Principal:      OperatorPrincipal("ws", "operator:test"),
	}
	probe, err := service.ProbeRun(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := invocationRequestFingerprint(request.App, request.Action, request.Input, "", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	run := state.NewRun("http", probe.AdmissionID, request.App, request.Action, contract.Deployment{Workspace: "ws", App: request.App}, request.Input)
	run.State = state.RunQueued
	run.RequestFingerprint = fingerprint
	run.PrincipalKind = string(request.Principal.Kind)
	job := state.NewActionJob(run, request.Input)
	if err := store.CreateRunAndEnqueue(ctx, run, job); err != nil {
		t.Fatal(err)
	}

	replayed, err := service.ProbeRun(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.State != "admitted" || replayed.RunID != run.ID || replayed.RunState != state.RunQueued {
		t.Fatalf("replay probe = %#v", replayed)
	}
	request.Input = json.RawMessage(`{"value":2}`)
	if _, err := service.ProbeRun(ctx, request); FaultKindOf(err) != FaultConflict {
		t.Fatalf("mismatched probe error = %v (%s)", err, FaultKindOf(err))
	}
}

func TestProbeRunRequiresIdempotencyAndAuthorizedPrincipal(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	service := NewAdmissionService(store, nil, nil)
	request := CreateRunRequest{Workspace: "ws", App: "demo", Action: "run", Input: json.RawMessage(`{}`)}
	if _, err := service.ProbeRun(ctx, request); FaultKindOf(err) != FaultInvalidRequest {
		t.Fatalf("missing key error = %v (%s)", err, FaultKindOf(err))
	}
	request.IdempotencyKey = "delivery-1"
	if _, err := service.ProbeRun(ctx, request); FaultKindOf(err) != FaultForbidden {
		t.Fatalf("missing principal error = %v (%s)", err, FaultKindOf(err))
	}
}
