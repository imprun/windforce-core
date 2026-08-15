package state

import (
	"context"
	"encoding/json"
	"time"

	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) GetWorkerGroupInventory(
	ctx context.Context,
	workspaceID string,
	includeUnauthorized bool,
) (WorkerGroupInventory, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return WorkerGroupInventory{}, err
	}
	defer tx.Rollback(ctx)
	observedAt, workers, credentials, runStates, jobs, err := postgresPlacementObservationSnapshot(ctx, tx)
	if err != nil {
		return WorkerGroupInventory{}, err
	}
	return buildWorkerGroupInventory(
		workspaceID, includeUnauthorized, observedAt, workers, credentials, runStates, jobs,
	)
}

func (s *PostgresStore) GetPlacementCandidates(
	ctx context.Context,
	workspaceID string,
	app string,
	action string,
	includeUnauthorized bool,
) (PlacementCandidates, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return PlacementCandidates{}, err
	}
	defer tx.Rollback(ctx)
	deployment, err := postgresDeploymentForObservation(ctx, tx, workspaceID, app)
	if err != nil {
		return PlacementCandidates{}, err
	}
	policy, err := postgresRoutingPolicy(ctx, tx, workspaceID, app, false)
	if err != nil {
		return PlacementCandidates{}, err
	}
	deployment = catalog.ApplyRoutingPolicy(deployment, policy)
	observedAt, workers, credentials, runStates, jobs, err := postgresPlacementObservationSnapshot(ctx, tx)
	if err != nil {
		return PlacementCandidates{}, err
	}
	return buildPlacementCandidates(
		workspaceID, action, includeUnauthorized, observedAt, deployment, workers, credentials, runStates, jobs,
	)
}

func (s *PostgresStore) GetExecutionDemand(
	ctx context.Context,
	workspaceID string,
	app string,
	action string,
	includeUnauthorized bool,
) (ExecutionDemand, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ExecutionDemand{}, err
	}
	defer tx.Rollback(ctx)
	observedAt, workers, credentials, runStates, jobs, err := postgresExecutionDemandObservationSnapshot(ctx, tx)
	if err != nil {
		return ExecutionDemand{}, err
	}
	return buildExecutionDemand(
		workspaceID, app, action, includeUnauthorized, observedAt, workers, credentials, runStates, jobs,
	)
}

func postgresPlacementObservationSnapshot(
	ctx context.Context,
	tx pgx.Tx,
) (time.Time, []WorkerRecord, map[string]WorkerCredential, map[string]WorkerGroupRunState, []Job, error) {
	observedAt, workers, credentials, runStates, err := postgresPlacementCapacitySnapshot(ctx, tx)
	if err != nil {
		return time.Time{}, nil, nil, nil, nil, err
	}
	jobs, err := postgresPlacementJobs(ctx, tx)
	if err != nil {
		return time.Time{}, nil, nil, nil, nil, err
	}
	return observedAt, workers, credentials, runStates, jobs, nil
}

func postgresExecutionDemandObservationSnapshot(
	ctx context.Context,
	tx pgx.Tx,
) (time.Time, []WorkerRecord, map[string]WorkerCredential, map[string]WorkerGroupRunState, []Job, error) {
	observedAt, workers, credentials, runStates, err := postgresPlacementCapacitySnapshot(ctx, tx)
	if err != nil {
		return time.Time{}, nil, nil, nil, nil, err
	}
	jobs, err := postgresExecutionDemandJobs(ctx, tx)
	if err != nil {
		return time.Time{}, nil, nil, nil, nil, err
	}
	return observedAt, workers, credentials, runStates, jobs, nil
}

func postgresPlacementCapacitySnapshot(
	ctx context.Context,
	tx pgx.Tx,
) (time.Time, []WorkerRecord, map[string]WorkerCredential, map[string]WorkerGroupRunState, error) {
	var observedAt time.Time
	if err := tx.QueryRow(ctx, `SELECT CURRENT_TIMESTAMP`).Scan(&observedAt); err != nil {
		return time.Time{}, nil, nil, nil, err
	}
	workers, err := postgresPlacementWorkers(ctx, tx)
	if err != nil {
		return time.Time{}, nil, nil, nil, err
	}
	credentials, err := postgresPlacementCredentials(ctx, tx)
	if err != nil {
		return time.Time{}, nil, nil, nil, err
	}
	runStates, err := postgresPlacementRunStates(ctx, tx)
	if err != nil {
		return time.Time{}, nil, nil, nil, err
	}
	return observedAt.UTC(), workers, credentials, runStates, nil
}

func postgresDeploymentForObservation(
	ctx context.Context,
	tx pgx.Tx,
	workspaceID string,
	app string,
) (contract.Deployment, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `
SELECT deployment
FROM control_active_release
WHERE workspace_id = $1 AND app_key = $2
`, contract.NormalizeWorkspace(workspaceID), app).Scan(&raw)
	if err == pgx.ErrNoRows {
		return contract.Deployment{}, catalog.ErrDeploymentNotFound
	}
	if err != nil {
		return contract.Deployment{}, err
	}
	var deployment contract.Deployment
	if err := json.Unmarshal(raw, &deployment); err != nil {
		return contract.Deployment{}, err
	}
	return deployment, nil
}

func postgresPlacementJobs(ctx context.Context, tx pgx.Tx) ([]Job, error) {
	rows, err := tx.Query(ctx, `
SELECT state, lease_owner, lease_expires_at, lease_identity
FROM jobs WHERE state=$1`, string(JobRunning))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []Job{}
	for rows.Next() {
		var job Job
		var leaseOwner *string
		var leaseIdentity json.RawMessage
		if err := rows.Scan(&job.State, &leaseOwner, &job.LeaseExpiresAt, &leaseIdentity); err != nil {
			return nil, err
		}
		if leaseOwner != nil {
			job.LeaseOwner = *leaseOwner
		}
		if len(leaseIdentity) > 0 && string(leaseIdentity) != "null" {
			var identity WorkerLeaseIdentity
			if err := json.Unmarshal(leaseIdentity, &identity); err != nil {
				return nil, err
			}
			job.LeaseIdentity = &identity
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func postgresExecutionDemandJobs(ctx context.Context, tx pgx.Tx) ([]Job, error) {
	rows, err := tx.Query(ctx, `SELECT `+jobColumns+` FROM jobs WHERE state IN ($1, $2)`, string(JobRunning), string(JobQueued))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []Job{}
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}
