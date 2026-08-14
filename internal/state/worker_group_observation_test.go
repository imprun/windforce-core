package state

import (
	"testing"
	"time"
)

func TestBuildWorkerGroupObservationCountsRunningAndActiveSeparately(t *testing.T) {
	observedAt := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	future := observedAt.Add(time.Minute)
	past := observedAt.Add(-time.Minute)
	workers := []WorkerRecord{
		{ID: "group-worker", Group: "group-a", Slots: 2, Status: WorkerStatusActive, CredentialGeneration: 2, LastHeartbeatAt: observedAt},
		{ID: "other-worker", Group: "group-b", Slots: 4, Status: WorkerStatusActive, CredentialGeneration: 1, LastHeartbeatAt: observedAt},
	}
	jobs := []Job{
		{State: JobRunning, LeaseOwner: "group-worker", LeaseExpiresAt: &future},
		{State: JobRunning, LeaseOwner: "group-worker", LeaseExpiresAt: &past},
		{State: JobRunning, LeaseOwner: "missing-worker", LeaseExpiresAt: &future},
		{State: JobRunning, LeaseOwner: "other-worker", LeaseExpiresAt: &future},
	}

	observation := buildWorkerGroupObservation(
		"group-a", WorkerGroupRunState{Group: "group-a", State: WorkerGroupRunning, Revision: 3}, observedAt, workers, jobs,
	)
	if observation.LiveWorkers != 1 || observation.AvailableSlots != 1 ||
		observation.ActiveLeases != 1 || observation.RunningJobs != 2 ||
		observation.UnattributedActiveLeases != 1 || observation.UnattributedRunningJobs != 1 || observation.Quiescent {
		t.Fatalf("observation = %#v", observation)
	}

	draining := buildWorkerGroupObservation(
		"group-a", WorkerGroupRunState{Group: "group-a", State: WorkerGroupDraining, Revision: 4}, observedAt, workers, nil,
	)
	if !draining.Quiescent || draining.AvailableSlots != 0 || draining.LiveWorkers != 1 {
		t.Fatalf("draining observation = %#v", draining)
	}
}
