package state

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) QueueDemandSnapshot(ctx context.Context, selectors []QueueDemandSelector) (QueueDemandSnapshot, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return QueueDemandSnapshot{}, err
	}
	defer tx.Rollback(ctx)

	var storeEpoch string
	var revision int64
	var observedAt time.Time
	if err := tx.QueryRow(ctx, `
SELECT store_epoch, revision, CURRENT_TIMESTAMP
FROM queue_snapshot_state
WHERE singleton = TRUE
`).Scan(&storeEpoch, &revision, &observedAt); err != nil {
		return QueueDemandSnapshot{}, err
	}

	rows, err := tx.Query(ctx, `SELECT `+jobColumns+` FROM jobs WHERE state IN ('queued', 'running')`)
	if err != nil {
		return QueueDemandSnapshot{}, err
	}
	jobs := []Job{}
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			rows.Close()
			return QueueDemandSnapshot{}, err
		}
		jobs = append(jobs, job)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return QueueDemandSnapshot{}, err
	}
	rateBuckets, err := postgresCurrentRateBuckets(ctx, tx, observedAt)
	if err != nil {
		return QueueDemandSnapshot{}, err
	}
	policies, err := postgresAllExecutionPolicies(ctx, tx)
	if err != nil {
		return QueueDemandSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return QueueDemandSnapshot{}, err
	}
	return buildQueueDemandSnapshotWithPolicies(storeEpoch, revision, observedAt, jobs, selectors, rateBuckets, policies), nil
}
