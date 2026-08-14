package state

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) GetWorkerGroupObservation(ctx context.Context, group string) (WorkerGroupObservation, error) {
	group, err := NormalizeWorkerGroup(group)
	if err != nil {
		return WorkerGroupObservation{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return WorkerGroupObservation{}, err
	}
	defer tx.Rollback(ctx)

	var observedAt time.Time
	if err := tx.QueryRow(ctx, `SELECT CURRENT_TIMESTAMP`).Scan(&observedAt); err != nil {
		return WorkerGroupObservation{}, err
	}
	runState := DefaultWorkerGroupRunState(group)
	err = tx.QueryRow(ctx, `
SELECT worker_group, state, revision, deadline_at
FROM worker_group_run_state WHERE worker_group=$1`, group).Scan(
		&runState.Group, &runState.State, &runState.Revision, &runState.DeadlineAt,
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return WorkerGroupObservation{}, err
	}

	workerRows, err := tx.Query(ctx, `
SELECT id, COALESCE(worker_group, ''), slots, status, credential_generation, last_heartbeat_at
FROM worker_registry`)
	if err != nil {
		return WorkerGroupObservation{}, err
	}
	workers := []WorkerRecord{}
	for workerRows.Next() {
		var worker WorkerRecord
		if err := workerRows.Scan(
			&worker.ID, &worker.Group, &worker.Slots, &worker.Status,
			&worker.CredentialGeneration, &worker.LastHeartbeatAt,
		); err != nil {
			workerRows.Close()
			return WorkerGroupObservation{}, err
		}
		workers = append(workers, worker)
	}
	workerRows.Close()
	if err := workerRows.Err(); err != nil {
		return WorkerGroupObservation{}, err
	}

	jobRows, err := tx.Query(ctx, `
SELECT state, lease_expires_at, lease_identity
FROM jobs WHERE state='running'`)
	if err != nil {
		return WorkerGroupObservation{}, err
	}
	jobs := []Job{}
	for jobRows.Next() {
		var job Job
		var leaseIdentity json.RawMessage
		if err := jobRows.Scan(&job.State, &job.LeaseExpiresAt, &leaseIdentity); err != nil {
			jobRows.Close()
			return WorkerGroupObservation{}, err
		}
		if len(leaseIdentity) > 0 && string(leaseIdentity) != "null" {
			var identity WorkerLeaseIdentity
			if err := json.Unmarshal(leaseIdentity, &identity); err != nil {
				jobRows.Close()
				return WorkerGroupObservation{}, err
			}
			job.LeaseIdentity = &identity
		}
		jobs = append(jobs, job)
	}
	jobRows.Close()
	if err := jobRows.Err(); err != nil {
		return WorkerGroupObservation{}, err
	}
	return buildWorkerGroupObservation(group, runState, observedAt, workers, jobs), nil
}
