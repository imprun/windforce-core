package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/resourceconfig"
	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) GetVariableScoped(ctx context.Context, workspaceID string, scope contract.RuntimeConfigScope, appKey string, path string) (Variable, bool, error) {
	key, err := normalizedRuntimeObjectKey(scope, appKey, path)
	if err != nil {
		return Variable{}, false, err
	}
	_ = key
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	if scope == contract.RuntimeConfigScopeWorkspace {
		appKey = ""
	}
	var variable Variable
	variable.OwnerScope = scope
	err = s.pool.QueryRow(ctx, `
SELECT app_key, path, value, is_secret, description, revision, updated_at
FROM runtime_variable
WHERE workspace_id=$1 AND owner_scope=$2 AND app_key=$3 AND path=$4
`, workspaceID, string(scope), strings.TrimSpace(appKey), strings.TrimSpace(path)).Scan(
		&variable.AppKey, &variable.Path, &variable.Value, &variable.IsSecret,
		&variable.Description, &variable.Revision, &variable.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Variable{}, false, nil
	}
	return variable, err == nil, err
}

func (s *PostgresStore) GetResourceScoped(ctx context.Context, workspaceID string, scope contract.RuntimeConfigScope, appKey string, path string) (Resource, bool, error) {
	key, err := normalizedRuntimeObjectKey(scope, appKey, path)
	if err != nil {
		return Resource{}, false, err
	}
	_ = key
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	if scope == contract.RuntimeConfigScopeWorkspace {
		appKey = ""
	}
	var resource Resource
	resource.OwnerScope = scope
	err = s.pool.QueryRow(ctx, `
SELECT app_key, path, value, resource_type, description, revision, updated_at
FROM runtime_resource
WHERE workspace_id=$1 AND owner_scope=$2 AND app_key=$3 AND path=$4
`, workspaceID, string(scope), strings.TrimSpace(appKey), strings.TrimSpace(path)).Scan(
		&resource.AppKey, &resource.Path, &resource.Value, &resource.ResourceType,
		&resource.Description, &resource.Revision, &resource.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Resource{}, false, nil
	}
	return resource, err == nil, err
}

func (s *PostgresStore) MutateRuntimeVariable(ctx context.Context, request RuntimeVariableMutationRequest) (RuntimeConfigMutationResult, error) {
	workspaceID, appKey, path, err := normalizeRuntimeMutation(
		request.WorkspaceID, request.AppKey, request.Path, request.OperationID,
		request.RequestFingerprint, request.JobID, request.Attempt,
	)
	if err != nil {
		return RuntimeConfigMutationResult{}, err
	}
	if err := validateRuntimeVariableValue(request); err != nil {
		return RuntimeConfigMutationResult{}, err
	}
	request.WorkspaceID, request.AppKey, request.Path = workspaceID, appKey, path
	var result RuntimeConfigMutationResult
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		now := time.Now().UTC()
		job, err := postgresAuthorizeRuntimeMutation(ctx, tx, now, workspaceID, appKey, request.JobID, request.Attempt)
		if err != nil {
			return err
		}
		storage := contract.RuntimeVariableStoragePlain
		if request.IsSecret {
			storage = contract.RuntimeVariableStorageSecret
		}
		if !allowsVariableWrite(job.Payload.RuntimeAccess, path, storage, job.Payload.PermissionedAs) {
			return runtimeConfigError(RuntimeConfigCodeStorageClassMismatch, fmt.Errorf("variable write target or storage class is not pinned: %w", ErrForbidden))
		}
		replay, found, err := postgresRuntimeOperation(ctx, tx, workspaceID, request.JobID, request.Attempt, request.OperationID)
		if err != nil {
			return err
		}
		if found {
			if replay.RequestFingerprint != request.RequestFingerprint {
				return runtimeConfigError(RuntimeConfigCodeOperationConflict, fmt.Errorf("operationId was already used with another payload: %w", ErrConflict))
			}
			result = RuntimeConfigMutationResult{Path: replay.Path, Revision: replay.Revision, Replayed: true}
			return nil
		}
		if err := postgresCheckRuntimeWriteLimit(ctx, tx, workspaceID, request.JobID, request.Attempt); err != nil {
			return err
		}
		current, err := postgresCurrentRuntimeRevision(ctx, tx, "runtime_variable", workspaceID, appKey, path)
		if err != nil {
			return err
		}
		if request.ExpectedRevision != nil && current != *request.ExpectedRevision {
			return runtimeConfigRevisionError(current)
		}
		revision := max(current+1, 1)
		if _, err := tx.Exec(ctx, `
INSERT INTO runtime_variable (
    workspace_id, owner_scope, app_key, path, value, is_secret, description, revision, updated_at
) VALUES ($1, 'app', $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (workspace_id, owner_scope, app_key, path)
DO UPDATE SET value=EXCLUDED.value, is_secret=EXCLUDED.is_secret,
              description=EXCLUDED.description, revision=EXCLUDED.revision, updated_at=EXCLUDED.updated_at
`, workspaceID, appKey, path, request.Value, request.IsSecret, request.Description, revision, now); err != nil {
			return err
		}
		operation := RuntimeConfigOperation{
			WorkspaceID: workspaceID, JobID: request.JobID, Attempt: request.Attempt,
			OperationID: request.OperationID, RequestFingerprint: request.RequestFingerprint,
			ObjectKind: "variable", AppKey: appKey, Path: path, Revision: revision, CreatedAt: now,
		}
		audit := RuntimeConfigAudit{
			WorkspaceID: workspaceID, OwnerScope: contract.RuntimeConfigScopeApp,
			AppKey: appKey, Path: path, ObjectKind: "variable", Storage: string(storage),
			Revision: revision, OperationID: request.OperationID, JobID: request.JobID,
			Attempt: request.Attempt, Actor: runtimeMutationActor(request.Actor, request.JobID), CreatedAt: now,
		}
		if err := postgresRecordRuntimeConfigSuccess(ctx, tx, operation, audit); err != nil {
			return err
		}
		result = RuntimeConfigMutationResult{Path: path, Revision: revision}
		return nil
	})
	return result, err
}

func (s *PostgresStore) MutateRuntimeResource(ctx context.Context, request RuntimeResourceMutationRequest) (RuntimeConfigMutationResult, error) {
	workspaceID, appKey, path, err := normalizeRuntimeMutation(
		request.WorkspaceID, request.AppKey, request.Path, request.OperationID,
		request.RequestFingerprint, request.JobID, request.Attempt,
	)
	if err != nil {
		return RuntimeConfigMutationResult{}, err
	}
	if len(request.Value) == 0 || len(request.Value) > RuntimeConfigMaxValueBytes || !json.Valid(request.Value) {
		return RuntimeConfigMutationResult{}, runtimeConfigError(RuntimeConfigCodeLimitExceeded, fmt.Errorf("resource value must be valid JSON within %d bytes: %w", RuntimeConfigMaxValueBytes, ErrInvalidState))
	}
	request.WorkspaceID, request.AppKey, request.Path = workspaceID, appKey, path
	var result RuntimeConfigMutationResult
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		now := time.Now().UTC()
		job, err := postgresAuthorizeRuntimeMutation(ctx, tx, now, workspaceID, appKey, request.JobID, request.Attempt)
		if err != nil {
			return err
		}
		if !allowsResourceWrite(job.Payload.RuntimeAccess, path, job.Payload.PermissionedAs) {
			return runtimeConfigError(RuntimeConfigCodeForbidden, fmt.Errorf("resource write target is not pinned: %w", ErrForbidden))
		}
		replay, found, err := postgresRuntimeOperation(ctx, tx, workspaceID, request.JobID, request.Attempt, request.OperationID)
		if err != nil {
			return err
		}
		if found {
			if replay.RequestFingerprint != request.RequestFingerprint {
				return runtimeConfigError(RuntimeConfigCodeOperationConflict, fmt.Errorf("operationId was already used with another payload: %w", ErrConflict))
			}
			result = RuntimeConfigMutationResult{Path: replay.Path, Revision: replay.Revision, Replayed: true}
			return nil
		}
		if err := postgresCheckRuntimeWriteLimit(ctx, tx, workspaceID, request.JobID, request.Attempt); err != nil {
			return err
		}
		name, version, err := resourceconfig.ParseTypeReference(request.ResourceType)
		if err != nil {
			return fmt.Errorf("%w: App-owned Resource requires a versioned resource type: %v", ErrInvalidState, err)
		}
		var schema json.RawMessage
		if err := tx.QueryRow(ctx, `SELECT schema FROM resource_type WHERE workspace_id=$1 AND name=$2 AND version=$3`, workspaceID, name, version).Scan(&schema); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if err := resourceconfig.ValidateValue(schema, request.Value); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidState, err)
		}
		if err := authorizeResourceReferences(request.Value, job.Payload.RuntimeAccess); err != nil {
			return err
		}
		current, err := postgresCurrentRuntimeRevision(ctx, tx, "runtime_resource", workspaceID, appKey, path)
		if err != nil {
			return err
		}
		if request.ExpectedRevision != nil && current != *request.ExpectedRevision {
			return runtimeConfigRevisionError(current)
		}
		revision := max(current+1, 1)
		if _, err := tx.Exec(ctx, `
INSERT INTO runtime_resource (
    workspace_id, owner_scope, app_key, path, value, resource_type, description, revision, updated_at
) VALUES ($1, 'app', $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (workspace_id, owner_scope, app_key, path)
DO UPDATE SET value=EXCLUDED.value, resource_type=EXCLUDED.resource_type,
              description=EXCLUDED.description, revision=EXCLUDED.revision, updated_at=EXCLUDED.updated_at
`, workspaceID, appKey, path, request.Value, name+"@"+version, request.Description, revision, now); err != nil {
			return err
		}
		operation := RuntimeConfigOperation{
			WorkspaceID: workspaceID, JobID: request.JobID, Attempt: request.Attempt,
			OperationID: request.OperationID, RequestFingerprint: request.RequestFingerprint,
			ObjectKind: "resource", AppKey: appKey, Path: path, Revision: revision, CreatedAt: now,
		}
		audit := RuntimeConfigAudit{
			WorkspaceID: workspaceID, OwnerScope: contract.RuntimeConfigScopeApp,
			AppKey: appKey, Path: path, ObjectKind: "resource", Revision: revision,
			OperationID: request.OperationID, JobID: request.JobID, Attempt: request.Attempt,
			Actor: runtimeMutationActor(request.Actor, request.JobID), CreatedAt: now,
		}
		if err := postgresRecordRuntimeConfigSuccess(ctx, tx, operation, audit); err != nil {
			return err
		}
		result = RuntimeConfigMutationResult{Path: path, Revision: revision}
		return nil
	})
	return result, err
}

func (s *PostgresStore) ListRuntimeConfigAudit(ctx context.Context, workspaceID string, appKey string) ([]RuntimeConfigAudit, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	rows, err := s.pool.Query(ctx, `
SELECT id, workspace_id, owner_scope, app_key, path, object_kind, storage, revision,
       operation_id, job_id, attempt, actor, created_at
FROM runtime_config_audit
WHERE workspace_id=$1 AND ($2='' OR app_key=$2)
ORDER BY created_at DESC, id DESC
`, workspaceID, strings.TrimSpace(appKey))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []RuntimeConfigAudit{}
	for rows.Next() {
		var item RuntimeConfigAudit
		if err := rows.Scan(
			&item.ID, &item.WorkspaceID, &item.OwnerScope, &item.AppKey, &item.Path,
			&item.ObjectKind, &item.Storage, &item.Revision, &item.OperationID,
			&item.JobID, &item.Attempt, &item.Actor, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func postgresAuthorizeRuntimeMutation(ctx context.Context, tx pgx.Tx, now time.Time, workspaceID, appKey, jobID string, attempt int) (Job, error) {
	job, err := scanJob(tx.QueryRow(ctx, `
SELECT `+jobColumns+`
FROM jobs
WHERE id=$1
  AND COALESCE(NULLIF(payload->>'workspace', ''), NULLIF(payload->'deployment'->>'workspace', ''), 'default')=$2
FOR UPDATE
`, jobID, workspaceID))
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (job.Payload.App != appKey || job.Attempt != attempt ||
		job.State != JobRunning || job.LeaseOwner == "" || job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.After(now)) {
		return Job{}, runtimeConfigError(RuntimeConfigCodeAttemptInvalid, fmt.Errorf("job attempt is not live: %w", ErrInvalidState))
	}
	if err != nil {
		return Job{}, err
	}
	var lifecycleState AppRuntimeState
	var lifecycleUpdatedAt time.Time
	err = tx.QueryRow(ctx, `SELECT state, updated_at FROM app_runtime_lifecycle WHERE workspace_id=$1 AND app_key=$2`, workspaceID, appKey).Scan(&lifecycleState, &lifecycleUpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return Job{}, err
	}
	if err == nil && (lifecycleState == AppRuntimeRevoked || lifecycleState == AppRuntimeTombstoned && (job.StartedAt == nil || job.StartedAt.After(lifecycleUpdatedAt))) {
		return Job{}, runtimeConfigError(RuntimeConfigCodeForbidden, fmt.Errorf("App runtime access is not active for this attempt: %w", ErrForbidden))
	}
	return job, err
}

func postgresRuntimeOperation(ctx context.Context, tx pgx.Tx, workspaceID, jobID string, attempt int, operationID string) (RuntimeConfigOperation, bool, error) {
	var operation RuntimeConfigOperation
	err := tx.QueryRow(ctx, `
SELECT request_fingerprint, object_kind, app_key, path, revision, created_at
FROM runtime_config_operation
WHERE workspace_id=$1 AND job_id=$2 AND attempt=$3 AND operation_id=$4
`, workspaceID, jobID, attempt, operationID).Scan(
		&operation.RequestFingerprint, &operation.ObjectKind, &operation.AppKey,
		&operation.Path, &operation.Revision, &operation.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RuntimeConfigOperation{}, false, nil
	}
	operation.WorkspaceID, operation.JobID, operation.Attempt, operation.OperationID = workspaceID, jobID, attempt, operationID
	return operation, err == nil, err
}

func postgresCheckRuntimeWriteLimit(ctx context.Context, tx pgx.Tx, workspaceID, jobID string, attempt int) error {
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM runtime_config_operation WHERE workspace_id=$1 AND job_id=$2 AND attempt=$3`, workspaceID, jobID, attempt).Scan(&count); err != nil {
		return err
	}
	if count >= RuntimeConfigMaxWritesPerAttempt {
		return runtimeConfigError(RuntimeConfigCodeLimitExceeded, fmt.Errorf("runtime writes exceed per-attempt limit %d: %w", RuntimeConfigMaxWritesPerAttempt, ErrInvalidState))
	}
	return nil
}

func postgresCurrentRuntimeRevision(ctx context.Context, tx pgx.Tx, table, workspaceID, appKey, path string) (int64, error) {
	if table != "runtime_variable" && table != "runtime_resource" {
		return 0, fmt.Errorf("unsupported runtime configuration table %q", table)
	}
	var revision int64
	err := tx.QueryRow(ctx, `SELECT revision FROM `+table+` WHERE workspace_id=$1 AND owner_scope='app' AND app_key=$2 AND path=$3 FOR UPDATE`, workspaceID, appKey, path).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return revision, err
}

func postgresRecordRuntimeConfigSuccess(ctx context.Context, tx pgx.Tx, operation RuntimeConfigOperation, audit RuntimeConfigAudit) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO runtime_config_operation (
    workspace_id, job_id, attempt, operation_id, request_fingerprint,
    object_kind, app_key, path, revision, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
`, operation.WorkspaceID, operation.JobID, operation.Attempt, operation.OperationID,
		operation.RequestFingerprint, operation.ObjectKind, operation.AppKey,
		operation.Path, operation.Revision, operation.CreatedAt); err != nil {
		return err
	}
	audit.ID = "runtime-config:" + operation.JobID + ":" + fmt.Sprint(operation.Attempt) + ":" + operation.OperationID
	_, err := tx.Exec(ctx, `
INSERT INTO runtime_config_audit (
    id, workspace_id, owner_scope, app_key, path, object_kind, storage,
    revision, operation_id, job_id, attempt, actor, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
`, audit.ID, audit.WorkspaceID, string(audit.OwnerScope), audit.AppKey, audit.Path,
		audit.ObjectKind, audit.Storage, audit.Revision, audit.OperationID,
		audit.JobID, audit.Attempt, audit.Actor, audit.CreatedAt)
	return err
}
