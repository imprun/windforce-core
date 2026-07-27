package state

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const workspaceTokenColumns = `id, workspace_id, name, token_hash, created_by, updated_by, created_at, updated_at, revoked_at`

func (s *PostgresStore) ListWorkspaceTokens(ctx context.Context, workspaceID string) ([]WorkspaceToken, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+workspaceTokenColumns+` FROM workspace_token WHERE workspace_id=$1 ORDER BY name, id`, contract.NormalizeWorkspace(workspaceID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []WorkspaceToken{}
	for rows.Next() {
		token, err := scanWorkspaceToken(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, token)
	}
	return items, rows.Err()
}

func (s *PostgresStore) GetWorkspaceToken(ctx context.Context, workspaceID string, id string) (WorkspaceToken, error) {
	token, err := scanWorkspaceToken(s.pool.QueryRow(ctx, `SELECT `+workspaceTokenColumns+` FROM workspace_token WHERE workspace_id=$1 AND id=$2`, contract.NormalizeWorkspace(workspaceID), strings.TrimSpace(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkspaceToken{}, ErrNotFound
	}
	return token, err
}

func (s *PostgresStore) GetWorkspaceTokenByTokenHash(ctx context.Context, workspaceID string, tokenHash string) (WorkspaceToken, error) {
	token, err := scanWorkspaceToken(s.pool.QueryRow(ctx, `SELECT `+workspaceTokenColumns+` FROM workspace_token WHERE workspace_id=$1 AND token_hash=$2 AND revoked_at IS NULL`, contract.NormalizeWorkspace(workspaceID), tokenHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkspaceToken{}, ErrNotFound
	}
	return token, err
}

func (s *PostgresStore) CreateWorkspaceToken(ctx context.Context, workspaceID string, name string, tokenHash string, actor string) (WorkspaceToken, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	var created WorkspaceToken
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		workspace, err := scanWorkspace(tx.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspace_registry WHERE id=$1 FOR UPDATE`, workspaceID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if workspace.Status == WorkspaceArchived {
			return ErrInvalidState
		}
		created, err = scanWorkspaceToken(tx.QueryRow(ctx, `
INSERT INTO workspace_token (id, workspace_id, name, token_hash, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $5)
RETURNING `+workspaceTokenColumns, NewID("workspace_token"), workspaceID, strings.TrimSpace(name), tokenHash, actor))
		if err != nil {
			return workspaceTokenPostgresError(err)
		}
		return insertWorkspaceAudit(ctx, tx, workspaceID, "token_created", created.ID+": "+created.Name, actor)
	})
	return created, err
}

func (s *PostgresStore) RotateWorkspaceToken(ctx context.Context, workspaceID string, id string, tokenHash string, actor string) (WorkspaceToken, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	var updated WorkspaceToken
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		workspace, err := scanWorkspace(tx.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspace_registry WHERE id=$1 FOR UPDATE`, workspaceID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if workspace.Status == WorkspaceArchived {
			return ErrInvalidState
		}
		current, err := scanWorkspaceToken(tx.QueryRow(ctx, `SELECT `+workspaceTokenColumns+` FROM workspace_token WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if current.RevokedAt != nil {
			return ErrInvalidState
		}
		updated, err = scanWorkspaceToken(tx.QueryRow(ctx, `
UPDATE workspace_token SET token_hash=$3, updated_by=$4, updated_at=now()
WHERE workspace_id=$1 AND id=$2
RETURNING `+workspaceTokenColumns, workspaceID, id, tokenHash, actor))
		if err != nil {
			return workspaceTokenPostgresError(err)
		}
		return insertWorkspaceAudit(ctx, tx, workspaceID, "token_rotated", id+": "+updated.Name, actor)
	})
	return updated, err
}

func (s *PostgresStore) RevokeWorkspaceToken(ctx context.Context, workspaceID string, id string, actor string) (WorkspaceToken, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	var updated WorkspaceToken
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		current, err := scanWorkspaceToken(tx.QueryRow(ctx, `SELECT `+workspaceTokenColumns+` FROM workspace_token WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if current.RevokedAt != nil || current.TokenHash == "" {
			return ErrInvalidState
		}
		updated, err = scanWorkspaceToken(tx.QueryRow(ctx, `
UPDATE workspace_token SET token_hash='', revoked_at=now(), updated_by=$3, updated_at=now()
WHERE workspace_id=$1 AND id=$2
RETURNING `+workspaceTokenColumns, workspaceID, id, actor))
		if err != nil {
			return err
		}
		return insertWorkspaceAudit(ctx, tx, workspaceID, "token_revoked", id+": "+updated.Name, actor)
	})
	return updated, err
}

type workspaceTokenScanner interface {
	Scan(dest ...any) error
}

func scanWorkspaceToken(row workspaceTokenScanner) (WorkspaceToken, error) {
	var token WorkspaceToken
	err := row.Scan(
		&token.ID, &token.WorkspaceID, &token.Name, &token.TokenHash,
		&token.CreatedBy, &token.UpdatedBy, &token.CreatedAt, &token.UpdatedAt, &token.RevokedAt,
	)
	return token, err
}

func workspaceTokenPostgresError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: workspace token already exists", ErrConflict)
	}
	return err
}
