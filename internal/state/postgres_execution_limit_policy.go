package state

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/imprun/windforce-core/internal/contract"
)

const executionLimitPolicyColumns = `
	workspace_id, app_key, action_key, scope, policy_id, kind,
	shape_fingerprint, allowance, window_seconds, revision, deleted,
	operation_id, request_fingerprint, updated_by, updated_at
`

func (s *PostgresStore) ListExecutionLimitPolicies(ctx context.Context, workspaceID string, appKey string) ([]ExecutionLimitPolicy, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	appKey = strings.TrimSpace(appKey)
	rows, err := s.pool.Query(ctx, `
SELECT `+executionLimitPolicyColumns+`
FROM execution_limit_policy
WHERE workspace_id=$1 AND ($2='' OR app_key=$2) AND deleted=false
ORDER BY scope, action_key, kind, policy_id
`, workspaceID, appKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	policies := make([]ExecutionLimitPolicy, 0)
	for rows.Next() {
		policy, scanErr := scanExecutionLimitPolicy(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		policies = append(policies, policy)
	}
	return policies, rows.Err()
}

func (s *PostgresStore) GetExecutionLimitPolicy(ctx context.Context, key ExecutionLimitPolicyKey) (ExecutionLimitPolicy, error) {
	key, err := NormalizeExecutionLimitPolicyKey(key)
	if err != nil {
		return ExecutionLimitPolicy{}, err
	}
	policy, err := scanExecutionLimitPolicy(s.pool.QueryRow(ctx, `
SELECT `+executionLimitPolicyColumns+`
FROM execution_limit_policy
WHERE workspace_id=$1 AND app_key=$2 AND action_key=$3 AND scope=$4 AND policy_id=$5 AND kind=$6 AND deleted=false
`, key.WorkspaceID, key.AppKey, key.ActionKey, key.Scope, key.PolicyID, key.Kind))
	if errors.Is(err, pgx.ErrNoRows) {
		return ExecutionLimitPolicy{}, ErrNotFound
	}
	return policy, err
}

func (s *PostgresStore) MutateExecutionLimitPolicy(ctx context.Context, request MutateExecutionLimitPolicyRequest) (ExecutionLimitPolicy, bool, error) {
	results, err := s.MutateExecutionLimitPolicies(ctx, []MutateExecutionLimitPolicyRequest{request})
	if err != nil {
		return ExecutionLimitPolicy{}, false, err
	}
	return results[0].Policy, results[0].Replayed, nil
}

func (s *PostgresStore) MutateExecutionLimitPolicies(ctx context.Context, requests []MutateExecutionLimitPolicyRequest) ([]ExecutionLimitPolicyMutationResult, error) {
	normalized, err := normalizeExecutionLimitPolicyBatch(requests)
	if err != nil {
		return nil, err
	}
	results := make([]ExecutionLimitPolicyMutationResult, len(normalized))
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		lockKeys := make([]string, 0, len(normalized))
		byLockKey := make(map[string]ExecutionLimitPolicyKey, len(normalized))
		for _, request := range normalized {
			key := request.Policy.ExecutionLimitPolicyKey
			lockKey := key.WorkspaceID + "\x1f" + executionLimitPolicyLockKey(key)
			lockKeys = append(lockKeys, lockKey)
			byLockKey[lockKey] = key
		}
		sort.Strings(lockKeys)
		for _, lockKey := range lockKeys {
			key := byLockKey[lockKey]
			if _, lockErr := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))`, key.WorkspaceID, executionLimitPolicyLockKey(key)); lockErr != nil {
				return lockErr
			}
		}
		for index, request := range normalized {
			policy, replayed, applyErr := postgresMutateExecutionLimitPolicyInTx(ctx, tx, request)
			if applyErr != nil {
				return applyErr
			}
			results[index] = ExecutionLimitPolicyMutationResult{Policy: policy, Replayed: replayed}
		}
		return nil
	})
	return results, err
}

func postgresMutateExecutionLimitPolicyInTx(ctx context.Context, tx pgx.Tx, request MutateExecutionLimitPolicyRequest) (ExecutionLimitPolicy, bool, error) {
	key := request.Policy.ExecutionLimitPolicyKey
	current, getErr := scanExecutionLimitPolicy(tx.QueryRow(ctx, `
SELECT `+executionLimitPolicyColumns+`
FROM execution_limit_policy
WHERE workspace_id=$1 AND app_key=$2 AND action_key=$3 AND scope=$4 AND policy_id=$5 AND kind=$6
FOR UPDATE
`, key.WorkspaceID, key.AppKey, key.ActionKey, key.Scope, key.PolicyID, key.Kind))
	exists := getErr == nil
	if getErr != nil && !errors.Is(getErr, pgx.ErrNoRows) {
		return ExecutionLimitPolicy{}, false, getErr
	}
	if exists && current.LastOperationID == request.OperationID {
		if current.LastRequestFingerprint != request.RequestFingerprint {
			return ExecutionLimitPolicy{}, false, ErrConflict
		}
		return current, true, nil
	}
	if (!exists && request.ExpectedRevision != 0) || (exists && current.Revision != request.ExpectedRevision) {
		return ExecutionLimitPolicy{}, false, ErrConflict
	}
	if exists && !current.Deleted && current.ShapeFingerprint != request.Policy.ShapeFingerprint {
		return ExecutionLimitPolicy{}, false, ErrConflict
	}
	previousAllowance := (*int32)(nil)
	if exists && !current.Deleted {
		previousAllowance = int32Value(current.Allowance)
	}
	result := request.Policy
	result.Revision = request.ExpectedRevision + 1
	result.Deleted = request.Delete
	result.LastOperationID = request.OperationID
	result.LastRequestFingerprint = request.RequestFingerprint
	result.UpdatedBy = request.Actor
	result, err := scanExecutionLimitPolicy(tx.QueryRow(ctx, `
INSERT INTO execution_limit_policy (
	workspace_id, app_key, action_key, scope, policy_id, kind,
	shape_fingerprint, allowance, window_seconds, revision, deleted,
	operation_id, request_fingerprint, updated_by, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,now())
ON CONFLICT (workspace_id, app_key, action_key, scope, policy_id, kind)
DO UPDATE SET shape_fingerprint=EXCLUDED.shape_fingerprint, allowance=EXCLUDED.allowance,
	window_seconds=EXCLUDED.window_seconds, revision=EXCLUDED.revision, deleted=EXCLUDED.deleted,
	operation_id=EXCLUDED.operation_id, request_fingerprint=EXCLUDED.request_fingerprint,
	updated_by=EXCLUDED.updated_by, updated_at=EXCLUDED.updated_at
RETURNING `+executionLimitPolicyColumns+`
`, result.WorkspaceID, result.AppKey, result.ActionKey, result.Scope, result.PolicyID, result.Kind,
		result.ShapeFingerprint, result.Allowance, result.WindowSeconds, result.Revision, result.Deleted,
		result.LastOperationID, result.LastRequestFingerprint, result.UpdatedBy))
	if err != nil {
		return ExecutionLimitPolicy{}, false, err
	}
	eventKind := "updated"
	allowance := int32Value(result.Allowance)
	if request.Delete {
		eventKind = "deleted"
		allowance = nil
	} else if !exists || current.Deleted {
		eventKind = "created"
	}
	_, err = tx.Exec(ctx, `
INSERT INTO execution_limit_policy_audit (
	id, workspace_id, app_key, action_key, scope, policy_id, kind, event_kind,
	shape_fingerprint, previous_allowance, allowance, revision, operation_id, reason, actor, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,now())
`, NewID("audit"), result.WorkspaceID, result.AppKey, result.ActionKey, result.Scope, result.PolicyID, result.Kind,
		eventKind, result.ShapeFingerprint, previousAllowance, allowance, result.Revision, request.OperationID, request.Reason, request.Actor)
	if err != nil {
		return ExecutionLimitPolicy{}, false, err
	}
	return result, false, nil
}

func (s *PostgresStore) ListExecutionLimitPolicyAudit(ctx context.Context, workspaceID string, appKey string) ([]ExecutionLimitPolicyAudit, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	appKey = strings.TrimSpace(appKey)
	rows, err := s.pool.Query(ctx, `
SELECT id, workspace_id, app_key, action_key, scope, policy_id, kind, event_kind,
	shape_fingerprint, previous_allowance, allowance, revision, operation_id, reason, actor, created_at
FROM execution_limit_policy_audit
WHERE workspace_id=$1 AND ($2='' OR app_key=$2)
ORDER BY created_at, id
`, workspaceID, appKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	audits := make([]ExecutionLimitPolicyAudit, 0)
	for rows.Next() {
		var audit ExecutionLimitPolicyAudit
		if err := rows.Scan(&audit.ID, &audit.WorkspaceID, &audit.AppKey, &audit.ActionKey, &audit.Scope, &audit.PolicyID, &audit.ExecutionLimitPolicyKey.Kind,
			&audit.EventKind, &audit.ShapeFingerprint, &audit.PreviousAllowance, &audit.Allowance, &audit.Revision,
			&audit.OperationID, &audit.Reason, &audit.Actor, &audit.CreatedAt); err != nil {
			return nil, err
		}
		audits = append(audits, audit)
	}
	sort.SliceStable(audits, func(i, j int) bool { return audits[i].CreatedAt.Before(audits[j].CreatedAt) })
	return audits, rows.Err()
}

type executionLimitPolicyRow interface {
	Scan(dest ...any) error
}

func scanExecutionLimitPolicy(row executionLimitPolicyRow) (ExecutionLimitPolicy, error) {
	var policy ExecutionLimitPolicy
	err := row.Scan(&policy.WorkspaceID, &policy.AppKey, &policy.ActionKey, &policy.Scope, &policy.PolicyID, &policy.Kind,
		&policy.ShapeFingerprint, &policy.Allowance, &policy.WindowSeconds, &policy.Revision, &policy.Deleted,
		&policy.LastOperationID, &policy.LastRequestFingerprint, &policy.UpdatedBy, &policy.UpdatedAt)
	return policy, err
}
