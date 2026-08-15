package state

import (
	"context"

	"github.com/imprun/windforce-core/internal/catalog"
)

func (s *LocalStore) GetWorkerGroupInventory(
	ctx context.Context,
	workspaceID string,
	includeUnauthorized bool,
) (WorkerGroupInventory, error) {
	var result WorkerGroupInventory
	err := s.withLock(ctx, func() error {
		snapshot, err := s.Load(ctx)
		if err != nil {
			return err
		}
		result, err = buildWorkerGroupInventory(
			workspaceID,
			includeUnauthorized,
			currentUTC(s.leaseNow),
			workerRecords(snapshot.Workers),
			snapshot.WorkerCredentials,
			snapshot.WorkerGroupRunStates,
			runningPlacementJobs(snapshot),
		)
		return err
	})
	return result, err
}

func (s *LocalStore) GetPlacementCandidates(
	ctx context.Context,
	workspaceID string,
	app string,
	action string,
	includeUnauthorized bool,
) (PlacementCandidates, error) {
	var result PlacementCandidates
	err := s.withLock(ctx, func() error {
		snapshot, err := s.Load(ctx)
		if err != nil {
			return err
		}
		key := catalog.DeploymentKey(workspaceID, app)
		deployment, ok := snapshot.ReleaseCatalog.Deployments[key]
		if !ok {
			return catalog.ErrDeploymentNotFound
		}
		if policy, ok := snapshot.ReleaseCatalog.RoutingPolicies[key]; ok {
			deployment = catalog.ApplyRoutingPolicy(deployment, policy)
		}
		result, err = buildPlacementCandidates(
			workspaceID,
			action,
			includeUnauthorized,
			currentUTC(s.leaseNow),
			deployment,
			workerRecords(snapshot.Workers),
			snapshot.WorkerCredentials,
			snapshot.WorkerGroupRunStates,
			runningPlacementJobs(snapshot),
		)
		return err
	})
	return result, err
}

func (s *LocalStore) GetExecutionDemand(
	ctx context.Context,
	workspaceID string,
	app string,
	action string,
	includeUnauthorized bool,
) (ExecutionDemand, error) {
	var result ExecutionDemand
	err := s.withLock(ctx, func() error {
		snapshot, err := s.Load(ctx)
		if err != nil {
			return err
		}
		result, err = buildExecutionDemand(
			workspaceID,
			app,
			action,
			includeUnauthorized,
			currentUTC(s.leaseNow),
			workerRecords(snapshot.Workers),
			snapshot.WorkerCredentials,
			snapshot.WorkerGroupRunStates,
			executionDemandJobs(snapshot),
		)
		return err
	})
	return result, err
}

func runningPlacementJobs(snapshot Snapshot) []Job {
	jobs := make([]Job, 0)
	for _, job := range snapshot.Jobs {
		if job.State == JobRunning {
			jobs = append(jobs, job)
		}
	}
	return jobs
}

func executionDemandJobs(snapshot Snapshot) []Job {
	jobs := make([]Job, 0)
	for _, job := range snapshot.Jobs {
		if job.State == JobRunning || job.State == JobQueued {
			jobs = append(jobs, job)
		}
	}
	return jobs
}
