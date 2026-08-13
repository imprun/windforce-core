package state

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/imprun/windforce-core/internal/contract"
)

const clientColumns = `id, workspace_id, name, token_hash,
invocation_policy_mode, invocation_allowed_targets, invocation_policy_revision,
invocation_policy_operation_id, invocation_policy_request_fingerprint,
created_by, updated_by, created_at, updated_at`

func (s *PostgresStore) ListClients(ctx context.Context, workspaceID string) ([]Client, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	rows, err := s.pool.Query(ctx, `
SELECT `+clientColumns+`
FROM client_registry WHERE workspace_id=$1 ORDER BY name, id
`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	clients := []Client{}
	for rows.Next() {
		client, err := scanClient(rows)
		if err != nil {
			return nil, err
		}
		clients = append(clients, client)
	}
	return clients, rows.Err()
}

func (s *PostgresStore) GetClient(ctx context.Context, workspaceID string, id string) (Client, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	client, err := scanClient(s.pool.QueryRow(ctx, `
SELECT `+clientColumns+`
FROM client_registry WHERE workspace_id=$1 AND id=$2
`, workspaceID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Client{}, ErrNotFound
	}
	return client, err
}

func (s *PostgresStore) GetClientByTokenHash(ctx context.Context, workspaceID string, tokenHash string) (Client, error) {
	client, err := scanClient(s.pool.QueryRow(ctx, `
SELECT `+clientColumns+`
FROM client_registry
WHERE workspace_id=$1 AND token_hash=$2 AND token_hash <> ''
`, contract.NormalizeWorkspace(workspaceID), tokenHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return Client{}, ErrNotFound
	}
	return client, err
}

func (s *PostgresStore) CreateClient(ctx context.Context, workspaceID string, name string, tokenHash string, actor string) (Client, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	id := NewID("client")
	var created Client
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var err error
		created, err = scanClient(tx.QueryRow(ctx, `
INSERT INTO client_registry (workspace_id, id, name, token_hash, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $5)
RETURNING `+clientColumns+`
`, workspaceID, id, name, tokenHash, actor))
		if err != nil {
			return clientPostgresError(err)
		}
		return insertClientAudit(ctx, tx, workspaceID, id, "created", "", actor)
	})
	return created, err
}

func (s *PostgresStore) UpdateClient(ctx context.Context, workspaceID string, id string, name string, actor string) (Client, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	var updated Client
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		current, err := scanClient(tx.QueryRow(ctx, `
SELECT `+clientColumns+`
FROM client_registry WHERE workspace_id=$1 AND id=$2 FOR UPDATE
`, workspaceID, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		updated, err = scanClient(tx.QueryRow(ctx, `
UPDATE client_registry SET name=$3, updated_by=$4, updated_at=now()
WHERE workspace_id=$1 AND id=$2
RETURNING `+clientColumns+`
`, workspaceID, id, name, actor))
		if err != nil {
			return clientPostgresError(err)
		}
		return insertClientAudit(ctx, tx, workspaceID, id, "updated", clientChangeDetail(current, name), actor)
	})
	return updated, err
}

func (s *PostgresStore) UpdateClientInvocationPolicy(ctx context.Context, request UpdateClientInvocationPolicyRequest) (Client, bool, error) {
	request.WorkspaceID = contract.NormalizeWorkspace(request.WorkspaceID)
	request.ClientID = strings.TrimSpace(request.ClientID)
	request.OperationID = strings.TrimSpace(request.OperationID)
	request.RequestFingerprint = strings.TrimSpace(request.RequestFingerprint)
	request.Actor = strings.TrimSpace(request.Actor)
	policy, err := NormalizeTargetPolicy(request.Policy)
	if err != nil || request.ClientID == "" || request.ExpectedRevision < 0 || request.OperationID == "" ||
		len(request.OperationID) > 128 || CleanID(request.OperationID) != request.OperationID || request.RequestFingerprint == "" {
		return Client{}, false, ErrInvalidState
	}
	var updated Client
	replayed := false
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		current, getErr := scanClient(tx.QueryRow(ctx, `
SELECT `+clientColumns+`
FROM client_registry WHERE workspace_id=$1 AND id=$2 FOR UPDATE
`, request.WorkspaceID, request.ClientID))
		if errors.Is(getErr, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if getErr != nil {
			return getErr
		}
		if current.InvocationPolicyOperationID == request.OperationID {
			if current.InvocationPolicyRequestFingerprint != request.RequestFingerprint {
				return ErrConflict
			}
			updated = current
			replayed = true
			return nil
		}
		if current.InvocationPolicyRevision != request.ExpectedRevision {
			return ErrConflict
		}
		var updateErr error
		updated, updateErr = scanClient(tx.QueryRow(ctx, `
UPDATE client_registry
SET invocation_policy_mode=$3, invocation_allowed_targets=$4,
    invocation_policy_revision=invocation_policy_revision+1,
    invocation_policy_operation_id=$5, invocation_policy_request_fingerprint=$6,
    updated_by=$7, updated_at=now()
WHERE workspace_id=$1 AND id=$2
RETURNING `+clientColumns+`
`, request.WorkspaceID, request.ClientID, policy.Mode, policy.AllowedTargets, request.OperationID, request.RequestFingerprint, request.Actor))
		if updateErr != nil {
			return clientPostgresError(updateErr)
		}
		return insertClientAudit(ctx, tx, request.WorkspaceID, request.ClientID, "invocation_policy_updated", clientInvocationPolicyDetail(updated), request.Actor)
	})
	return updated, replayed, err
}

func (s *PostgresStore) RotateClientToken(ctx context.Context, workspaceID string, id string, tokenHash string, actor string) (Client, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	if tokenHash == "" {
		return Client{}, ErrInvalidState
	}
	var updated Client
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var err error
		updated, err = scanClient(tx.QueryRow(ctx, `
UPDATE client_registry SET token_hash=$3, updated_by=$4, updated_at=now()
WHERE workspace_id=$1 AND id=$2
RETURNING `+clientColumns+`
`, workspaceID, id, tokenHash, actor))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return clientPostgresError(err)
		}
		return insertClientAudit(ctx, tx, workspaceID, id, "token_rotated", "", actor)
	})
	return updated, err
}

func (s *PostgresStore) RevokeClientToken(ctx context.Context, workspaceID string, id string, actor string) (Client, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	var updated Client
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var err error
		updated, err = scanClient(tx.QueryRow(ctx, `
UPDATE client_registry SET token_hash='', updated_by=$3, updated_at=now()
WHERE workspace_id=$1 AND id=$2 AND token_hash <> ''
RETURNING `+clientColumns+`
`, workspaceID, id, actor))
		if errors.Is(err, pgx.ErrNoRows) {
			var currentHash string
			getErr := tx.QueryRow(ctx, `SELECT token_hash FROM client_registry WHERE workspace_id=$1 AND id=$2`, workspaceID, id).Scan(&currentHash)
			if errors.Is(getErr, pgx.ErrNoRows) {
				return ErrNotFound
			}
			if getErr != nil {
				return getErr
			}
			return ErrInvalidState
		}
		if err != nil {
			return err
		}
		return insertClientAudit(ctx, tx, workspaceID, id, "token_revoked", "", actor)
	})
	return updated, err
}

func (s *PostgresStore) DeleteClient(ctx context.Context, workspaceID string, id string, actor string) error {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	return s.withTx(ctx, func(tx pgx.Tx) error {
		result, err := tx.Exec(ctx, `DELETE FROM client_registry WHERE workspace_id=$1 AND id=$2 AND token_hash=''`, workspaceID, id)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			var currentHash string
			getErr := tx.QueryRow(ctx, `SELECT token_hash FROM client_registry WHERE workspace_id=$1 AND id=$2`, workspaceID, id).Scan(&currentHash)
			if errors.Is(getErr, pgx.ErrNoRows) {
				return ErrNotFound
			}
			if getErr != nil {
				return getErr
			}
			if currentHash != "" {
				return fmt.Errorf("%w: revoke the active client token before deleting the client", ErrConflict)
			}
			return ErrNotFound
		}
		return insertClientAudit(ctx, tx, workspaceID, id, "deleted", "", actor)
	})
}

func (s *PostgresStore) AppendClientAudit(ctx context.Context, workspaceID string, id string, kind string, detail string, actor string) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		return insertClientAudit(ctx, tx, contract.NormalizeWorkspace(workspaceID), id, kind, detail, actor)
	})
}

func (s *PostgresStore) ListClientAudit(ctx context.Context, workspaceID string, id string) ([]ClientAudit, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	rows, err := s.pool.Query(ctx, `
SELECT id::text, workspace_id, client_id, kind, detail, actor, created_at
FROM client_registry_audit WHERE workspace_id=$1 AND ($2='' OR client_id=$2)
ORDER BY created_at DESC, id DESC
`, workspaceID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []ClientAudit{}
	for rows.Next() {
		var record ClientAudit
		if err := rows.Scan(&record.ID, &record.WorkspaceID, &record.ClientID, &record.Kind, &record.Detail, &record.Actor, &record.CreatedAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

type clientScanner interface {
	Scan(dest ...any) error
}

func scanClient(row clientScanner) (Client, error) {
	var client Client
	err := row.Scan(
		&client.ID,
		&client.WorkspaceID,
		&client.Name,
		&client.TokenHash,
		&client.InvocationPolicy.Mode,
		&client.InvocationPolicy.AllowedTargets,
		&client.InvocationPolicyRevision,
		&client.InvocationPolicyOperationID,
		&client.InvocationPolicyRequestFingerprint,
		&client.CreatedBy,
		&client.UpdatedBy,
		&client.CreatedAt,
		&client.UpdatedAt,
	)
	return cloneClient(client), err
}

func insertClientAudit(ctx context.Context, tx pgx.Tx, workspaceID string, id string, kind string, detail string, actor string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO client_registry_audit (workspace_id, client_id, kind, detail, actor)
VALUES ($1, $2, $3, $4, $5)
`, workspaceID, id, kind, detail, actor)
	return err
}

func clientPostgresError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: external key already exists", ErrConflict)
	}
	return err
}
