package state

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
	wfcrypto "github.com/imprun/windforce-core/internal/crypto"
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

func (s *PostgresStore) CreateWorkspace(ctx context.Context, workspaceID string, name string, actor string) (Workspace, error) {
	var (
		wrappedDEK string
		kekVersion int32
	)
	if strings.TrimSpace(s.SecretKey) != "" {
		var err error
		wrappedDEK, kekVersion, err = wfcrypto.NewWrappedDEK(s.SecretKey)
		if err != nil {
			return Workspace{}, fmt.Errorf("create workspace data-encryption key: %w", err)
		}
	}
	var created Workspace
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var err error
		created, err = scanWorkspace(tx.QueryRow(ctx, `
INSERT INTO workspace_registry (id, display_name, status, created_by, updated_by)
VALUES ($1, $2, $3, $4, $4)
RETURNING `+workspaceColumns, workspaceID, name, WorkspaceActive, actor))
		if err != nil {
			return workspacePostgresError(err)
		}
		if wrappedDEK != "" {
			if _, err := tx.Exec(ctx, `
INSERT INTO workspace_key (workspace_id, key, kek_version)
VALUES ($1, $2, $3)
`, workspaceID, wrappedDEK, kekVersion); err != nil {
				return fmt.Errorf("store workspace data-encryption key: %w", err)
			}
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

func (s *PostgresStore) DeleteWorkspace(ctx context.Context, workspaceID string, actor string) error {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	if workspaceID == contract.DefaultWorkspace {
		return fmt.Errorf("%w: default workspace cannot be deleted", ErrInvalidState)
	}
	return s.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := scanWorkspace(tx.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspace_registry WHERE id=$1 FOR UPDATE`, workspaceID)); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}

		rows, err := tx.Query(ctx, `
SELECT id
FROM runs
WHERE COALESCE(NULLIF(deployment->>'workspace', ''), 'default')=$1
UNION
SELECT run_id
FROM jobs
WHERE COALESCE(NULLIF(payload->>'workspace', ''), NULLIF(payload->'deployment'->>'workspace', ''), 'default')=$1
`, workspaceID)
		if err != nil {
			return err
		}
		runIDs := []string{}
		for rows.Next() {
			var runID string
			if err := rows.Scan(&runID); err != nil {
				rows.Close()
				return err
			}
			runIDs = append(runIDs, runID)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(runIDs) > 0 {
			for _, query := range []string{
				`DELETE FROM human_tasks WHERE run_id = ANY($1)`,
				`DELETE FROM run_events WHERE run_id = ANY($1)`,
				`DELETE FROM jobs WHERE run_id = ANY($1)`,
				`DELETE FROM runs WHERE id = ANY($1)`,
			} {
				if _, err := tx.Exec(ctx, query, runIDs); err != nil {
					return err
				}
			}
		}

		queries := []string{
			`DELETE FROM job_logs WHERE workspace_id=$1`,
			`DELETE FROM job_state WHERE workspace_id=$1`,
			`DELETE FROM variable WHERE workspace_id=$1`,
			`DELETE FROM resource WHERE workspace_id=$1`,
			`DELETE FROM resource_type WHERE workspace_id=$1`,
			`DELETE FROM secret_access_audit WHERE workspace_id=$1`,
			`DELETE FROM webhook_delivery WHERE workspace_id=$1`,
			`DELETE FROM webhook_audit WHERE workspace_id=$1`,
			`DELETE FROM webhook_subscription WHERE workspace_id=$1`,
			`DELETE FROM control_plane_event WHERE workspace_id=$1`,
			`DELETE FROM http_route_binding_audit WHERE workspace_id=$1`,
			`DELETE FROM http_route_binding WHERE workspace_id=$1`,
			`DELETE FROM trigger_audit WHERE workspace_id=$1`,
			`DELETE FROM trigger_delivery WHERE workspace_id=$1`,
			`DELETE FROM trigger_definition WHERE workspace_id=$1`,
			`DELETE FROM input_config WHERE workspace_id=$1`,
			`DELETE FROM input_config_audit WHERE workspace_id=$1`,
			`DELETE FROM client_registry_audit WHERE workspace_id=$1`,
			`DELETE FROM client_registry WHERE workspace_id=$1`,
			`DELETE FROM service_principal_audit WHERE workspace_id=$1`,
			`DELETE FROM service_principal WHERE workspace_id=$1`,
			`DELETE FROM control_release_candidate WHERE workspace_id=$1`,
			`DELETE FROM control_source_operation_lease WHERE workspace_id=$1`,
			`DELETE FROM control_source_release_marker WHERE workspace_id=$1`,
			`DELETE FROM control_active_release WHERE workspace_id=$1`,
			`DELETE FROM control_audit WHERE workspace_id=$1`,
			`DELETE FROM control_release_history WHERE workspace_id=$1`,
			`DELETE FROM workspace_audit WHERE workspace_id=$1`,
			`DELETE FROM workspace_token WHERE workspace_id=$1`,
			`DELETE FROM workspace_key WHERE workspace_id=$1`,
		}
		for _, query := range queries {
			if _, err := tx.Exec(ctx, query, workspaceID); err != nil {
				return err
			}
		}
		command, err := tx.Exec(ctx, `DELETE FROM workspace_registry WHERE id=$1`, workspaceID)
		if err != nil {
			return err
		}
		if command.RowsAffected() != 1 {
			return ErrNotFound
		}
		return nil
	})
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
