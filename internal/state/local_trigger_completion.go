package state

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *LocalStore) ClaimTriggerCompletion(ctx context.Context, workerID string, leaseTTL time.Duration) (*TriggerCompletionClaim, error) {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return nil, ErrInvalidLease
	}
	if leaseTTL <= 0 {
		leaseTTL = defaultLeaseTime
	}

	var selected TriggerDelivery
	err := s.updateLease(ctx, func(snapshot *Snapshot, now time.Time) error {
		keys := make([]string, 0, len(snapshot.TriggerDeliveries))
		for key := range snapshot.TriggerDeliveries {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		found := false
		for _, key := range keys {
			delivery := snapshot.TriggerDeliveries[key]
			run, ok := snapshot.Runs[delivery.RunID]
			if delivery.State != "admitted" || !ok || !IsTerminal(run) {
				continue
			}
			switch delivery.CompletionState {
			case TriggerCompletionWaiting:
				switch delivery.Completion.Mode {
				case TriggerCompletionModeNone:
					delivery.CompletionState = TriggerCompletionIgnored
					delivery.CompletionCompletedAt = cloneTime(&now)
				case TriggerCompletionModePoll:
					delivery.CompletionState = TriggerCompletionAvailable
					delivery.CompletionCompletedAt = cloneTime(&now)
				case TriggerCompletionModeCallback, TriggerCompletionModePublish:
					delivery.CompletionState = TriggerCompletionPending
					delivery.CompletionNextAttemptAt = now
				}
				delivery.UpdatedAt = now
				snapshot.TriggerDeliveries[key] = delivery
			}
			if !triggerCompletionEligible(delivery, now) {
				continue
			}
			if !found ||
				delivery.CompletionNextAttemptAt.Before(selected.CompletionNextAttemptAt) ||
				(delivery.CompletionNextAttemptAt.Equal(selected.CompletionNextAttemptAt) && delivery.CreatedAt.Before(selected.CreatedAt)) {
				selected = delivery
				found = true
			}
		}
		if !found {
			return nil
		}

		expiresAt := now.Add(leaseTTL)
		selected.CompletionState = TriggerCompletionDelivering
		selected.CompletionAttempt++
		selected.CompletionLeaseOwner = workerID
		selected.CompletionLeaseExpiresAt = cloneTime(&expiresAt)
		selected.UpdatedAt = now
		snapshot.TriggerDeliveries[triggerDeliveryKey(selected.WorkspaceID, selected.TriggerID, selected.DeliveryID)] = selected
		return nil
	})
	if err != nil {
		return nil, err
	}
	if selected.ID == "" {
		return nil, ErrNoCompletion
	}

	run, err := s.GetRun(ctx, selected.RunID)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	record, ok := snapshot.Triggers[triggerKey(selected.WorkspaceID, selected.TriggerID)]
	if !ok {
		return nil, fmt.Errorf("%w: trigger %q", ErrNotFound, selected.TriggerID)
	}
	definition, err := s.triggerFromRecord(ctx, record)
	if err != nil {
		return nil, err
	}
	return &TriggerCompletionClaim{
		Delivery: cloneTriggerDelivery(selected),
		Run:      run,
		Trigger:  definition,
		Lease: TriggerCompletionLease{
			DeliveryID: selected.ID,
			WorkerID:   workerID,
			Attempt:    selected.CompletionAttempt,
			ExpiresAt:  *selected.CompletionLeaseExpiresAt,
		},
	}, nil
}

func (s *LocalStore) CompleteTriggerCompletion(ctx context.Context, lease TriggerCompletionLease, result TriggerCompletionResult) error {
	if err := validateTriggerCompletionResult(result); err != nil {
		return err
	}
	return s.updateLease(ctx, func(snapshot *Snapshot, now time.Time) error {
		key, delivery, ok := findTriggerDeliveryByID(snapshot, lease.DeliveryID)
		if !ok {
			return fmt.Errorf("%w: trigger delivery %q", ErrNotFound, lease.DeliveryID)
		}
		if err := validateTriggerCompletionLease(delivery, lease, now); err != nil {
			return err
		}
		delivery.CompletionState = result.State
		delivery.CompletionNextAttemptAt = result.NextAttemptAt
		delivery.CompletionResponseStatus = cloneInt(result.ResponseStatus)
		delivery.CompletionErrorSummary = truncateTriggerError(result.ErrorSummary)
		delivery.CompletionLeaseOwner = ""
		delivery.CompletionLeaseExpiresAt = nil
		delivery.UpdatedAt = now
		if result.State == TriggerCompletionRetrying {
			delivery.CompletionCompletedAt = nil
		} else if result.CompletedAt != nil {
			delivery.CompletionCompletedAt = cloneTime(result.CompletedAt)
		} else {
			delivery.CompletionCompletedAt = cloneTime(&now)
		}
		snapshot.TriggerDeliveries[key] = delivery
		return nil
	})
}

func triggerCompletionEligible(delivery TriggerDelivery, now time.Time) bool {
	switch delivery.CompletionState {
	case TriggerCompletionPending, TriggerCompletionRetrying:
		return delivery.CompletionNextAttemptAt.IsZero() || !delivery.CompletionNextAttemptAt.After(now)
	case TriggerCompletionDelivering:
		return delivery.CompletionLeaseExpiresAt != nil && !delivery.CompletionLeaseExpiresAt.After(now)
	default:
		return false
	}
}

func validateTriggerCompletionLease(delivery TriggerDelivery, lease TriggerCompletionLease, now time.Time) error {
	if delivery.CompletionState != TriggerCompletionDelivering ||
		delivery.CompletionLeaseOwner != lease.WorkerID ||
		delivery.CompletionAttempt != lease.Attempt ||
		delivery.CompletionLeaseExpiresAt == nil ||
		!delivery.CompletionLeaseExpiresAt.Equal(lease.ExpiresAt) ||
		!delivery.CompletionLeaseExpiresAt.After(now) {
		return ErrInvalidLease
	}
	return nil
}

func findTriggerDeliveryByID(snapshot *Snapshot, id string) (string, TriggerDelivery, bool) {
	for key, delivery := range snapshot.TriggerDeliveries {
		if delivery.ID == id {
			return key, delivery, true
		}
	}
	return "", TriggerDelivery{}, false
}

func cloneTriggerDelivery(delivery TriggerDelivery) TriggerDelivery {
	delivery.CompletionLeaseExpiresAt = cloneTime(delivery.CompletionLeaseExpiresAt)
	delivery.CompletionResponseStatus = cloneInt(delivery.CompletionResponseStatus)
	delivery.CompletionCompletedAt = cloneTime(delivery.CompletionCompletedAt)
	if delivery.Completion.Callback != nil {
		callback := *delivery.Completion.Callback
		delivery.Completion.Callback = &callback
	}
	if delivery.Completion.Publish != nil {
		publish := *delivery.Completion.Publish
		delivery.Completion.Publish = &publish
	}
	return delivery
}
