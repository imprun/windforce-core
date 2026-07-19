package state

import (
	"context"
	"errors"
	"fmt"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const workspaceColumns = `id, display_name, status, token_hash, created_by, updated_by, created_at, updated_at`

func (s *PostgresStore) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+workspaceColumns+` FROM workspace_registry ORDER BY status, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Workspace{}
	for rows.Next() {
		workspace, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, workspace)
	}
	return items, rows.Err()
}

func (s *PostgresStore) GetWorkspace(ctx context.Context, workspaceID string) (Workspace, error) {
	workspace, err := scanWorkspace(s.pool.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspace_registry WHERE id=$1`, contract.NormalizeWorkspace(workspaceID)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	return workspace, err
}

func (s *PostgresStore) CreateWorkspace(ctx context.Context, workspaceID string, name string, tokenHash string, actor string) (Workspace, error) {
	var created Workspace
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var err error
		created, err = scanWorkspace(tx.QueryRow(ctx, `
INSERT INTO workspace_registry (id, display_name, status, token_hash, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $5)
RETURNING `+workspaceColumns, workspaceID, name, WorkspaceActive, tokenHash, actor))
		if err != nil {
			return workspacePostgresError(err)
		}
		return insertWorkspaceAudit(ctx, tx, workspaceID, "created", "", actor)
	})
	return created, err
}

func (s *PostgresStore) UpdateWorkspace(ctx context.Context, workspaceID string, name string, actor string) (Workspace, error) {
	var updated Workspace
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		current, err := scanWorkspace(tx.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspace_registry WHERE id=$1 FOR UPDATE`, workspaceID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if current.Status == WorkspaceArchived {
			return ErrInvalidState
		}
		detail := "no value change"
		if current.Name != name {
			detail = "display name changed"
		}
		updated, err = scanWorkspace(tx.QueryRow(ctx, `
UPDATE workspace_registry SET display_name=$2, updated_by=$3, updated_at=now()
WHERE id=$1 RETURNING `+workspaceColumns, workspaceID, name, actor))
		if err != nil {
			return err
		}
		return insertWorkspaceAudit(ctx, tx, workspaceID, "updated", detail, actor)
	})
	return updated, err
}

func (s *PostgresStore) ArchiveWorkspace(ctx context.Context, workspaceID string, actor string) (Workspace, error) {
	if workspaceID == contract.DefaultWorkspace {
		return Workspace{}, fmt.Errorf("%w: default workspace cannot be archived", ErrInvalidState)
	}
	var archived Workspace
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		current, err := scanWorkspace(tx.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspace_registry WHERE id=$1 FOR UPDATE`, workspaceID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if current.Status == WorkspaceArchived {
			return fmt.Errorf("%w: workspace is already archived", ErrInvalidState)
		}
		archived, err = scanWorkspace(tx.QueryRow(ctx, `
UPDATE workspace_registry SET status=$2, updated_by=$3, updated_at=now()
WHERE id=$1 RETURNING `+workspaceColumns, workspaceID, WorkspaceArchived, actor))
		if err != nil {
			return err
		}
		return insertWorkspaceAudit(ctx, tx, workspaceID, "archived", "", actor)
	})
	return archived, err
}

func (s *PostgresStore) RotateWorkspaceToken(ctx context.Context, workspaceID string, tokenHash string, actor string) (Workspace, error) {
	var updated Workspace
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		current, err := scanWorkspace(tx.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspace_registry WHERE id=$1 FOR UPDATE`, workspaceID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if current.Status == WorkspaceArchived {
			return ErrInvalidState
		}
		updated, err = scanWorkspace(tx.QueryRow(ctx, `
UPDATE workspace_registry SET token_hash=$2, updated_by=$3, updated_at=now()
WHERE id=$1 RETURNING `+workspaceColumns, workspaceID, tokenHash, actor))
		if err != nil {
			return err
		}
		return insertWorkspaceAudit(ctx, tx, workspaceID, "token_rotated", "", actor)
	})
	return updated, err
}

func (s *PostgresStore) ListWorkspaceAudit(ctx context.Context, workspaceID string) ([]WorkspaceAudit, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id::text, workspace_id, kind, detail, actor, created_at
FROM workspace_audit WHERE $1='' OR workspace_id=$1
ORDER BY created_at DESC, id DESC
`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []WorkspaceAudit{}
	for rows.Next() {
		var record WorkspaceAudit
		if err := rows.Scan(&record.ID, &record.WorkspaceID, &record.Kind, &record.Detail, &record.Actor, &record.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, rows.Err()
}

type workspaceScanner interface {
	Scan(dest ...any) error
}

func scanWorkspace(row workspaceScanner) (Workspace, error) {
	var workspace Workspace
	err := row.Scan(&workspace.ID, &workspace.Name, &workspace.Status, &workspace.TokenHash, &workspace.CreatedBy, &workspace.UpdatedBy, &workspace.CreatedAt, &workspace.UpdatedAt)
	return workspace, err
}

func insertWorkspaceAudit(ctx context.Context, tx pgx.Tx, workspaceID string, kind string, detail string, actor string) error {
	_, err := tx.Exec(ctx, `INSERT INTO workspace_audit (workspace_id, kind, detail, actor) VALUES ($1, $2, $3, $4)`, workspaceID, kind, detail, actor)
	return err
}

func workspacePostgresError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: workspace already exists", ErrConflict)
	}
	return err
}
