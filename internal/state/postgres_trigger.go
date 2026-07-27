package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/imprun/windforce-core/internal/contract"
)

const triggerColumns = `
	workspace_id, id, name, kind, enabled, app_key, action_key, credential_ref,
	config, secret_config_encrypted, created_by, updated_by, created_at, updated_at, deleted_at
`

type triggerScanner interface {
	Scan(dest ...any) error
}

func (s *PostgresStore) ListTriggers(ctx context.Context, workspaceID string) ([]TriggerDefinition, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	rows, err := s.pool.Query(ctx, `
SELECT `+triggerColumns+`
FROM trigger_definition
WHERE workspace_id=$1 AND deleted_at IS NULL
ORDER BY name, id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]TriggerDefinition, 0)
	for rows.Next() {
		definition, err := s.scanTrigger(ctx, rows)
		if err != nil {
			return nil, err
		}
		items = append(items, definition)
	}
	return items, rows.Err()
}

func (s *PostgresStore) GetTrigger(ctx context.Context, workspaceID string, id string) (TriggerDefinition, error) {
	return s.scanTrigger(ctx, s.pool.QueryRow(ctx, `
SELECT `+triggerColumns+`
FROM trigger_definition
WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL`, contract.NormalizeWorkspace(workspaceID), strings.TrimSpace(id)))
}

func (s *PostgresStore) CreateTrigger(ctx context.Context, definition TriggerDefinition, actor string) (TriggerDefinition, error) {
	definition, err := prepareTriggerDefinition(definition, actor, true)
	if err != nil {
		return TriggerDefinition{}, err
	}
	encrypted, err := s.encryptInput(ctx, definition.WorkspaceID, definition.SecretConfig)
	if err != nil {
		return TriggerDefinition{}, err
	}
	var created TriggerDefinition
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		created, _, err = scanTriggerRecord(tx.QueryRow(ctx, `
INSERT INTO trigger_definition (
	workspace_id, id, name, kind, enabled, app_key, action_key, credential_ref,
	config, secret_config_encrypted, created_by, updated_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11)
RETURNING `+triggerColumns,
			definition.WorkspaceID, definition.ID, definition.Name, definition.Kind, definition.Enabled,
			definition.AppKey, definition.ActionKey, definition.CredentialRef, definition.Config, encrypted, normalizedActor(actor)))
		if err != nil {
			return triggerPostgresError(err)
		}
		return insertTriggerAudit(ctx, tx, created, "created", triggerAuditDetail(created), actor)
	})
	if err == nil {
		created.SecretConfig = cloneRaw(definition.SecretConfig)
	}
	return created, err
}

func (s *PostgresStore) UpdateTrigger(ctx context.Context, definition TriggerDefinition, actor string) (TriggerDefinition, error) {
	definition, err := prepareTriggerDefinition(definition, actor, false)
	if err != nil {
		return TriggerDefinition{}, err
	}
	var encrypted json.RawMessage
	if len(definition.SecretConfig) > 0 {
		encrypted, err = s.encryptInput(ctx, definition.WorkspaceID, definition.SecretConfig)
		if err != nil {
			return TriggerDefinition{}, err
		}
	}
	var updated TriggerDefinition
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		if len(encrypted) == 0 {
			err = tx.QueryRow(ctx, `
SELECT secret_config_encrypted
FROM trigger_definition
WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL
FOR UPDATE`, definition.WorkspaceID, definition.ID).Scan(&encrypted)
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: trigger %q", ErrNotFound, definition.ID)
			}
			if err != nil {
				return err
			}
		}
		updated, _, err = scanTriggerRecord(tx.QueryRow(ctx, `
UPDATE trigger_definition
SET name=$3, kind=$4, enabled=$5, app_key=$6, action_key=$7, credential_ref=$8,
	config=$9, secret_config_encrypted=$10, updated_by=$11, updated_at=now()
WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL
RETURNING `+triggerColumns,
			definition.WorkspaceID, definition.ID, definition.Name, definition.Kind, definition.Enabled,
			definition.AppKey, definition.ActionKey, definition.CredentialRef, definition.Config, encrypted, normalizedActor(actor)))
		if err != nil {
			return triggerPostgresError(err)
		}
		return insertTriggerAudit(ctx, tx, updated, "updated", triggerAuditDetail(updated), actor)
	})
	if err != nil {
		return TriggerDefinition{}, err
	}
	return s.GetTrigger(ctx, definition.WorkspaceID, definition.ID)
}

func (s *PostgresStore) SetTriggerEnabled(ctx context.Context, workspaceID string, id string, enabled bool, actor string) (TriggerDefinition, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	var updated TriggerDefinition
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var err error
		updated, _, err = scanTriggerRecord(tx.QueryRow(ctx, `
UPDATE trigger_definition
SET enabled=$3, updated_by=$4, updated_at=now()
WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL
RETURNING `+triggerColumns, workspaceID, strings.TrimSpace(id), enabled, normalizedActor(actor)))
		if err != nil {
			return triggerPostgresError(err)
		}
		kind := "disabled"
		if enabled {
			kind = "enabled"
		}
		return insertTriggerAudit(ctx, tx, updated, kind, "", actor)
	})
	if err != nil {
		return TriggerDefinition{}, err
	}
	return s.GetTrigger(ctx, workspaceID, id)
}

func (s *PostgresStore) DeleteTrigger(ctx context.Context, workspaceID string, id string, actor string) error {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	return s.withTx(ctx, func(tx pgx.Tx) error {
		var deleted TriggerDefinition
		var err error
		deleted, _, err = scanTriggerRecord(tx.QueryRow(ctx, `
UPDATE trigger_definition
SET enabled=false, updated_by=$3, updated_at=now(), deleted_at=now()
WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL
RETURNING `+triggerColumns, workspaceID, strings.TrimSpace(id), normalizedActor(actor)))
		if err != nil {
			return triggerPostgresError(err)
		}
		if err := requestPostgresHTTPRouteBindingsForTrigger(ctx, tx, deleted.WorkspaceID, deleted.ID, actor); err != nil {
			return err
		}
		return insertTriggerAudit(ctx, tx, deleted, "deleted", "", actor)
	})
}

func (s *PostgresStore) ListTriggerAudit(ctx context.Context, workspaceID string, id string) ([]TriggerAudit, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, workspace_id, trigger_id, kind, detail, actor, created_at
FROM trigger_audit
WHERE workspace_id=$1 AND trigger_id=$2
ORDER BY created_at, id`, contract.NormalizeWorkspace(workspaceID), strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]TriggerAudit, 0)
	for rows.Next() {
		var item TriggerAudit
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.TriggerID, &item.Kind, &item.Detail, &item.Actor, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) UpsertTriggerDelivery(ctx context.Context, delivery TriggerDelivery) (TriggerDelivery, error) {
	delivery.WorkspaceID = contract.NormalizeWorkspace(delivery.WorkspaceID)
	delivery.TriggerID = strings.TrimSpace(delivery.TriggerID)
	delivery.DeliveryID = strings.TrimSpace(delivery.DeliveryID)
	delivery.State = strings.TrimSpace(delivery.State)
	delivery.ErrorSummary = truncateTriggerError(delivery.ErrorSummary)
	if delivery.TriggerID == "" || delivery.DeliveryID == "" || delivery.State == "" {
		return TriggerDelivery{}, fmt.Errorf("%w: trigger_id, delivery_id, and state are required", ErrInvalidState)
	}
	if delivery.ID == "" {
		delivery.ID = NewID("trd")
	}
	if delivery.Attempt <= 0 {
		delivery.Attempt = 1
	}
	var stored TriggerDelivery
	err := s.pool.QueryRow(ctx, `
INSERT INTO trigger_delivery (
	id, workspace_id, trigger_id, delivery_id, correlation_id, state, run_id, attempt, error_summary, scheduled_for
) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,$10)
ON CONFLICT (workspace_id, trigger_id, delivery_id) DO UPDATE SET
	correlation_id=EXCLUDED.correlation_id,
	state=EXCLUDED.state,
	run_id=EXCLUDED.run_id,
	attempt=GREATEST(trigger_delivery.attempt + 1, EXCLUDED.attempt),
	error_summary=EXCLUDED.error_summary,
	scheduled_for=EXCLUDED.scheduled_for,
	updated_at=now()
RETURNING id, workspace_id, trigger_id, delivery_id, correlation_id, state, COALESCE(run_id,''), attempt, error_summary, scheduled_for, created_at, updated_at`,
		delivery.ID, delivery.WorkspaceID, delivery.TriggerID, delivery.DeliveryID, delivery.CorrelationID,
		delivery.State, delivery.RunID, delivery.Attempt, delivery.ErrorSummary, delivery.ScheduledFor).
		Scan(&stored.ID, &stored.WorkspaceID, &stored.TriggerID, &stored.DeliveryID, &stored.CorrelationID,
			&stored.State, &stored.RunID, &stored.Attempt, &stored.ErrorSummary, &stored.ScheduledFor,
			&stored.CreatedAt, &stored.UpdatedAt)
	return stored, err
}

func (s *PostgresStore) ListTriggerDeliveries(ctx context.Context, workspaceID string, triggerID string, limit int) ([]TriggerDelivery, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
SELECT id, workspace_id, trigger_id, delivery_id, correlation_id, state, COALESCE(run_id,''), attempt, error_summary, scheduled_for, created_at, updated_at
FROM trigger_delivery
WHERE workspace_id=$1 AND trigger_id=$2
ORDER BY updated_at DESC, id DESC
LIMIT $3`, contract.NormalizeWorkspace(workspaceID), strings.TrimSpace(triggerID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]TriggerDelivery, 0)
	for rows.Next() {
		var item TriggerDelivery
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.TriggerID, &item.DeliveryID, &item.CorrelationID,
			&item.State, &item.RunID, &item.Attempt, &item.ErrorSummary, &item.ScheduledFor,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) scanTrigger(ctx context.Context, scanner triggerScanner) (TriggerDefinition, error) {
	definition, encrypted, err := scanTriggerRecord(scanner)
	if err != nil {
		return TriggerDefinition{}, err
	}
	definition.SecretConfig, err = s.decryptInput(ctx, definition.WorkspaceID, encrypted)
	return definition, err
}

func scanTriggerRecord(scanner triggerScanner) (TriggerDefinition, json.RawMessage, error) {
	var definition TriggerDefinition
	var encrypted json.RawMessage
	err := scanner.Scan(
		&definition.WorkspaceID, &definition.ID, &definition.Name, &definition.Kind, &definition.Enabled,
		&definition.AppKey, &definition.ActionKey, &definition.CredentialRef, &definition.Config, &encrypted,
		&definition.CreatedBy, &definition.UpdatedBy, &definition.CreatedAt, &definition.UpdatedAt, &definition.DeletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return TriggerDefinition{}, nil, fmt.Errorf("%w: trigger not found", ErrNotFound)
	}
	if err != nil {
		return TriggerDefinition{}, nil, err
	}
	return definition, encrypted, nil
}

func insertTriggerAudit(ctx context.Context, tx pgx.Tx, definition TriggerDefinition, kind string, detail string, actor string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO trigger_audit (id, workspace_id, trigger_id, kind, detail, actor)
VALUES ($1,$2,$3,$4,$5,$6)`,
		NewID("tra"), definition.WorkspaceID, definition.ID, kind, detail, normalizedActor(actor))
	return err
}

func triggerPostgresError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: trigger not found", ErrNotFound)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: trigger name already exists", ErrConflict)
	}
	return err
}
