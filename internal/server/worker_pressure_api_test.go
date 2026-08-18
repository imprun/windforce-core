package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/imprun/windforce-core/internal/state"
)

func TestWorkerPlanePressureRegistrationHeartbeatAndFinalClaimGate(t *testing.T) {
	server, store := newWorkerPlaneServer(t)
	resp, payload := workerPlanePost(t, server.URL+"/worker/v1/workers", "admin-secret", `{
		"id":"worker-pressure","group":"pressure","tags":["ready"],
		"resource_pressure":{"supported":true,"accepting_claims":false,"reason_code":"memory_high","scope":"cgroup_v2","measurements":{"memory":{"supported":true,"ratio":0.95}}}
	}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register = %d: %s", resp.StatusCode, payload)
	}

	resp, payload = workerPlanePost(t, server.URL+"/worker/v1/claims", "admin-secret", `{"worker_id":"worker-pressure","tags":["ready"]}`)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("pressure-paused claim = %d: %s", resp.StatusCode, payload)
	}

	resp, payload = workerPlanePost(t, server.URL+"/worker/v1/workers/worker-pressure/heartbeat", "admin-secret", `{
		"resource_pressure":{"supported":true,"accepting_claims":true,"scope":"cgroup_v2","measurements":{"memory":{"supported":true,"ratio":0.2}}}
	}`)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("pressure heartbeat = %d: %s", resp.StatusCode, payload)
	}
	workers, err := store.ListWorkers(context.Background())
	if err != nil || len(workers) != 1 || workers[0].ResourcePressure == nil || !workers[0].ResourcePressure.AcceptingClaims {
		t.Fatalf("stored pressure = %#v, err=%v", workers, err)
	}

	resp, payload = workerPlanePost(t, server.URL+"/worker/v1/workers/worker-pressure/heartbeat", "admin-secret", "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("legacy empty heartbeat = %d: %s", resp.StatusCode, payload)
	}
}

func TestWorkerPlaneRejectsInvalidPressureWithoutEchoingInput(t *testing.T) {
	server, _ := newWorkerPlaneServer(t)
	secretMarker := "must-not-echo"
	resp, payload := workerPlanePost(t, server.URL+"/worker/v1/workers", "admin-secret", `{
		"id":"worker-invalid","resource_pressure":{"accepting_claims":false,"reason_code":"`+secretMarker+`","scope":"unknown"}
	}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid pressure = %d: %s", resp.StatusCode, payload)
	}
	if strings.Contains(payload, secretMarker) {
		t.Fatalf("invalid pressure response echoed untrusted reason: %s", payload)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(payload), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] == nil {
		t.Fatalf("error response = %s", payload)
	}
}

func TestCanonicalWorkerAndGroupObservationsExposeRedactedPressure(t *testing.T) {
	server, store := newWorkerPlaneServer(t)
	ratio := 0.95
	if err := store.RegisterWorker(context.Background(), state.WorkerRecord{
		ID: "worker-observed", Group: "pressure", Slots: 4, Status: state.WorkerStatusActive,
		ResourcePressure: &state.WorkerResourcePressure{
			AcceptingClaims: false, ReasonCode: state.WorkerPressureReasonMemoryHigh,
			Scope: state.WorkerPressureScopeCgroupV2,
			Measurements: map[string]state.WorkerResourceMeasurement{
				state.WorkerPressureResourceMemory: {Supported: true, Ratio: &ratio},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	get := func(path string, target any) string {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, server.URL+path, nil)
		req.Header.Set("Authorization", "Bearer admin-secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", path, resp.StatusCode, raw)
		}
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	var workers struct {
		Workers []struct {
			ResourcePressure *state.WorkerResourcePressure `json:"resource_pressure"`
		} `json:"workers"`
	}
	raw := get("/api/w/default/workers", &workers)
	if len(workers.Workers) != 1 || workers.Workers[0].ResourcePressure == nil || workers.Workers[0].ResourcePressure.AcceptingClaims {
		t.Fatalf("worker observation = %s", raw)
	}
	for _, forbidden := range []string{"credential", "token", "filesystem", "raw_error"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("worker observation leaked %q: %s", forbidden, raw)
		}
	}

	var inventory state.WorkerGroupInventory
	get("/api/w/default/worker-groups", &inventory)
	if len(inventory.Groups) != 1 || inventory.Groups[0].Status != "ready" ||
		inventory.Groups[0].PressurePausedWorkers != 1 || inventory.Groups[0].PressureAcceptingWorkers != 0 || inventory.Groups[0].TotalSlots != 0 {
		t.Fatalf("group pressure observation = %#v", inventory.Groups)
	}
}
