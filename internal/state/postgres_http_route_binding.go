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

const httpRouteBindingColumns = `
	workspace_id, id, trigger_id, hostname, path, visibility, provider, state,
	public_url, error_summary, generation, observed_generation,
	created_by, updated_by, created_at, updated_at, delete_requested_at, deleted_at
`

type httpRouteBindingScanner interface {
	Scan(dest ...any) error
}

func (s *PostgresStore) ListHTTPRouteBindings(ctx context.Context, workspaceID string, triggerID string, includeDeleted bool) ([]HTTPRouteBinding, error) {
	rows, err := s.pool.Query(ctx, `
SELECT `+httpRouteBindingColumns+`
FROM http_route_binding
WHERE workspace_id=$1
  AND ($2='' OR trigger_id=$2)
  AND ($3 OR deleted_at IS NULL)
ORDER BY trigger_id, hostname, path, id`,
		contract.NormalizeWorkspace(workspaceID), strings.TrimSpace(triggerID), includeDeleted)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]HTTPRouteBinding, 0)
	for rows.Next() {
		binding, err := scanHTTPRouteBinding(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, binding)
	}
	return items, rows.Err()
}

func (s *PostgresStore) GetHTTPRouteBinding(ctx context.Context, workspaceID string, triggerID string, id string) (HTTPRouteBinding, error) {
	return scanHTTPRouteBinding(s.pool.QueryRow(ctx, `
SELECT `+httpRouteBindingColumns+`
FROM http_route_binding
WHERE workspace_id=$1 AND trigger_id=$2 AND id=$3 AND deleted_at IS NULL`,
		contract.NormalizeWorkspace(workspaceID), strings.TrimSpace(triggerID), strings.TrimSpace(id)))
}

func (s *PostgresStore) CreateHTTPRouteBinding(ctx context.Context, binding HTTPRouteBinding, actor string) (HTTPRouteBinding, error) {
	binding, err := prepareHTTPRouteBinding(binding, actor, true)
	if err != nil {
		return HTTPRouteBinding{}, err
	}
	var created HTTPRouteBinding
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		var kind string
		err := tx.QueryRow(ctx, `
SELECT kind
FROM trigger_definition
WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL
FOR SHARE`, binding.WorkspaceID, binding.TriggerID).Scan(&kind)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: trigger %q", ErrNotFound, binding.TriggerID)
		}
		if err != nil {
			return err
		}
		if kind != "webhook" {
			return fmt.Errorf("%w: HTTP route bindings require a webhook trigger", ErrInvalidState)
		}
		binding.State = HTTPRouteBindingPending
		binding.Generation = 1
		binding.ObservedGeneration = 0
		created, err = scanHTTPRouteBinding(tx.QueryRow(ctx, `
INSERT INTO http_route_binding (
	workspace_id, id, trigger_id, hostname, path, visibility, provider, state,
	public_url, error_summary, generation, observed_generation, created_by, updated_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'','',$9,0,$10,$10)
RETURNING `+httpRouteBindingColumns,
			binding.WorkspaceID, binding.ID, binding.TriggerID, binding.Hostname, binding.Path,
			binding.Visibility, binding.Provider, binding.State, binding.Generation, normalizedActor(actor)))
		if err != nil {
			return httpRouteBindingPostgresError(err)
		}
		return insertHTTPRouteBindingAudit(ctx, tx, created, "created", httpRouteBindingAuditDetail(created), actor)
	})
	return created, err
}

func (s *PostgresStore) UpdateHTTPRouteBinding(ctx context.Context, binding HTTPRouteBinding, actor string) (HTTPRouteBinding, error) {
	binding, err := prepareHTTPRouteBinding(binding, actor, false)
	if err != nil {
		return HTTPRouteBinding{}, err
	}
	var updated HTTPRouteBinding
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		existing, err := scanHTTPRouteBinding(tx.QueryRow(ctx, `
SELECT `+httpRouteBindingColumns+`
FROM http_route_binding
WHERE workspace_id=$1 AND trigger_id=$2 AND id=$3 AND deleted_at IS NULL
FOR UPDATE`, binding.WorkspaceID, binding.TriggerID, binding.ID))
		if err != nil {
			return err
		}
		if existing.DeleteRequestedAt != nil {
			return fmt.Errorf("%w: HTTP route binding %q is deleting", ErrInvalidState, binding.ID)
		}
		if sameHTTPRouteBindingDesired(existing, binding) {
			updated = existing
			return nil
		}
		updated, err = scanHTTPRouteBinding(tx.QueryRow(ctx, `
UPDATE http_route_binding
SET hostname=$4, path=$5, visibility=$6, provider=$7,
	state='pending', public_url='', error_summary='', generation=generation+1,
	updated_by=$8, updated_at=now()
WHERE workspace_id=$1 AND trigger_id=$2 AND id=$3 AND deleted_at IS NULL
RETURNING `+httpRouteBindingColumns,
			binding.WorkspaceID, binding.TriggerID, binding.ID, binding.Hostname, binding.Path,
			binding.Visibility, binding.Provider, normalizedActor(actor)))
		if err != nil {
			return httpRouteBindingPostgresError(err)
		}
		return insertHTTPRouteBindingAudit(ctx, tx, updated, "updated", httpRouteBindingAuditDetail(updated), actor)
	})
	return updated, err
}

func (s *PostgresStore) RequestDeleteHTTPRouteBinding(ctx context.Context, workspaceID string, triggerID string, id string, actor string) (HTTPRouteBinding, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	triggerID = strings.TrimSpace(triggerID)
	id = strings.TrimSpace(id)
	var updated HTTPRouteBinding
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		existing, err := scanHTTPRouteBinding(tx.QueryRow(ctx, `
SELECT `+httpRouteBindingColumns+`
FROM http_route_binding
WHERE workspace_id=$1 AND trigger_id=$2 AND id=$3 AND deleted_at IS NULL
FOR UPDATE`, workspaceID, triggerID, id))
		if err != nil {
			return err
		}
		if existing.DeleteRequestedAt != nil {
			updated = existing
			return nil
		}
		updated, err = scanHTTPRouteBinding(tx.QueryRow(ctx, `
UPDATE http_route_binding
SET state='deleting', error_summary='', generation=generation+1,
	delete_requested_at=now(), updated_by=$4, updated_at=now()
WHERE workspace_id=$1 AND trigger_id=$2 AND id=$3 AND deleted_at IS NULL
RETURNING `+httpRouteBindingColumns, workspaceID, triggerID, id, normalizedActor(actor)))
		if err != nil {
			return httpRouteBindingPostgresError(err)
		}
		return insertHTTPRouteBindingAudit(ctx, tx, updated, "delete_requested", "", actor)
	})
	return updated, err
}

func (s *PostgresStore) UpdateHTTPRouteBindingStatus(ctx context.Context, workspaceID string, id string, status HTTPRouteBindingStatus, actor string) (HTTPRouteBinding, error) {
	status, err := prepareHTTPRouteBindingStatus(status)
	if err != nil {
		return HTTPRouteBinding{}, err
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	id = strings.TrimSpace(id)
	var updated HTTPRouteBinding
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		existing, err := scanHTTPRouteBinding(tx.QueryRow(ctx, `
SELECT `+httpRouteBindingColumns+`
FROM http_route_binding
WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL
FOR UPDATE`, workspaceID, id))
		if err != nil {
			return err
		}
		if status.ObservedGeneration < existing.ObservedGeneration {
			return fmt.Errorf("%w: observed generation moved backwards", ErrConflict)
		}
		if status.ObservedGeneration > existing.Generation {
			return fmt.Errorf("%w: observed generation exceeds desired generation", ErrConflict)
		}
		if status.State == HTTPRouteBindingReady && status.ObservedGeneration != existing.Generation {
			return fmt.Errorf("%w: stale generation cannot be ready", ErrConflict)
		}
		if status.State == HTTPRouteBindingDeleted && existing.DeleteRequestedAt == nil {
			return fmt.Errorf("%w: binding is not deleting", ErrInvalidState)
		}
		previousState := existing.State
		publicURL := status.PublicURL
		errorSummary := truncateHTTPRouteBindingError(status.ErrorSummary)
		if status.State == HTTPRouteBindingDeleted {
			publicURL = ""
			errorSummary = ""
		}
		updated, err = scanHTTPRouteBinding(tx.QueryRow(ctx, `
UPDATE http_route_binding
SET state=$3, public_url=$4, error_summary=$5, observed_generation=$6,
	updated_by=$7, updated_at=now(),
	deleted_at=CASE WHEN $3='deleted' THEN now() ELSE deleted_at END
WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL
RETURNING `+httpRouteBindingColumns,
			workspaceID, id, status.State, publicURL, errorSummary, status.ObservedGeneration, normalizedActor(actor)))
		if err != nil {
			return httpRouteBindingPostgresError(err)
		}
		detail := fmt.Sprintf("state=%s previous=%s observed_generation=%d", updated.State, previousState, updated.ObservedGeneration)
		return insertHTTPRouteBindingAudit(ctx, tx, updated, "status_changed", detail, actor)
	})
	return updated, err
}

func (s *PostgresStore) ListHTTPRouteBindingAudit(ctx context.Context, workspaceID string, triggerID string, id string) ([]HTTPRouteBindingAudit, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, workspace_id, trigger_id, binding_id, kind, detail, actor, created_at
FROM http_route_binding_audit
WHERE workspace_id=$1 AND trigger_id=$2 AND binding_id=$3
ORDER BY created_at, id`,
		contract.NormalizeWorkspace(workspaceID), strings.TrimSpace(triggerID), strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]HTTPRouteBindingAudit, 0)
	for rows.Next() {
		var item HTTPRouteBindingAudit
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.TriggerID, &item.BindingID, &item.Kind, &item.Detail, &item.Actor, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanHTTPRouteBinding(scanner httpRouteBindingScanner) (HTTPRouteBinding, error) {
	var binding HTTPRouteBinding
	err := scanner.Scan(
		&binding.WorkspaceID, &binding.ID, &binding.TriggerID, &binding.Hostname, &binding.Path,
		&binding.Visibility, &binding.Provider, &binding.State, &binding.PublicURL, &binding.ErrorSummary,
		&binding.Generation, &binding.ObservedGeneration, &binding.CreatedBy, &binding.UpdatedBy,
		&binding.CreatedAt, &binding.UpdatedAt, &binding.DeleteRequestedAt, &binding.DeletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return HTTPRouteBinding{}, fmt.Errorf("%w: HTTP route binding not found", ErrNotFound)
	}
	return binding, err
}

func insertHTTPRouteBindingAudit(ctx context.Context, tx pgx.Tx, binding HTTPRouteBinding, kind string, detail string, actor string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO http_route_binding_audit (
	id, workspace_id, trigger_id, binding_id, kind, detail, actor
) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		NewID("hra"), binding.WorkspaceID, binding.TriggerID, binding.ID,
		kind, detail, normalizedActor(actor))
	return err
}

func httpRouteBindingPostgresError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: HTTP route binding not found", ErrNotFound)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: HTTP route binding address already exists", ErrConflict)
	}
	return err
}

func requestPostgresHTTPRouteBindingsForTrigger(ctx context.Context, tx pgx.Tx, workspaceID string, triggerID string, actor string) error {
	rows, err := tx.Query(ctx, `
UPDATE http_route_binding
SET state='deleting', error_summary='', generation=generation+1,
	delete_requested_at=now(), updated_by=$3, updated_at=now()
WHERE workspace_id=$1 AND trigger_id=$2 AND deleted_at IS NULL AND delete_requested_at IS NULL
RETURNING `+httpRouteBindingColumns,
		contract.NormalizeWorkspace(workspaceID), strings.TrimSpace(triggerID), normalizedActor(actor))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		binding, err := scanHTTPRouteBinding(rows)
		if err != nil {
			return err
		}
		if err := insertHTTPRouteBindingAudit(ctx, tx, binding, "delete_requested", "trigger deleted", actor); err != nil {
			return err
		}
	}
	return rows.Err()
}
