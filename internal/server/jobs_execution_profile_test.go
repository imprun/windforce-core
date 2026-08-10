package server

import (
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/state"
)

func TestHasCompatibleExecutionWorker(t *testing.T) {
	required, err := contract.NewExecutionProfile("", "linux", "amd64", "bun", "1.2.3", "glibc-2.39")
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := contract.NewExecutionProfile("", "windows", "amd64", "bun", "1.2.3", "none")
	if err != nil {
		t.Fatal(err)
	}
	job := state.Job{Payload: state.JobPayload{ExecutionProfile: required}}
	now := time.Now().UTC()
	if hasCompatibleExecutionWorker(job, nil, now) {
		t.Fatal("empty registry reported a compatible worker")
	}
	if hasCompatibleExecutionWorker(job, []state.WorkerRecord{{LastHeartbeatAt: now, ExecutionProfiles: []contract.ExecutionProfile{wrong}}}, now) {
		t.Fatal("incompatible worker reported as compatible")
	}
	if !hasCompatibleExecutionWorker(job, []state.WorkerRecord{{LastHeartbeatAt: now, ExecutionProfiles: []contract.ExecutionProfile{required}}}, now) {
		t.Fatal("compatible live worker was not detected")
	}
}
