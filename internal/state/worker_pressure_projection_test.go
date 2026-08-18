package state

import (
	"slices"
	"testing"
	"time"
)

func TestWorkerPressureIsSeparateFromLifecycleAndPlacement(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	ratio := 0.95
	worker := WorkerRecord{
		ID: "worker-pressure", Group: "pressure", Tags: []string{"ready"}, Slots: 4,
		Status: WorkerStatusActive, LastHeartbeatAt: now,
		ResourcePressure: &WorkerResourcePressure{
			AcceptingClaims: false, ReasonCode: WorkerPressureReasonMemoryHigh,
			Scope: WorkerPressureScopeCgroupV2, ObservedAt: now, FreshUntil: now.Add(time.Minute),
			Measurements: map[string]WorkerResourceMeasurement{
				WorkerPressureResourceMemory: {Supported: true, Ratio: &ratio},
			},
		},
	}
	item, err := buildWorkerGroupInventoryItem("pressure", "workspace-a", false, now, []WorkerRecord{worker}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != "ready" || item.LiveWorkers != 1 || item.PressureAcceptingWorkers != 0 || item.PressurePausedWorkers != 1 {
		t.Fatalf("pressure inventory = %#v", item)
	}
	if item.TotalSlots != 0 || item.AvailableSlots != 0 || !slices.Contains(item.PressureReasonCodes, WorkerPressureReasonMemoryHigh) {
		t.Fatalf("pressure capacity = %#v", item)
	}
	observation := buildWorkerGroupObservation("pressure", DefaultWorkerGroupRunState("pressure"), now, []WorkerRecord{worker}, nil)
	if observation.LiveWorkers != 1 || observation.PressureAcceptingWorkers != 0 || observation.PressurePausedWorkers != 1 ||
		observation.AvailableSlots != 0 || !slices.Contains(observation.PressureReasonCodes, WorkerPressureReasonMemoryHigh) {
		t.Fatalf("pressure group observation = %#v", observation)
	}

	candidate, err := buildWorkerGroupPlacementCandidate(
		placementTarget{tag: "ready"}, item, "workspace-a", now, []WorkerRecord{worker}, nil, nil, workerActiveLeaseCounts{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Eligible || !slices.Contains(candidate.ReasonCodes, PlacementReasonResourcePressure) {
		t.Fatalf("pressure placement candidate = %#v", candidate)
	}
}

func TestWorkerPressureInventoryReportsStaleObservationWithoutResuming(t *testing.T) {
	now := time.Now().UTC()
	worker := WorkerRecord{
		ID: "worker-stale", Group: "pressure", Status: WorkerStatusActive, LastHeartbeatAt: now,
		ResourcePressure: &WorkerResourcePressure{
			AcceptingClaims: false, ReasonCode: WorkerPressureReasonObservationUnknown,
			Scope: WorkerPressureScopeUnknown, ObservedAt: now.Add(-time.Minute), FreshUntil: now.Add(-time.Second),
		},
	}
	item, err := buildWorkerGroupInventoryItem("pressure", "workspace-a", false, now, []WorkerRecord{worker}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if item.StalePressureWorkers != 1 || item.PressurePausedWorkers != 1 || item.PressureAcceptingWorkers != 0 {
		t.Fatalf("stale pressure inventory = %#v", item)
	}
}
