package state

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

func insertHumanTaskLifecycleWebhookEvent(ctx context.Context, tx pgx.Tx, task HumanTask, eventType string, occurredAt time.Time) error {
	run, err := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id=$1`, task.RunID))
	if err != nil {
		return err
	}
	lifecycleEvent, err := prepareHumanTaskLifecycleEvent(task, run, eventType, occurredAt)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(lifecycleEvent)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO control_plane_event (id, workspace_id, event_type, subject, body, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
`, lifecycleEvent.ID, task.WorkspaceID, lifecycleEvent.Type, lifecycleEvent.Subject, raw, lifecycleEvent.Time); err != nil {
		return err
	}
	return postgresEnqueueEventDeliveries(ctx, tx, lifecycleEvent, task.WorkspaceID, run.App, occurredAt)
}
