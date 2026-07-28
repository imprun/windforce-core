package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) ClaimTriggerCompletion(ctx context.Context, workerID string, leaseTTL time.Duration) (*TriggerCompletionClaim, error) {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return nil, ErrInvalidLease
	}
	if leaseTTL <= 0 {
		leaseTTL = defaultLeaseTime
	}

	var claimed *TriggerCompletionClaim
	var encrypted json.RawMessage
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		now := currentUTC(s.leaseNow)
		if _, err := tx.Exec(ctx, `
UPDATE trigger_delivery d
SET completion_state = CASE d.completion->>'mode'
        WHEN 'none' THEN $1
        WHEN 'poll' THEN $2
        ELSE $3
    END,
    completion_next_attempt_at = CASE
        WHEN d.completion->>'mode' IN ('callback', 'publish') THEN $4
        ELSE d.completion_next_attempt_at
    END,
    completion_completed_at = CASE
        WHEN d.completion->>'mode' IN ('none', 'poll') THEN $4
        ELSE NULL
    END,
    updated_at = $4
FROM runs r
WHERE d.run_id = r.id
  AND d.state = 'admitted'
  AND d.completion_state = $5
  AND r.state IN ('SUCCEEDED', 'FAILED', 'CANCELED', 'EXPIRED')
`, TriggerCompletionIgnored, TriggerCompletionAvailable, TriggerCompletionPending, now, TriggerCompletionWaiting); err != nil {
			return err
		}

		var id string
		err := tx.QueryRow(ctx, `
SELECT d.id
FROM trigger_delivery d
JOIN runs r ON r.id = d.run_id
WHERE d.state = 'admitted'
  AND r.state IN ('SUCCEEDED', 'FAILED', 'CANCELED', 'EXPIRED')
  AND (
      (d.completion_state IN ($1, $2) AND COALESCE(d.completion_next_attempt_at, d.created_at) <= $3)
      OR (d.completion_state = $4 AND d.completion_lease_expires_at <= $3)
  )
ORDER BY COALESCE(d.completion_next_attempt_at, d.created_at), d.created_at, d.id
FOR UPDATE OF d SKIP LOCKED
LIMIT 1
		`, TriggerCompletionPending, TriggerCompletionRetrying, now, TriggerCompletionDelivering).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}

		delivery, err := scanTriggerDelivery(tx.QueryRow(ctx, `
SELECT `+triggerDeliveryColumns+`
FROM trigger_delivery
WHERE id=$1
`, id))
		if err != nil {
			return err
		}
		trigger, encryptedValue, err := scanTriggerRecord(tx.QueryRow(ctx, `
SELECT `+triggerColumns+`
FROM trigger_definition
WHERE workspace_id=$1 AND id=$2
`, delivery.WorkspaceID, delivery.TriggerID))
		if err != nil {
			return err
		}
		expiresAt := now.Add(leaseTTL).Truncate(time.Microsecond)
		delivery.CompletionState = TriggerCompletionDelivering
		delivery.CompletionAttempt++
		delivery.CompletionLeaseOwner = workerID
		delivery.CompletionLeaseExpiresAt = cloneTime(&expiresAt)
		delivery.UpdatedAt = now
		if _, err := tx.Exec(ctx, `
UPDATE trigger_delivery
SET completion_state=$2,
    completion_attempt=$3,
    completion_lease_owner=$4,
    completion_lease_expires_at=$5,
    updated_at=$6
WHERE id=$1
`, delivery.ID, delivery.CompletionState, delivery.CompletionAttempt, workerID, expiresAt, now); err != nil {
			return err
		}
		encrypted = encryptedValue
		claimed = &TriggerCompletionClaim{
			Delivery: delivery,
			Trigger:  trigger,
			Lease: TriggerCompletionLease{
				DeliveryID: delivery.ID,
				WorkerID:   workerID,
				Attempt:    delivery.CompletionAttempt,
				ExpiresAt:  expiresAt,
			},
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if claimed == nil {
		return nil, ErrNoCompletion
	}

	claimed.Trigger.SecretConfig, err = s.decryptInput(ctx, claimed.Trigger.WorkspaceID, encrypted)
	if err != nil {
		return nil, err
	}
	claimed.Run, err = s.GetRun(ctx, claimed.Delivery.RunID)
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func (s *PostgresStore) CompleteTriggerCompletion(ctx context.Context, lease TriggerCompletionLease, result TriggerCompletionResult) error {
	if err := validateTriggerCompletionResult(result); err != nil {
		return err
	}
	return s.withTx(ctx, func(tx pgx.Tx) error {
		delivery, err := scanTriggerDelivery(tx.QueryRow(ctx, `
SELECT `+triggerDeliveryColumns+`
FROM trigger_delivery
WHERE id=$1
FOR UPDATE
`, lease.DeliveryID))
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: trigger delivery %q", ErrNotFound, lease.DeliveryID)
		}
		if err != nil {
			return err
		}
		now := currentUTC(s.leaseNow)
		if err := validateTriggerCompletionLease(delivery, lease, now); err != nil {
			return err
		}
		completedAt := result.CompletedAt
		if result.State == TriggerCompletionRetrying {
			completedAt = nil
		} else if completedAt == nil {
			completedAt = cloneTime(&now)
		}
		_, err = tx.Exec(ctx, `
UPDATE trigger_delivery
SET completion_state=$2,
    completion_next_attempt_at=$3,
    completion_lease_owner=NULL,
    completion_lease_expires_at=NULL,
    completion_response_status=$4,
    completion_error_summary=$5,
    completion_completed_at=$6,
    updated_at=$7
WHERE id=$1
`, lease.DeliveryID, result.State, optionalTime(result.NextAttemptAt), result.ResponseStatus,
			truncateTriggerError(result.ErrorSummary), completedAt, now)
		return err
	})
}
