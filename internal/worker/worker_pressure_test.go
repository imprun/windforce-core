package worker

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	actionruntime "github.com/imprun/windforce-core/internal/runtime"
	"github.com/imprun/windforce-core/internal/state"
)

type fixedPressureObserver struct {
	observation state.WorkerResourcePressure
}

func (o fixedPressureObserver) Observe(context.Context) state.WorkerResourcePressure {
	return o.observation
}

type toggledPressureObserver struct {
	mu       sync.Mutex
	high     bool
	sequence int64
}

func (o *toggledPressureObserver) Observe(context.Context) state.WorkerResourcePressure {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sequence++
	ratio := 0.2
	reason := ""
	accepting := true
	if o.high {
		ratio = 0.95
		reason = state.WorkerPressureReasonMemoryHigh
		accepting = false
	}
	now := time.Now().UTC().Add(time.Duration(o.sequence))
	return state.WorkerResourcePressure{
		Supported: true, AcceptingClaims: accepting, ReasonCode: reason,
		Scope: state.WorkerPressureScopeCgroupV2, ObservedAt: now, FreshUntil: now.Add(state.WorkerPressureFreshTTL),
		Measurements: map[string]state.WorkerResourceMeasurement{
			state.WorkerPressureResourceMemory: {Supported: true, Ratio: &ratio},
		},
	}
}

func (o *toggledPressureObserver) setHigh() {
	o.mu.Lock()
	o.high = true
	o.mu.Unlock()
}

type blockingPressureRunner struct {
	started  chan struct{}
	release  chan struct{}
	canceled chan struct{}
}

func (r *blockingPressureRunner) Run(ctx context.Context, _ actionruntime.RunRequest) (contract.JobResult, error) {
	close(r.started)
	select {
	case <-r.release:
		return contract.JobResult{ExitCode: 0, Output: json.RawMessage(`{"ok":true}`)}, nil
	case <-ctx.Done():
		close(r.canceled)
		return contract.JobResult{}, ctx.Err()
	}
}

func TestRunOncePressurePauseRegistersWithoutClaiming(t *testing.T) {
	store := &onceLifecycleStore{}
	ratio := 0.95
	processor := Processor{
		Store: store, WorkerID: "pressure-once", ResourcePressure: fixedPressureObserver{observation: state.WorkerResourcePressure{
			AcceptingClaims: false, ReasonCode: state.WorkerPressureReasonMemoryHigh,
			Scope: state.WorkerPressureScopeCgroupV2, ObservedAt: time.Now().UTC(),
			Measurements: map[string]state.WorkerResourceMeasurement{
				state.WorkerPressureResourceMemory: {Supported: true, Ratio: &ratio},
			},
		}},
	}
	processed, err := processor.RunOnce(context.Background())
	if err != nil || processed {
		t.Fatalf("RunOnce() = %v, %v", processed, err)
	}
	if got, want := strings.Join(store.calls, ","), "register,deregister"; got != want {
		t.Fatalf("lifecycle calls = %q, want %q", got, want)
	}
	if store.record.ResourcePressure == nil || store.record.ResourcePressure.AcceptingClaims {
		t.Fatalf("registered pressure = %#v", store.record.ResourcePressure)
	}
}

func TestPressureTransitionDoesNotCancelRunningJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	deployment := contract.Deployment{
		Workspace: "workspace-a", App: "pressure", Commit: "commit-a",
		Actions: map[string]contract.Action{"run": {Action: "run", Command: []string{"helper"}}},
	}
	run := state.NewRun("test", "run-pressure-active", "pressure", "run", deployment, json.RawMessage(`{}`))
	if err := store.CreateRunAndEnqueue(ctx, run, state.NewActionJob(run, nil)); err != nil {
		t.Fatal(err)
	}
	observer := &toggledPressureObserver{}
	runner := &blockingPressureRunner{started: make(chan struct{}), release: make(chan struct{}), canceled: make(chan struct{})}
	processor := Processor{
		Store: store, Runner: runner, WorkerID: "pressure-loop", LeaseTTL: time.Minute,
		DrainTimeout: time.Second, ResourcePressure: observer, registryHeartbeatEvery: 5 * time.Millisecond,
	}
	done := make(chan error, 1)
	go func() { done <- processor.RunLoop(ctx, 5*time.Millisecond) }()
	select {
	case <-runner.started:
	case <-time.After(2 * time.Second):
		t.Fatal("job did not start")
	}
	observer.setHigh()
	deadline := time.Now().Add(2 * time.Second)
	for {
		workers, err := store.ListWorkers(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(workers) == 1 && workers[0].ResourcePressure != nil && !workers[0].ResourcePressure.AcceptingClaims {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("high pressure was not reported while the job was running")
		}
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case <-runner.canceled:
		t.Fatal("pressure transition canceled the running job")
	case <-time.After(25 * time.Millisecond):
	}
	close(runner.release)

	deadline = time.Now().Add(2 * time.Second)
	for {
		stored, err := store.GetRun(context.Background(), run.ID)
		if err == nil && stored.State == state.RunSucceeded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not complete after pressure transition: state=%s err=%v", stored.State, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker loop did not stop")
	}
}
