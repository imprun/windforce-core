package state

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

const workerCredentialColumns = `id, worker_group, generation, workspace_ids, labels, status,
expires_at, revoked_at, drain_deadline_at, token_hash, operation_id, request_fingerprint,
revoke_operation_id, revoke_fingerprint, created_by, created_at, updated_at`

func scanWorkerCredential(row pgx.Row) (WorkerCredential, error) {
	var credential WorkerCredential
	err := row.Scan(
		&credential.ID, &credential.Group, &credential.Generation, &credential.WorkspaceIDs, &credential.Labels,
		&credential.Status, &credential.ExpiresAt, &credential.RevokedAt, &credential.DrainDeadlineAt,
		&credential.TokenHash, &credential.OperationID, &credential.RequestFingerprint,
		&credential.RevokeOperationID, &credential.RevokeFingerprint, &credential.CreatedBy,
		&credential.CreatedAt, &credential.UpdatedAt,
	)
	return credential, err
}

func (s *PostgresStore) CreateWorkerCredential(ctx context.Context, request CreateWorkerCredentialRequest) (WorkerCredential, bool, error) {
	group, err := NormalizeWorkerGroup(request.Group)
	if err != nil {
		return WorkerCredential{}, false, err
	}
	request.OperationID = strings.TrimSpace(request.OperationID)
	request.RequestFingerprint = strings.TrimSpace(request.RequestFingerprint)
	request.TokenHash = strings.TrimSpace(request.TokenHash)
	workspaces, labels, err := NormalizeWorkerCredentialScope(request.WorkspaceIDs, request.Labels)
	if err != nil {
		return WorkerCredential{}, false, err
	}
	if request.ExpectedGeneration < 0 || request.OperationID == "" || request.RequestFingerprint == "" || request.TokenHash == "" {
		return WorkerCredential{}, false, ErrInvalidState
	}
	var result WorkerCredential
	var replayed bool
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "worker-credential:"+group); err != nil {
			return err
		}
		existing, err := scanWorkerCredential(tx.QueryRow(ctx, `SELECT `+workerCredentialColumns+`
FROM worker_credential WHERE worker_group=$1 AND operation_id=$2`, group, request.OperationID))
		if err == nil {
			if existing.RequestFingerprint != request.RequestFingerprint {
				return ErrConflict
			}
			result = existing
			replayed = true
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var currentGeneration int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(generation), 0) FROM worker_credential WHERE worker_group=$1`, group).Scan(&currentGeneration); err != nil {
			return err
		}
		if currentGeneration != request.ExpectedGeneration {
			return ErrConflict
		}
		id := strings.TrimSpace(request.ID)
		if id == "" {
			id = NewID("worker_credential")
		}
		result, err = scanWorkerCredential(tx.QueryRow(ctx, `
INSERT INTO worker_credential (
    id, worker_group, generation, workspace_ids, labels, status, expires_at,
    token_hash, operation_id, request_fingerprint, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING `+workerCredentialColumns,
			id, group, currentGeneration+1, workspaces, labels,
			WorkerCredentialActive, request.ExpiresAt, request.TokenHash, request.OperationID,
			request.RequestFingerprint, strings.TrimSpace(request.Actor)))
		if err != nil {
			return clientPostgresError(err)
		}
		return nil
	})
	return result, replayed, err
}

func (s *PostgresStore) ListWorkerCredentials(ctx context.Context, group string) ([]WorkerCredential, error) {
	group, err := NormalizeWorkerGroup(group)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+workerCredentialColumns+`
FROM worker_credential WHERE worker_group=$1 ORDER BY generation DESC`, group)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkerCredential{}
	for rows.Next() {
		credential, err := scanWorkerCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, credential)
	}
	return out, rows.Err()
}

func (s *PostgresStore) GetWorkerCredential(ctx context.Context, group string, id string) (WorkerCredential, error) {
	group, err := NormalizeWorkerGroup(group)
	if err != nil {
		return WorkerCredential{}, err
	}
	credential, err := scanWorkerCredential(s.pool.QueryRow(ctx, `SELECT `+workerCredentialColumns+`
FROM worker_credential WHERE worker_group=$1 AND id=$2`, group, strings.TrimSpace(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkerCredential{}, ErrNotFound
	}
	return credential, err
}

func (s *PostgresStore) GetWorkerCredentialByTokenHash(ctx context.Context, tokenHash string) (WorkerCredential, error) {
	credential, err := scanWorkerCredential(s.pool.QueryRow(ctx, `SELECT `+workerCredentialColumns+`
FROM worker_credential WHERE token_hash=$1`, strings.TrimSpace(tokenHash)))
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkerCredential{}, ErrNotFound
	}
	return credential, err
}

func (s *PostgresStore) RevokeWorkerCredential(ctx context.Context, request RevokeWorkerCredentialRequest) (WorkerCredential, bool, error) {
	group, err := NormalizeWorkerGroup(request.Group)
	if err != nil {
		return WorkerCredential{}, false, err
	}
	request.CredentialID = strings.TrimSpace(request.CredentialID)
	request.OperationID = strings.TrimSpace(request.OperationID)
	request.RequestFingerprint = strings.TrimSpace(request.RequestFingerprint)
	if request.CredentialID == "" || request.OperationID == "" || request.RequestFingerprint == "" || request.DrainDeadlineAt.IsZero() {
		return WorkerCredential{}, false, ErrInvalidState
	}
	var result WorkerCredential
	var replayed bool
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		credential, err := scanWorkerCredential(tx.QueryRow(ctx, `SELECT `+workerCredentialColumns+`
FROM worker_credential WHERE worker_group=$1 AND id=$2 FOR UPDATE`, group, request.CredentialID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if credential.RevokeOperationID != "" {
			if credential.RevokeOperationID != request.OperationID || credential.RevokeFingerprint != request.RequestFingerprint {
				return ErrConflict
			}
			result = credential
			replayed = true
			return nil
		}
		result, err = scanWorkerCredential(tx.QueryRow(ctx, `
UPDATE worker_credential SET
    status=$3, revoked_at=now(), drain_deadline_at=$4,
    revoke_operation_id=$5, revoke_fingerprint=$6, updated_at=now()
WHERE worker_group=$1 AND id=$2
RETURNING `+workerCredentialColumns,
			group, request.CredentialID, WorkerCredentialRevoked, request.DrainDeadlineAt.UTC(),
			request.OperationID, request.RequestFingerprint))
		return err
	})
	return result, replayed, err
}

func (s *PostgresStore) GetWorkerGroupRunState(ctx context.Context, group string) (WorkerGroupRunState, error) {
	group, err := NormalizeWorkerGroup(group)
	if err != nil {
		return WorkerGroupRunState{}, err
	}
	var result WorkerGroupRunState
	err = s.pool.QueryRow(ctx, `
SELECT worker_group, state, operation_id, revision, deadline_at, request_fingerprint, updated_by, updated_at
FROM worker_group_run_state WHERE worker_group=$1`, group).Scan(
		&result.Group, &result.State, &result.OperationID, &result.Revision, &result.DeadlineAt,
		&result.RequestFingerprint, &result.UpdatedBy, &result.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultWorkerGroupRunState(group), nil
	}
	return result, err
}

func (s *PostgresStore) PutWorkerGroupRunState(ctx context.Context, request PutWorkerGroupRunStateRequest) (WorkerGroupRunState, bool, error) {
	group, err := NormalizeWorkerGroup(request.Group)
	if err != nil {
		return WorkerGroupRunState{}, false, err
	}
	stateValue, err := NormalizeWorkerGroupRunState(request.State)
	if err != nil {
		return WorkerGroupRunState{}, false, err
	}
	request.OperationID = strings.TrimSpace(request.OperationID)
	request.RequestFingerprint = strings.TrimSpace(request.RequestFingerprint)
	if request.ExpectedRevision < 0 || request.OperationID == "" || request.RequestFingerprint == "" ||
		(stateValue == WorkerGroupDraining && request.DeadlineAt == nil) {
		return WorkerGroupRunState{}, false, ErrInvalidState
	}
	if stateValue == WorkerGroupRunning {
		request.DeadlineAt = nil
	} else if request.DeadlineAt != nil {
		deadline := request.DeadlineAt.UTC()
		request.DeadlineAt = &deadline
	}
	var result WorkerGroupRunState
	var replayed bool
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "worker-run-state:"+group); err != nil {
			return err
		}
		current := DefaultWorkerGroupRunState(group)
		err := tx.QueryRow(ctx, `
SELECT worker_group, state, operation_id, revision, deadline_at, request_fingerprint, updated_by, updated_at
FROM worker_group_run_state WHERE worker_group=$1`, group).Scan(
			&current.Group, &current.State, &current.OperationID, &current.Revision, &current.DeadlineAt,
			&current.RequestFingerprint, &current.UpdatedBy, &current.UpdatedAt,
		)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if current.OperationID == request.OperationID {
			if current.RequestFingerprint != request.RequestFingerprint {
				return ErrConflict
			}
			result = current
			replayed = true
			return nil
		}
		if current.Revision != request.ExpectedRevision {
			return ErrConflict
		}
		result = WorkerGroupRunState{
			Group: group, State: stateValue, OperationID: request.OperationID,
			Revision: current.Revision + 1, DeadlineAt: request.DeadlineAt,
			RequestFingerprint: request.RequestFingerprint,
			UpdatedBy:          strings.TrimSpace(request.Actor),
		}
		return tx.QueryRow(ctx, `
INSERT INTO worker_group_run_state (
    worker_group, state, operation_id, revision, deadline_at, request_fingerprint, updated_by
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (worker_group) DO UPDATE SET
    state=EXCLUDED.state, operation_id=EXCLUDED.operation_id, revision=EXCLUDED.revision,
    deadline_at=EXCLUDED.deadline_at, request_fingerprint=EXCLUDED.request_fingerprint,
    updated_by=EXCLUDED.updated_by, updated_at=now()
RETURNING updated_at`,
			result.Group, result.State, result.OperationID, result.Revision, result.DeadlineAt,
			result.RequestFingerprint, result.UpdatedBy).Scan(&result.UpdatedAt)
	})
	return result, replayed, err
}

func (s *PostgresStore) GetWorker(ctx context.Context, workerID string) (WorkerRecord, error) {
	var record WorkerRecord
	var tags, labels, profiles []byte
	err := s.pool.QueryRow(ctx, `
SELECT id, worker_group, engine_version, build_revision, tags, labels, execution_profiles, slots, status, credential_id, credential_generation, started_at, last_heartbeat_at
FROM worker_registry WHERE id=$1`, strings.TrimSpace(workerID)).Scan(
		&record.ID, &record.Group, &record.EngineVersion, &record.BuildRevision, &tags, &labels, &profiles, &record.Slots, &record.Status,
		&record.CredentialID, &record.CredentialGeneration, &record.StartedAt, &record.LastHeartbeatAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkerRecord{}, ErrNotFound
	}
	if err != nil {
		return WorkerRecord{}, err
	}
	if err := json.Unmarshal(tags, &record.Tags); err != nil {
		return WorkerRecord{}, err
	}
	if err := json.Unmarshal(labels, &record.Labels); err != nil {
		return WorkerRecord{}, err
	}
	if err := json.Unmarshal(profiles, &record.ExecutionProfiles); err != nil {
		return WorkerRecord{}, err
	}
	return record, nil
}
