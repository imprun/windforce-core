package state

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/imprun/windforce-core/internal/contract"
)

const servicePrincipalColumns = `id, workspace_id, name, token_hash, scopes, allowed_targets, created_by, updated_by, created_at, updated_at`

func (s *PostgresStore) ListServicePrincipals(ctx context.Context, workspaceID string) ([]ServicePrincipal, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+servicePrincipalColumns+` FROM service_principal WHERE workspace_id=$1 ORDER BY name, id`, contract.NormalizeWorkspace(workspaceID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	principals := []ServicePrincipal{}
	for rows.Next() {
		principal, scanErr := scanServicePrincipal(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		principals = append(principals, principal)
	}
	return principals, rows.Err()
}

func (s *PostgresStore) GetServicePrincipal(ctx context.Context, workspaceID string, id string) (ServicePrincipal, error) {
	principal, err := scanServicePrincipal(s.pool.QueryRow(ctx, `SELECT `+servicePrincipalColumns+` FROM service_principal WHERE workspace_id=$1 AND id=$2`, contract.NormalizeWorkspace(workspaceID), id))
	if errors.Is(err, pgx.ErrNoRows) {
		return ServicePrincipal{}, ErrNotFound
	}
	return principal, err
}

func (s *PostgresStore) GetServicePrincipalByTokenHash(ctx context.Context, workspaceID string, tokenHash string) (ServicePrincipal, error) {
	principal, err := scanServicePrincipal(s.pool.QueryRow(ctx, `SELECT `+servicePrincipalColumns+` FROM service_principal WHERE workspace_id=$1 AND token_hash=$2 AND token_hash <> ''`, contract.NormalizeWorkspace(workspaceID), tokenHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return ServicePrincipal{}, ErrNotFound
	}
	return principal, err
}

func (s *PostgresStore) CreateServicePrincipal(ctx context.Context, principal ServicePrincipal, tokenHash string, actor string) (ServicePrincipal, error) {
	principal.WorkspaceID = contract.NormalizeWorkspace(principal.WorkspaceID)
	id := NewID("service")
	var created ServicePrincipal
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var createErr error
		created, createErr = scanServicePrincipal(tx.QueryRow(ctx, `
INSERT INTO service_principal (workspace_id, id, name, token_hash, scopes, allowed_targets, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
RETURNING `+servicePrincipalColumns, principal.WorkspaceID, id, principal.Name, tokenHash, principal.Scopes, principal.AllowedTargets, actor))
		if createErr != nil {
			return servicePrincipalPostgresError(createErr)
		}
		return insertServicePrincipalAudit(ctx, tx, principal.WorkspaceID, id, "created", "", actor)
	})
	return created, err
}

func (s *PostgresStore) UpdateServicePrincipal(ctx context.Context, principal ServicePrincipal, actor string) (ServicePrincipal, error) {
	principal.WorkspaceID = contract.NormalizeWorkspace(principal.WorkspaceID)
	var updated ServicePrincipal
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var updateErr error
		updated, updateErr = scanServicePrincipal(tx.QueryRow(ctx, `
UPDATE service_principal
SET name=$3, scopes=$4, allowed_targets=$5, updated_by=$6, updated_at=now()
WHERE workspace_id=$1 AND id=$2
RETURNING `+servicePrincipalColumns, principal.WorkspaceID, principal.ID, principal.Name, principal.Scopes, principal.AllowedTargets, actor))
		if errors.Is(updateErr, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if updateErr != nil {
			return servicePrincipalPostgresError(updateErr)
		}
		return insertServicePrincipalAudit(ctx, tx, principal.WorkspaceID, principal.ID, "updated", "authorization changed", actor)
	})
	return updated, err
}

func (s *PostgresStore) RotateServicePrincipalToken(ctx context.Context, workspaceID string, id string, tokenHash string, actor string) (ServicePrincipal, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	if tokenHash == "" {
		return ServicePrincipal{}, ErrInvalidState
	}
	var updated ServicePrincipal
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var rotateErr error
		updated, rotateErr = scanServicePrincipal(tx.QueryRow(ctx, `
UPDATE service_principal SET token_hash=$3, updated_by=$4, updated_at=now()
WHERE workspace_id=$1 AND id=$2
RETURNING `+servicePrincipalColumns, workspaceID, id, tokenHash, actor))
		if errors.Is(rotateErr, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if rotateErr != nil {
			return servicePrincipalPostgresError(rotateErr)
		}
		return insertServicePrincipalAudit(ctx, tx, workspaceID, id, "token_rotated", "", actor)
	})
	return updated, err
}

func (s *PostgresStore) RevokeServicePrincipalToken(ctx context.Context, workspaceID string, id string, actor string) (ServicePrincipal, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	var updated ServicePrincipal
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var revokeErr error
		updated, revokeErr = scanServicePrincipal(tx.QueryRow(ctx, `
UPDATE service_principal SET token_hash='', updated_by=$3, updated_at=now()
WHERE workspace_id=$1 AND id=$2 AND token_hash <> ''
RETURNING `+servicePrincipalColumns, workspaceID, id, actor))
		if errors.Is(revokeErr, pgx.ErrNoRows) {
			var currentHash string
			getErr := tx.QueryRow(ctx, `SELECT token_hash FROM service_principal WHERE workspace_id=$1 AND id=$2`, workspaceID, id).Scan(&currentHash)
			if errors.Is(getErr, pgx.ErrNoRows) {
				return ErrNotFound
			}
			if getErr != nil {
				return getErr
			}
			return ErrInvalidState
		}
		if revokeErr != nil {
			return revokeErr
		}
		return insertServicePrincipalAudit(ctx, tx, workspaceID, id, "token_revoked", "", actor)
	})
	return updated, err
}

func (s *PostgresStore) DeleteServicePrincipal(ctx context.Context, workspaceID string, id string, actor string) error {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	return s.withTx(ctx, func(tx pgx.Tx) error {
		result, err := tx.Exec(ctx, `DELETE FROM service_principal WHERE workspace_id=$1 AND id=$2 AND token_hash=''`, workspaceID, id)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			var tokenHash string
			getErr := tx.QueryRow(ctx, `SELECT token_hash FROM service_principal WHERE workspace_id=$1 AND id=$2`, workspaceID, id).Scan(&tokenHash)
			if errors.Is(getErr, pgx.ErrNoRows) {
				return ErrNotFound
			}
			if getErr != nil {
				return getErr
			}
			return fmt.Errorf("%w: revoke the active service principal token before deleting the principal", ErrConflict)
		}
		return insertServicePrincipalAudit(ctx, tx, workspaceID, id, "deleted", "", actor)
	})
}

func (s *PostgresStore) ListServicePrincipalAudit(ctx context.Context, workspaceID string, id string) ([]ServicePrincipalAudit, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id::text, workspace_id, service_principal_id, kind, detail, actor, created_at
FROM service_principal_audit
WHERE workspace_id=$1 AND ($2='' OR service_principal_id=$2)
ORDER BY created_at DESC, id DESC
`, contract.NormalizeWorkspace(workspaceID), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []ServicePrincipalAudit{}
	for rows.Next() {
		var record ServicePrincipalAudit
		if scanErr := rows.Scan(&record.ID, &record.WorkspaceID, &record.ServicePrincipalID, &record.Kind, &record.Detail, &record.Actor, &record.CreatedAt); scanErr != nil {
			return nil, scanErr
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

type servicePrincipalScanner interface {
	Scan(dest ...any) error
}

func scanServicePrincipal(row servicePrincipalScanner) (ServicePrincipal, error) {
	var principal ServicePrincipal
	err := row.Scan(
		&principal.ID,
		&principal.WorkspaceID,
		&principal.Name,
		&principal.TokenHash,
		&principal.Scopes,
		&principal.AllowedTargets,
		&principal.CreatedBy,
		&principal.UpdatedBy,
		&principal.CreatedAt,
		&principal.UpdatedAt,
	)
	return principal, err
}

func insertServicePrincipalAudit(ctx context.Context, tx pgx.Tx, workspaceID string, id string, kind string, detail string, actor string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO service_principal_audit (workspace_id, service_principal_id, kind, detail, actor)
VALUES ($1, $2, $3, $4, $5)
`, workspaceID, id, kind, detail, actor)
	return err
}

func servicePrincipalPostgresError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: service principal token already exists", ErrConflict)
	}
	return err
}
