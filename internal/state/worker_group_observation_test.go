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
		{State: JobRunning, LeaseOwner: "group-worker", LeaseExpiresAt: &future, LeaseIdentity: &WorkerLeaseIdentity{Group: "group-a", CredentialGeneration: 2}},
		{State: JobRunning, LeaseOwner: "group-worker", LeaseExpiresAt: &past, LeaseIdentity: &WorkerLeaseIdentity{Group: "group-a", CredentialGeneration: 2}},
		{State: JobRunning, LeaseOwner: "missing-worker", LeaseExpiresAt: &future},
		{State: JobRunning, LeaseOwner: "other-worker", LeaseExpiresAt: &future, LeaseIdentity: &WorkerLeaseIdentity{Group: "group-b", CredentialGeneration: 1}},
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

func TestBuildWorkerGroupObservationDoesNotReattributeReusedWorkerID(t *testing.T) {
	observedAt := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	future := observedAt.Add(time.Minute)
	workers := []WorkerRecord{{
		ID: "reused-worker", Group: "group-b", Slots: 1, Status: WorkerStatusActive,
		CredentialGeneration: 7, LastHeartbeatAt: observedAt,
	}}
	jobs := []Job{{
		State: JobRunning, LeaseOwner: "reused-worker", LeaseExpiresAt: &future,
		LeaseIdentity: &WorkerLeaseIdentity{Group: "group-a", CredentialGeneration: 3},
	}}

	groupA := buildWorkerGroupObservation(
		"group-a", WorkerGroupRunState{Group: "group-a", State: WorkerGroupDraining}, observedAt, workers, jobs,
	)
	if groupA.RunningJobs != 1 || groupA.ActiveLeases != 1 || groupA.Quiescent ||
		groupA.UnattributedRunningJobs != 0 || groupA.UnattributedActiveLeases != 0 {
		t.Fatalf("original group observation = %#v", groupA)
	}

	groupB := buildWorkerGroupObservation(
		"group-b", WorkerGroupRunState{Group: "group-b", State: WorkerGroupDraining}, observedAt, workers, jobs,
	)
	if groupB.RunningJobs != 0 || groupB.ActiveLeases != 0 || !groupB.Quiescent {
		t.Fatalf("reused-ID group observation = %#v", groupB)
	}
}
