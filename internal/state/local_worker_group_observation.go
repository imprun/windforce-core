package state

import "context"

func (s *LocalStore) GetWorkerGroupObservation(ctx context.Context, group string) (WorkerGroupObservation, error) {
	group, err := NormalizeWorkerGroup(group)
	if err != nil {
		return WorkerGroupObservation{}, err
	}

	var result WorkerGroupObservation
	err = s.withLock(ctx, func() error {
		snapshot, err := s.Load(ctx)
		if err != nil {
			return err
		}
		runState := DefaultWorkerGroupRunState(group)
		if current, ok := snapshot.WorkerGroupRunStates[group]; ok {
			runState = current
		}
		workers := make([]WorkerRecord, 0, len(snapshot.Workers))
		for _, worker := range snapshot.Workers {
			workers = append(workers, worker)
		}
		jobs := make([]Job, 0, len(snapshot.Jobs))
		for _, job := range snapshot.Jobs {
			if job.State == JobRunning {
				jobs = append(jobs, job)
			}
		}
		result = buildWorkerGroupObservation(group, runState, currentUTC(s.leaseNow), workers, jobs)
		return nil
	})
	return result, err
}
