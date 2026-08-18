package state

import (
	"sort"
	"strings"
	"time"
)

// WorkerGenerationActivity is a redacted count of live Worker registrations
// using one managed credential generation. Generation zero represents Workers
// registered through the legacy static Worker Plane credential.
type WorkerGenerationActivity struct {
	Generation int64 `json:"generation"`
	Workers    int   `json:"workers"`
}

// WorkerGroupObservation is a consistent, group-scoped operational snapshot.
// It intentionally excludes Worker identities and credential material.
type WorkerGroupObservation struct {
	Group                     string                     `json:"group"`
	RunState                  string                     `json:"run_state"`
	RunStateRevision          int64                      `json:"run_state_revision"`
	DeadlineAt                *time.Time                 `json:"deadline_at,omitempty"`
	ObservedAt                time.Time                  `json:"observed_at"`
	LiveWorkers               int                        `json:"live_workers"`
	PressureAcceptingWorkers  int                        `json:"pressure_accepting_workers"`
	PressurePausedWorkers     int                        `json:"pressure_paused_workers"`
	StalePressureWorkers      int                        `json:"stale_pressure_workers"`
	PressureReasonCodes       []string                   `json:"pressure_reason_codes"`
	UnmanagedLiveWorkers      int                        `json:"unmanaged_live_workers"`
	AvailableSlots            int                        `json:"available_slots"`
	ActiveLeases              int                        `json:"active_leases"`
	RunningJobs               int                        `json:"running_jobs"`
	UnattributedActiveLeases  int                        `json:"unattributed_active_leases"`
	UnattributedRunningJobs   int                        `json:"unattributed_running_jobs"`
	ActiveWorkersByGeneration []WorkerGenerationActivity `json:"active_workers_by_generation"`
	Quiescent                 bool                       `json:"quiescent"`
}

func buildWorkerGroupObservation(
	group string,
	runState WorkerGroupRunState,
	observedAt time.Time,
	workers []WorkerRecord,
	jobs []Job,
) WorkerGroupObservation {
	observedAt = observedAt.UTC()
	generationCounts := map[int64]int{}

	result := WorkerGroupObservation{
		Group:               group,
		RunState:            runState.State,
		RunStateRevision:    runState.Revision,
		DeadlineAt:          runState.DeadlineAt,
		ObservedAt:          observedAt,
		PressureReasonCodes: []string{},
	}
	pressureReasons := map[string]struct{}{}
	for _, job := range jobs {
		if job.State != JobRunning {
			continue
		}
		if job.LeaseIdentity == nil || strings.TrimSpace(job.LeaseIdentity.Group) == "" {
			result.UnattributedRunningJobs++
			if activeQueueLease(job, observedAt) {
				result.UnattributedActiveLeases++
			}
			continue
		}
		if strings.TrimSpace(job.LeaseIdentity.Group) != group {
			continue
		}
		result.RunningJobs++
		if activeQueueLease(job, observedAt) {
			result.ActiveLeases++
		}
	}

	for _, worker := range workers {
		if worker.Group != group || !worker.Live(observedAt) {
			continue
		}
		result.LiveWorkers++
		if worker.ResourcePressure != nil && !worker.ResourcePressure.Fresh(observedAt) {
			result.StalePressureWorkers++
		}
		if worker.AcceptingClaims() {
			result.PressureAcceptingWorkers++
		} else {
			result.PressurePausedWorkers++
			pressureReasons[worker.ResourcePressure.ReasonCode] = struct{}{}
		}
		generationCounts[worker.CredentialGeneration]++
		if worker.CredentialGeneration == 0 {
			result.UnmanagedLiveWorkers++
		}
		if runState.Draining() || worker.Status != WorkerStatusActive || !worker.AcceptingClaims() {
			continue
		}
		if worker.Slots > 0 {
			result.AvailableSlots += worker.Slots
		}
	}
	result.PressureReasonCodes = sortedSet(pressureReasons)
	if result.ActiveLeases >= result.AvailableSlots {
		result.AvailableSlots = 0
	} else {
		result.AvailableSlots -= result.ActiveLeases
	}

	generations := make([]int64, 0, len(generationCounts))
	for generation := range generationCounts {
		generations = append(generations, generation)
	}
	sort.Slice(generations, func(i, j int) bool { return generations[i] < generations[j] })
	result.ActiveWorkersByGeneration = make([]WorkerGenerationActivity, 0, len(generations))
	for _, generation := range generations {
		result.ActiveWorkersByGeneration = append(result.ActiveWorkersByGeneration, WorkerGenerationActivity{
			Generation: generation,
			Workers:    generationCounts[generation],
		})
	}

	result.Quiescent = runState.Draining() &&
		result.UnmanagedLiveWorkers == 0 &&
		result.ActiveLeases == 0 && result.RunningJobs == 0 &&
		result.UnattributedActiveLeases == 0 && result.UnattributedRunningJobs == 0
	return result
}
