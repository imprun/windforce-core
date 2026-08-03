package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/resourceconfig"
)

func (s *PostgresStore) AppendLogs(ctx context.Context, jobID string, workspaceID string, chunk string) error {
	if chunk == "" {
		return nil
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	chunk = strings.ToValidUTF8(chunk, "\uFFFD")
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var legacyBytes int64
	err = tx.QueryRow(ctx, `
SELECT octet_length(logs)
FROM job_logs
WHERE job_id=$1 AND workspace_id=$2
`, jobID, workspaceID).Scan(&legacyBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		legacyBytes = 0
	} else if err != nil {
		return err
	}

	chunkBytes := int64(len([]byte(chunk)))
	var startOffset int64
	err = tx.QueryRow(ctx, `
INSERT INTO job_log_state (job_id, workspace_id, next_offset, updated_at)
SELECT $1, $2, $3, now()
WHERE EXISTS (
    SELECT 1
    FROM jobs
    WHERE id=$1
      AND COALESCE(NULLIF(payload->>'workspace', ''), NULLIF(payload->'deployment'->>'workspace', ''), 'default')=$2
)
ON CONFLICT (job_id) DO UPDATE
SET next_offset = job_log_state.next_offset + $4,
    updated_at = now()
RETURNING next_offset - $4
`, jobID, workspaceID, legacyBytes+chunkBytes, chunkBytes).Scan(&startOffset)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: job %q", ErrNotFound, jobID)
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO job_log_chunks (job_id, workspace_id, start_offset, chunk)
VALUES ($1, $2, $3, $4)
`, jobID, workspaceID, startOffset, chunk); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) GetLogs(ctx context.Context, workspaceID string, jobID string) (string, bool, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	exists, err := s.postgresJobExists(ctx, workspaceID, jobID)
	if err != nil || !exists {
		return "", exists, err
	}
	var legacy string
	err = s.pool.QueryRow(ctx, `
SELECT logs FROM job_logs WHERE job_id=$1 AND workspace_id=$2
`, jobID, workspaceID).Scan(&legacy)
	if errors.Is(err, pgx.ErrNoRows) {
		legacy = ""
	} else if err != nil {
		return "", false, err
	}
	rows, err := s.pool.Query(ctx, `
SELECT chunk
FROM job_log_chunks
WHERE job_id=$1 AND workspace_id=$2
ORDER BY start_offset
`, jobID, workspaceID)
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	var logs strings.Builder
	logs.WriteString(legacy)
	for rows.Next() {
		var chunk string
		if err := rows.Scan(&chunk); err != nil {
			return "", false, err
		}
		logs.WriteString(chunk)
	}
	if err := rows.Err(); err != nil {
		return "", false, err
	}
	return logs.String(), true, nil
}

func (s *PostgresStore) GetLogUpdate(ctx context.Context, workspaceID string, jobID string, afterOffset int64, limitBytes int) (JobLogUpdate, bool, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	exists, err := s.postgresJobExists(ctx, workspaceID, jobID)
	if err != nil || !exists {
		return JobLogUpdate{}, exists, err
	}
	if afterOffset < 0 {
		afterOffset = 0
	}
	if limitBytes <= 0 {
		limitBytes = 256 << 10
	}

	var legacy string
	err = s.pool.QueryRow(ctx, `
SELECT logs FROM job_logs WHERE job_id=$1 AND workspace_id=$2
`, jobID, workspaceID).Scan(&legacy)
	if errors.Is(err, pgx.ErrNoRows) {
		legacy = ""
	} else if err != nil {
		return JobLogUpdate{}, false, err
	}
	legacyBytes := int64(len([]byte(legacy)))
	totalBytes := legacyBytes
	err = s.pool.QueryRow(ctx, `
SELECT next_offset
FROM job_log_state
WHERE job_id=$1 AND workspace_id=$2
`, jobID, workspaceID).Scan(&totalBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		totalBytes = legacyBytes
	} else if err != nil {
		return JobLogUpdate{}, false, err
	}
	if afterOffset > totalBytes {
		afterOffset = totalBytes
	}

	currentOffset := afterOffset
	var logs strings.Builder
	if afterOffset < legacyBytes {
		window := logWindow(legacy, afterOffset, limitBytes)
		logs.WriteString(window.NewLogs)
		currentOffset = window.Offset
		if logs.Len() >= limitBytes {
			return JobLogUpdate{NewLogs: logs.String(), Offset: currentOffset}, true, nil
		}
	}

	rows, err := s.pool.Query(ctx, `
SELECT start_offset, chunk
FROM job_log_chunks
WHERE job_id=$1
  AND workspace_id=$2
  AND start_offset + octet_length(chunk) > $3
ORDER BY start_offset
`, jobID, workspaceID, currentOffset)
	if err != nil {
		return JobLogUpdate{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var startOffset int64
		var chunk string
		if err := rows.Scan(&startOffset, &chunk); err != nil {
			return JobLogUpdate{}, false, err
		}
		if currentOffset < startOffset {
			currentOffset = startOffset
		}
		remaining := limitBytes - logs.Len()
		if remaining <= 0 {
			break
		}
		window := logWindow(chunk, currentOffset-startOffset, remaining)
		logs.WriteString(window.NewLogs)
		currentOffset = startOffset + window.Offset
		if logs.Len() >= limitBytes {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return JobLogUpdate{}, false, err
	}
	return JobLogUpdate{NewLogs: logs.String(), Offset: currentOffset}, true, nil
}

func (s *PostgresStore) postgresJobExists(ctx context.Context, workspaceID string, jobID string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM jobs
    WHERE id=$1
      AND COALESCE(NULLIF(payload->>'workspace', ''), NULLIF(payload->'deployment'->>'workspace', ''), 'default')=$2
)
`, jobID, workspaceID).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (s *PostgresStore) GetState(ctx context.Context, workspaceID string, statePath string) (json.RawMessage, bool, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	var value json.RawMessage
	err := s.pool.QueryRow(ctx, `
SELECT value
FROM job_state
WHERE workspace_id=$1 AND state_path=$2
`, workspaceID, statePath).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return json.RawMessage("null"), false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return cloneRaw(value), true, nil
}

func (s *PostgresStore) SetState(ctx context.Context, workspaceID string, statePath string, value json.RawMessage) error {
	if len(value) == 0 {
		value = json.RawMessage("null")
	}
	if !json.Valid(value) {
		return errors.New("state value is not valid JSON")
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	_, err := s.pool.Exec(ctx, `
INSERT INTO job_state (workspace_id, state_path, value, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (workspace_id, state_path)
DO UPDATE SET value=EXCLUDED.value, updated_at=now()
`, workspaceID, statePath, value)
	return err
}

func (s *PostgresStore) ListVariables(ctx context.Context, workspaceID string) ([]Variable, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	rows, err := s.pool.Query(ctx, `
SELECT app_key, path, value, is_secret, description
FROM variable
WHERE workspace_id=$1
ORDER BY app_key, path
`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	variables := []Variable{}
	for rows.Next() {
		var variable Variable
		if err := rows.Scan(&variable.AppKey, &variable.Path, &variable.Value, &variable.IsSecret, &variable.Description); err != nil {
			return nil, err
		}
		variables = append(variables, variable)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return variables, nil
}

func (s *PostgresStore) SetVariable(ctx context.Context, workspaceID string, appKey string, path string, value string, isSecret bool, description string) error {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	path, err := contract.NormalizeRuntimeConfigPath(path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidState, err)
	}
	_, err = s.pool.Exec(ctx, `
INSERT INTO variable (workspace_id, app_key, path, value, is_secret, description)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (workspace_id, app_key, path)
DO UPDATE SET value=EXCLUDED.value, is_secret=EXCLUDED.is_secret, description=EXCLUDED.description
`, workspaceID, appKey, path, value, isSecret, description)
	return err
}

func (s *PostgresStore) GetVariable(ctx context.Context, workspaceID string, appKey string, path string) (Variable, bool, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	var variable Variable
	err := s.pool.QueryRow(ctx, `
SELECT app_key, path, value, is_secret, description
FROM variable
WHERE workspace_id=$1 AND path=$2 AND (app_key=$3 OR app_key='')
ORDER BY app_key DESC
LIMIT 1
`, workspaceID, path, appKey).Scan(&variable.AppKey, &variable.Path, &variable.Value, &variable.IsSecret, &variable.Description)
	if errors.Is(err, pgx.ErrNoRows) {
		return Variable{}, false, nil
	}
	if err != nil {
		return Variable{}, false, err
	}
	return variable, true, nil
}

func (s *PostgresStore) GetVariableExact(ctx context.Context, workspaceID string, appKey string, path string) (Variable, bool, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	var variable Variable
	err := s.pool.QueryRow(ctx, `
SELECT app_key, path, value, is_secret, description
FROM variable
WHERE workspace_id=$1 AND app_key=$2 AND path=$3
`, workspaceID, appKey, path).Scan(&variable.AppKey, &variable.Path, &variable.Value, &variable.IsSecret, &variable.Description)
	if errors.Is(err, pgx.ErrNoRows) {
		return Variable{}, false, nil
	}
	if err != nil {
		return Variable{}, false, err
	}
	return variable, true, nil
}

func (s *PostgresStore) GetWorkspaceKeyVersioned(ctx context.Context, workspaceID string) (string, int32, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	var key string
	var version int32
	err := s.pool.QueryRow(ctx, `
SELECT key, kek_version
FROM workspace_key
WHERE workspace_id=$1
`, workspaceID).Scan(&key, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	return key, version, nil
}

func (s *PostgresStore) DeleteVariable(ctx context.Context, workspaceID string, appKey string, path string) error {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	_, err := s.pool.Exec(ctx, `
DELETE FROM variable
WHERE workspace_id=$1 AND app_key=$2 AND path=$3
`, workspaceID, appKey, path)
	return err
}

func (s *PostgresStore) SetResource(ctx context.Context, workspaceID string, path string, value json.RawMessage, resourceType string, description string) error {
	normalizedPath, err := contract.NormalizeRuntimeConfigPath(path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidState, err)
	}
	path = normalizedPath
	if len(value) == 0 {
		value = json.RawMessage("{}")
	}
	if !json.Valid(value) {
		return errors.New("resource value is not valid JSON")
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	if resourceType != "" {
		name, version, err := resourceconfig.ParseTypeReference(resourceType)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidState, err)
		}
		registered, found, err := s.GetResourceType(ctx, workspaceID, name, version)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		if err := resourceconfig.ValidateValue(registered.Schema, value); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidState, err)
		}
	}
	_, err = s.pool.Exec(ctx, `
INSERT INTO resource (workspace_id, path, value, resource_type, description)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, path)
DO UPDATE SET value=EXCLUDED.value, resource_type=EXCLUDED.resource_type, description=EXCLUDED.description
`, workspaceID, path, value, resourceType, description)
	return err
}

func (s *PostgresStore) GetResource(ctx context.Context, workspaceID string, path string) (Resource, bool, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	var resource Resource
	err := s.pool.QueryRow(ctx, `
SELECT path, value, resource_type, description
FROM resource
WHERE workspace_id=$1 AND path=$2
`, workspaceID, path).Scan(&resource.Path, &resource.Value, &resource.ResourceType, &resource.Description)
	if errors.Is(err, pgx.ErrNoRows) {
		return Resource{}, false, nil
	}
	if err != nil {
		return Resource{}, false, err
	}
	resource.Value = cloneRaw(resource.Value)
	return resource, true, nil
}
