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
	workerByID := make(map[string]WorkerRecord, len(workers))
	activeLeasesByWorker := make(map[string]int)
	generationCounts := map[int64]int{}

	for _, worker := range workers {
		workerByID[worker.ID] = worker
	}

	result := WorkerGroupObservation{
		Group:            group,
		RunState:         runState.State,
		RunStateRevision: runState.Revision,
		DeadlineAt:       runState.DeadlineAt,
		ObservedAt:       observedAt,
	}
	for _, job := range jobs {
		if job.State != JobRunning {
			continue
		}
		worker, attributed := workerByID[strings.TrimSpace(job.LeaseOwner)]
		if !attributed || strings.TrimSpace(worker.Group) == "" {
			result.UnattributedRunningJobs++
			if activeQueueLease(job, observedAt) {
				result.UnattributedActiveLeases++
			}
			continue
		}
		if worker.Group != group {
			continue
		}
		result.RunningJobs++
		if activeQueueLease(job, observedAt) {
			result.ActiveLeases++
			activeLeasesByWorker[worker.ID]++
		}
	}

	for _, worker := range workers {
		if worker.Group != group || !worker.Live(observedAt) {
			continue
		}
		result.LiveWorkers++
		generationCounts[worker.CredentialGeneration]++
		if worker.CredentialGeneration == 0 {
			result.UnmanagedLiveWorkers++
		}
		if runState.Draining() || worker.Status != WorkerStatusActive {
			continue
		}
		available := worker.Slots - activeLeasesByWorker[worker.ID]
		if available > 0 {
			result.AvailableSlots += available
		}
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
