package state

import "context"

func (s *LocalStore) QueueDemandSnapshot(ctx context.Context, selectors []QueueDemandSelector) (QueueDemandSnapshot, error) {
	var result QueueDemandSnapshot
	err := s.withLock(ctx, func() error {
		snapshot, err := s.Load(ctx)
		if err != nil {
			return err
		}
		// A missing or legacy state file has no durable fence yet. Persist the
		// generated epoch once before returning it to an observer.
		if snapshot.SnapshotRevision == 0 {
			if err := s.write(snapshot); err != nil {
				return err
			}
			snapshot, err = s.Load(ctx)
			if err != nil {
				return err
			}
		}
		jobs := make([]Job, 0, len(snapshot.Jobs))
		for _, job := range snapshot.Jobs {
			jobs = append(jobs, job)
		}
		result = buildQueueDemandSnapshotWithRates(snapshot.StoreEpoch, snapshot.SnapshotRevision, currentUTC(s.leaseNow), jobs, selectors, snapshot.ExecutionRateBuckets)
		return nil
	})
	return result, err
}
