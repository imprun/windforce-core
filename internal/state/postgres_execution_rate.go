package state

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

func postgresCurrentRateTime(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT CURRENT_TIMESTAMP`).Scan(&now); err != nil {
		return time.Time{}, err
	}
	return now.UTC(), nil
}

func postgresRateLimitsReached(ctx context.Context, tx pgx.Tx, candidate Job, now time.Time) (bool, error) {
	workspaceID := normalizedJobWorkspace("", candidate)
	for _, limit := range candidate.Payload.ExecutionLimits.Rate {
		if !validKeyedRatePin(limit) {
			return true, nil
		}
		start, _ := rateWindow(now, limit.WindowSeconds)
		var bucketStart time.Time
		var consumed int32
		err := tx.QueryRow(ctx, `
SELECT window_start, consumed
FROM execution_rate_bucket
WHERE workspace_id=$1 AND scope=$2 AND policy_id=$3 AND key_digest=$4 AND window_seconds=$5
`, workspaceID, limit.Scope, limit.PolicyID, limit.KeyDigest, limit.WindowSeconds).Scan(&bucketStart, &consumed)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return false, err
		}
		if bucketStart.UTC().Equal(start) && consumed >= limit.MaxAttempts {
			return true, nil
		}
	}
	return false, nil
}

func postgresConsumeRateLimits(ctx context.Context, tx pgx.Tx, candidate Job, now time.Time) error {
	workspaceID := normalizedJobWorkspace("", candidate)
	for _, limit := range candidate.Payload.ExecutionLimits.Rate {
		start, end := rateWindow(now, limit.WindowSeconds)
		if _, err := tx.Exec(ctx, `
INSERT INTO execution_rate_bucket (
    workspace_id, scope, policy_id, key_digest, window_seconds,
    window_start, window_end, consumed, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, 1, $8)
ON CONFLICT (workspace_id, scope, policy_id, key_digest, window_seconds)
DO UPDATE SET
    window_start = EXCLUDED.window_start,
    window_end = EXCLUDED.window_end,
    consumed = CASE
        WHEN execution_rate_bucket.window_start = EXCLUDED.window_start
        THEN execution_rate_bucket.consumed + 1
        ELSE 1
    END,
    updated_at = EXCLUDED.updated_at
`, workspaceID, limit.Scope, limit.PolicyID, limit.KeyDigest, limit.WindowSeconds, start, end, now); err != nil {
			return err
		}
	}
	return nil
}

func postgresCurrentRateBuckets(ctx context.Context, tx pgx.Tx, observedAt time.Time) (map[string]ExecutionRateBucket, error) {
	rows, err := tx.Query(ctx, `
SELECT workspace_id, scope, policy_id, key_digest, window_seconds, window_start, window_end, consumed
FROM execution_rate_bucket
WHERE window_end > $1
`, observedAt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	buckets := map[string]ExecutionRateBucket{}
	for rows.Next() {
		var workspaceID string
		var limit KeyedRateLimitPin
		var bucket ExecutionRateBucket
		if err := rows.Scan(
			&workspaceID, &limit.Scope, &limit.PolicyID, &limit.KeyDigest, &limit.WindowSeconds,
			&bucket.WindowStart, &bucket.WindowEnd, &bucket.Consumed,
		); err != nil {
			return nil, err
		}
		buckets[rateBucketKey(workspaceID, limit)] = bucket
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return buckets, nil
}
