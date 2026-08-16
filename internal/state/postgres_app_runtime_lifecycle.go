package state

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/imprun/windforce-core/internal/contract"
)

func (s *PostgresStore) GetAppRuntimeLifecycle(ctx context.Context, workspaceID, appKey string) (AppRuntimeLifecycle, error) {
	workspaceID, appKey = contract.NormalizeWorkspace(workspaceID), strings.TrimSpace(appKey)
	var result AppRuntimeLifecycle
	err := s.pool.QueryRow(ctx, `
SELECT workspace_id, app_key, state, reason, actor, revision, updated_at
FROM app_runtime_lifecycle WHERE workspace_id=$1 AND app_key=$2
`, workspaceID, appKey).Scan(&result.WorkspaceID, &result.AppKey, &result.State, &result.Reason, &result.Actor, &result.Revision, &result.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return activeAppRuntimeLifecycle(workspaceID, appKey), nil
	}
	return result, err
}

func (s *PostgresStore) ListAppRuntimeLifecycles(ctx context.Context, workspaceID string) ([]AppRuntimeLifecycle, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	rows, err := s.pool.Query(ctx, `
SELECT workspace_id, app_key, state, reason, actor, revision, updated_at
FROM app_runtime_lifecycle WHERE workspace_id=$1 ORDER BY app_key
`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]AppRuntimeLifecycle, 0)
	for rows.Next() {
		var lifecycle AppRuntimeLifecycle
		if err := rows.Scan(&lifecycle.WorkspaceID, &lifecycle.AppKey, &lifecycle.State, &lifecycle.Reason, &lifecycle.Actor, &lifecycle.Revision, &lifecycle.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, lifecycle)
	}
	return result, rows.Err()
}

func (s *PostgresStore) SetAppRuntimeLifecycle(ctx context.Context, request SetAppRuntimeLifecycleRequest) (AppRuntimeLifecycle, error) {
	request.WorkspaceID, request.AppKey = contract.NormalizeWorkspace(request.WorkspaceID), strings.TrimSpace(request.AppKey)
	request.Actor, request.Reason = strings.TrimSpace(request.Actor), strings.TrimSpace(request.Reason)
	if request.AppKey == "" || request.Actor == "" {
		return AppRuntimeLifecycle{}, fmt.Errorf("%w: App key and actor are required", ErrInvalidState)
	}
	if request.State != AppRuntimeActive && request.State != AppRuntimeTombstoned && request.State != AppRuntimeRevoked {
		return AppRuntimeLifecycle{}, fmt.Errorf("%w: invalid App runtime state", ErrInvalidState)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AppRuntimeLifecycle{}, err
	}
	defer tx.Rollback(ctx)
	currentRevision := int64(0)
	err = tx.QueryRow(ctx, `SELECT revision FROM app_runtime_lifecycle WHERE workspace_id=$1 AND app_key=$2 FOR UPDATE`, request.WorkspaceID, request.AppKey).Scan(&currentRevision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return AppRuntimeLifecycle{}, err
	}
	if request.ExpectedRevision != nil && currentRevision != *request.ExpectedRevision {
		return AppRuntimeLifecycle{}, runtimeConfigRevisionError(currentRevision)
	}
	revision := currentRevision + 1
	var result AppRuntimeLifecycle
	err = tx.QueryRow(ctx, `
INSERT INTO app_runtime_lifecycle (workspace_id, app_key, state, reason, actor, revision, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,now())
ON CONFLICT (workspace_id, app_key) DO UPDATE SET
 state=EXCLUDED.state, reason=EXCLUDED.reason, actor=EXCLUDED.actor, revision=EXCLUDED.revision, updated_at=EXCLUDED.updated_at
RETURNING workspace_id, app_key, state, reason, actor, revision, updated_at
`, request.WorkspaceID, request.AppKey, request.State, request.Reason, request.Actor, revision).Scan(
		&result.WorkspaceID, &result.AppKey, &result.State, &result.Reason, &result.Actor, &result.Revision, &result.UpdatedAt)
	if err != nil {
		return AppRuntimeLifecycle{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO app_runtime_lifecycle_audit
(id,workspace_id,app_key,state,reason,actor,revision,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,now())`,
		NewID("audit"), result.WorkspaceID, result.AppKey, result.State, result.Reason, result.Actor, result.Revision); err != nil {
		return AppRuntimeLifecycle{}, err
	}
	if request.State == AppRuntimeRevoked {
		reason := request.Reason
		if reason == "" {
			reason = "App runtime access was emergency revoked"
		}
		if _, err = tx.Exec(ctx, `UPDATE jobs SET canceled_by=$3, canceled_reason=$4, updated_at=now()
WHERE state='running' AND COALESCE(NULLIF(payload->>'workspace',''), NULLIF(payload->'deployment'->>'workspace',''), 'default')=$1
AND COALESCE(NULLIF(payload->>'app',''), NULLIF(payload->'deployment'->>'app',''), '')=$2`,
			request.WorkspaceID, request.AppKey, request.Actor, reason); err != nil {
			return AppRuntimeLifecycle{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return AppRuntimeLifecycle{}, err
	}
	return result, nil
}

func (s *PostgresStore) PurgeAppRuntimeConfig(ctx context.Context, request PurgeAppRuntimeConfigRequest) error {
	request.WorkspaceID, request.AppKey = contract.NormalizeWorkspace(request.WorkspaceID), strings.TrimSpace(request.AppKey)
	request.Actor, request.Reason = strings.TrimSpace(request.Actor), strings.TrimSpace(request.Reason)
	if request.AppKey == "" || request.Actor == "" {
		return fmt.Errorf("%w: App key and actor are required", ErrInvalidState)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var lifecycle AppRuntimeLifecycle
	err = tx.QueryRow(ctx, `SELECT workspace_id,app_key,state,reason,actor,revision,updated_at FROM app_runtime_lifecycle
WHERE workspace_id=$1 AND app_key=$2 FOR UPDATE`, request.WorkspaceID, request.AppKey).Scan(
		&lifecycle.WorkspaceID, &lifecycle.AppKey, &lifecycle.State, &lifecycle.Reason, &lifecycle.Actor, &lifecycle.Revision, &lifecycle.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && lifecycle.State == AppRuntimeActive {
		return fmt.Errorf("%w: App must be tombstoned or revoked before purge", ErrConflict)
	}
	if err != nil {
		return err
	}
	var validLeases int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE state='running' AND lease_expires_at > now()
AND COALESCE(NULLIF(payload->>'workspace',''), NULLIF(payload->'deployment'->>'workspace',''), 'default')=$1
AND COALESCE(NULLIF(payload->>'app',''), NULLIF(payload->'deployment'->>'app',''), '')=$2`, request.WorkspaceID, request.AppKey).Scan(&validLeases); err != nil {
		return err
	}
	if validLeases > 0 && !request.Force {
		return fmt.Errorf("%w: valid App Job leases still exist", ErrConflict)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM runtime_variable WHERE workspace_id=$1 AND owner_scope='app' AND app_key=$2`, request.WorkspaceID, request.AppKey); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM runtime_resource WHERE workspace_id=$1 AND owner_scope='app' AND app_key=$2`, request.WorkspaceID, request.AppKey); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO app_runtime_lifecycle_audit
(id,workspace_id,app_key,state,reason,actor,revision,purged,forced,created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,true,$8,now())`, NewID("audit"), request.WorkspaceID, request.AppKey, lifecycle.State,
		request.Reason, request.Actor, lifecycle.Revision, request.Force); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) ListAppRuntimeLifecycleAudit(ctx context.Context, workspaceID, appKey string) ([]AppRuntimeLifecycleAudit, error) {
	rows, err := s.pool.Query(ctx, `SELECT workspace_id,app_key,state,reason,actor,revision,purged,forced,created_at
FROM app_runtime_lifecycle_audit WHERE workspace_id=$1 AND app_key=$2 ORDER BY created_at,id`,
		contract.NormalizeWorkspace(workspaceID), strings.TrimSpace(appKey))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []AppRuntimeLifecycleAudit
	for rows.Next() {
		var audit AppRuntimeLifecycleAudit
		if err := rows.Scan(&audit.WorkspaceID, &audit.AppKey, &audit.State, &audit.Reason, &audit.Actor, &audit.Revision, &audit.Purged, &audit.Forced, &audit.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, audit)
	}
	return result, rows.Err()
}

func postgresAppRuntimeActive(ctx context.Context, tx pgx.Tx, workspaceID, appKey string) (bool, error) {
	var state AppRuntimeState
	err := tx.QueryRow(ctx, `SELECT state FROM app_runtime_lifecycle WHERE workspace_id=$1 AND app_key=$2`,
		contract.NormalizeWorkspace(workspaceID), strings.TrimSpace(appKey)).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	return state == AppRuntimeActive, err
}
