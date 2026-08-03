package state

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) CreateHeldHumanTask(ctx context.Context, candidate HumanTask) (HumanTask, bool, error) {
	now := time.Now().UTC()
	task, err := normalizeHeldHumanTask(candidate, now)
	if err != nil {
		return HumanTask{}, false, err
	}
	if len(task.PrivateContext) > 0 {
		task.PrivateContextEncrypted, err = s.encryptInput(ctx, task.WorkspaceID, task.PrivateContext)
		if err != nil {
			return HumanTask{}, false, err
		}
	}
	task.PrivateContext = nil
	var stored HumanTask
	created := false
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		job, err := scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id=$1 FOR UPDATE`, task.JobID))
		if errors.Is(err, pgx.ErrNoRows) || err == nil && (normalizedJobWorkspace("", job) != task.WorkspaceID || job.RunID != task.RunID) {
			return fmt.Errorf("%w: running job %q", ErrNotFound, task.JobID)
		}
		if err != nil {
			return err
		}
		if job.State != JobRunning || job.Attempt != task.Attempt || job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.After(now) {
			return fmt.Errorf("%w: job %q is not actively leased", ErrInvalidLease, task.JobID)
		}
		existing, err := scanHumanTask(tx.QueryRow(ctx, `
SELECT `+humanTaskColumns+`
FROM human_tasks
WHERE workspace_id=$1 AND job_id=$2 AND attempt=$3 AND task_key=$4 AND mode='hold'
FOR UPDATE
`, task.WorkspaceID, task.JobID, task.Attempt, task.Key))
		if err == nil {
			if existing.RequestFingerprint != task.RequestFingerprint {
				return fmt.Errorf("%w: human task key %q was reused with a different request", ErrConflict, task.Key)
			}
			stored = existing
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		task.CreatedAt = now
		task.UpdatedAt = now
		command, err := tx.Exec(ctx, `
INSERT INTO human_tasks (
    id, workspace_id, run_id, job_id, attempt, task_key, request_fingerprint,
    mode, kind, state, title, description, schema, presentation,
    private_context_encrypted, created_at, updated_at, expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    $8, $9, $10, $11, $12, $13, $14,
    $15, $16, $17, $18
)
ON CONFLICT DO NOTHING
`, task.ID, task.WorkspaceID, task.RunID, task.JobID, task.Attempt, task.Key, task.RequestFingerprint,
			string(task.Mode), task.Kind, string(task.State), task.Title, nullableString(task.Description), nullableRaw(task.Schema), nullableRaw(task.Presentation),
			nullableRaw(task.PrivateContextEncrypted), task.CreatedAt, task.UpdatedAt, task.ExpiresAt)
		if err != nil {
			return err
		}
		if command.RowsAffected() == 0 {
			existing, err = scanHumanTask(tx.QueryRow(ctx, `
SELECT `+humanTaskColumns+`
FROM human_tasks
WHERE workspace_id=$1 AND job_id=$2 AND attempt=$3 AND task_key=$4 AND mode='hold'
FOR UPDATE
`, task.WorkspaceID, task.JobID, task.Attempt, task.Key))
			if err != nil || existing.RequestFingerprint != task.RequestFingerprint {
				return fmt.Errorf("%w: human task key %q conflicts", ErrConflict, task.Key)
			}
			stored = existing
			return nil
		}
		run, err := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id=$1`, task.RunID))
		if err != nil {
			return err
		}
		if err := insertEvent(ctx, tx, task.RunID, "human_task.created", eventPayload(run.CorrelationID, map[string]any{
			"jobId": task.JobID, "taskId": task.ID, "mode": string(task.Mode), "kind": task.Kind,
		})); err != nil {
			return err
		}
		stored = task
		created = true
		return nil
	})
	return humanTaskMetadata(stored), created, err
}

func (s *PostgresStore) ListHumanTasks(ctx context.Context, query HumanTaskListQuery) ([]HumanTask, error) {
	workspaceID := contract.NormalizeWorkspace(query.WorkspaceID)
	limit := query.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
SELECT `+humanTaskColumns+`
FROM human_tasks
WHERE workspace_id=$1 AND mode='hold' AND ($2='' OR state=$2)
ORDER BY created_at DESC, id DESC
LIMIT $3
`, workspaceID, string(query.State), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := make([]HumanTask, 0)
	for rows.Next() {
		task, err := scanHumanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, humanTaskMetadata(task))
	}
	return tasks, rows.Err()
}

func (s *PostgresStore) GetHumanTaskForWorkspace(ctx context.Context, workspaceID string, taskID string) (HumanTask, error) {
	task, err := scanHumanTask(s.pool.QueryRow(ctx, `
SELECT `+humanTaskColumns+` FROM human_tasks WHERE workspace_id=$1 AND id=$2
`, contract.NormalizeWorkspace(workspaceID), strings.TrimSpace(taskID)))
	if errors.Is(err, pgx.ErrNoRows) {
		return HumanTask{}, fmt.Errorf("%w: human task %q", ErrNotFound, taskID)
	}
	if err != nil {
		return HumanTask{}, err
	}
	if err := validateHeldTaskOwnership(task, workspaceID); err != nil {
		return HumanTask{}, err
	}
	return humanTaskMetadata(task), nil
}

func (s *PostgresStore) GetHeldHumanTaskDecision(ctx context.Context, workspaceID string, taskID string) (HumanTaskDecisionResult, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	task, err := scanHumanTask(s.pool.QueryRow(ctx, `
SELECT `+humanTaskColumns+` FROM human_tasks WHERE workspace_id=$1 AND id=$2
`, workspaceID, strings.TrimSpace(taskID)))
	if errors.Is(err, pgx.ErrNoRows) {
		return HumanTaskDecisionResult{}, fmt.Errorf("%w: human task %q", ErrNotFound, taskID)
	}
	if err != nil {
		return HumanTaskDecisionResult{}, err
	}
	if err := validateHeldTaskOwnership(task, workspaceID); err != nil {
		return HumanTaskDecisionResult{}, err
	}
	result := HumanTaskDecisionResult{Task: humanTaskMetadata(task)}
	if task.State != HumanTaskDecided {
		return result, nil
	}
	plain, err := s.decryptInput(ctx, workspaceID, task.DecisionEncrypted)
	if err != nil {
		return HumanTaskDecisionResult{}, err
	}
	result.Decision = HumanTaskDecision{Outcome: task.DecisionOutcome, Value: plain, DecidedAt: valueOrZero(task.DecidedAt)}
	return result, nil
}

func (s *PostgresStore) DecideHeldHumanTask(ctx context.Context, workspaceID string, taskID string, candidate HumanTaskDecision) (HumanTaskDecisionResult, error) {
	decision, err := normalizeHumanTaskDecision(candidate)
	if err != nil {
		return HumanTaskDecisionResult{}, err
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	encrypted, err := s.encryptInput(ctx, workspaceID, decision.Value)
	if err != nil {
		return HumanTaskDecisionResult{}, err
	}
	var stored HumanTask
	var replayed bool
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		task, err := scanHumanTask(tx.QueryRow(ctx, `
SELECT `+humanTaskColumns+` FROM human_tasks WHERE workspace_id=$1 AND id=$2 FOR UPDATE
`, workspaceID, strings.TrimSpace(taskID)))
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: human task %q", ErrNotFound, taskID)
		}
		if err != nil {
			return err
		}
		if err := validateHeldTaskOwnership(task, workspaceID); err != nil {
			return err
		}
		if task.State == HumanTaskDecided {
			if task.DecisionIdempotencyKey != decision.IdempotencyKey || task.DecisionFingerprint != decision.Fingerprint {
				return fmt.Errorf("%w: human task %q already has a decision", ErrConflict, task.ID)
			}
			stored = task
			replayed = true
			return nil
		}
		if task.State != HumanTaskPending {
			return fmt.Errorf("%w: human task %q is %s", ErrInvalidState, task.ID, task.State)
		}
		now := time.Now().UTC()
		if task.ExpiresAt != nil && !task.ExpiresAt.After(now) {
			task.State = HumanTaskExpired
			task.TerminalCause = HumanTaskCauseDeadline
			task.UpdatedAt = now
			if _, err := tx.Exec(ctx, `UPDATE human_tasks SET state=$1, terminal_cause=$2, updated_at=$3 WHERE id=$4`, string(task.State), task.TerminalCause, now, task.ID); err != nil {
				return err
			}
			if err := insertHumanTaskTerminalEvent(ctx, tx, task, "human_task.expired"); err != nil {
				return err
			}
			stored = task
			return nil
		}
		task.State = HumanTaskDecided
		task.DecisionOutcome = decision.Outcome
		task.DecisionEncrypted = cloneRaw(encrypted)
		task.DecisionIdempotencyKey = decision.IdempotencyKey
		task.DecisionFingerprint = decision.Fingerprint
		task.DecidedBy = decision.Actor
		task.DecidedAt = &now
		task.UpdatedAt = now
		if _, err := tx.Exec(ctx, `
UPDATE human_tasks
SET state=$1, decision_outcome=$2, decision_encrypted=$3,
    decision_idempotency_key=$4, decision_fingerprint=$5, decided_by=$6,
    decided_at=$7, updated_at=$7
WHERE id=$8
`, string(task.State), string(task.DecisionOutcome), nullableRaw(task.DecisionEncrypted), task.DecisionIdempotencyKey,
			task.DecisionFingerprint, task.DecidedBy, now, task.ID); err != nil {
			return err
		}
		run, err := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id=$1`, task.RunID))
		if err != nil {
			return err
		}
		if err := insertEvent(ctx, tx, task.RunID, "human_task.decided", eventPayload(run.CorrelationID, map[string]any{
			"jobId": task.JobID, "taskId": task.ID, "outcome": string(task.DecisionOutcome), "actor": task.DecidedBy,
		})); err != nil {
			return err
		}
		stored = task
		return nil
	})
	if err != nil {
		return HumanTaskDecisionResult{}, err
	}
	result := HumanTaskDecisionResult{Task: humanTaskMetadata(stored), Replayed: replayed}
	if stored.State != HumanTaskDecided {
		return result, nil
	}
	plain, err := s.decryptInput(ctx, workspaceID, stored.DecisionEncrypted)
	if err != nil {
		return HumanTaskDecisionResult{}, err
	}
	result.Decision = HumanTaskDecision{Outcome: stored.DecisionOutcome, Value: plain, DecidedAt: valueOrZero(stored.DecidedAt)}
	return result, nil
}

func (s *PostgresStore) ExpireHeldHumanTask(ctx context.Context, workspaceID string, taskID string, cause string) (HumanTask, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	var stored HumanTask
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		task, err := scanHumanTask(tx.QueryRow(ctx, `
SELECT `+humanTaskColumns+` FROM human_tasks WHERE workspace_id=$1 AND id=$2 FOR UPDATE
`, workspaceID, strings.TrimSpace(taskID)))
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: human task %q", ErrNotFound, taskID)
		}
		if err != nil {
			return err
		}
		if err := validateHeldTaskOwnership(task, workspaceID); err != nil {
			return err
		}
		if task.State == HumanTaskExpired {
			stored = task
			return nil
		}
		if task.State != HumanTaskPending {
			return fmt.Errorf("%w: human task %q is %s", ErrInvalidState, task.ID, task.State)
		}
		now := time.Now().UTC()
		task.State = HumanTaskExpired
		task.TerminalCause = strings.TrimSpace(cause)
		task.UpdatedAt = now
		if _, err := tx.Exec(ctx, `UPDATE human_tasks SET state=$1, terminal_cause=$2, updated_at=$3 WHERE id=$4`, string(task.State), task.TerminalCause, now, task.ID); err != nil {
			return err
		}
		if err := insertHumanTaskTerminalEvent(ctx, tx, task, "human_task.expired"); err != nil {
			return err
		}
		stored = task
		return nil
	})
	return humanTaskMetadata(stored), err
}

func (s *PostgresStore) CancelHeldHumanTasksForJob(ctx context.Context, workspaceID string, jobID string, cause string) error {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	return s.withTx(ctx, func(tx pgx.Tx) error {
		return cancelHeldHumanTasksPostgresTx(ctx, tx, workspaceID, strings.TrimSpace(jobID), strings.TrimSpace(cause), time.Now().UTC())
	})
}

func cancelHeldHumanTasksPostgresTx(ctx context.Context, tx pgx.Tx, workspaceID string, jobID string, cause string, now time.Time) error {
	rows, err := tx.Query(ctx, `
SELECT `+humanTaskColumns+`
FROM human_tasks
WHERE workspace_id=$1 AND job_id=$2 AND mode='hold' AND state='pending'
FOR UPDATE
`, workspaceID, jobID)
	if err != nil {
		return err
	}
	tasks := make([]HumanTask, 0)
	for rows.Next() {
		task, err := scanHumanTask(rows)
		if err != nil {
			rows.Close()
			return err
		}
		tasks = append(tasks, task)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, task := range tasks {
		task.State = HumanTaskCanceled
		task.TerminalCause = cause
		task.UpdatedAt = now
		if _, err := tx.Exec(ctx, `UPDATE human_tasks SET state=$1, terminal_cause=$2, updated_at=$3 WHERE id=$4`, string(task.State), task.TerminalCause, now, task.ID); err != nil {
			return err
		}
		if err := insertHumanTaskTerminalEvent(ctx, tx, task, "human_task.canceled"); err != nil {
			return err
		}
	}
	return nil
}

func cancelHeldHumanTasksForRunPostgresTx(ctx context.Context, tx pgx.Tx, runID string, cause string, now time.Time) error {
	return cancelHeldHumanTasksByQueryPostgresTx(ctx, tx, `run_id=$1`, []any{runID}, cause, now)
}

func cancelHeldHumanTasksForStuckRunsPostgresTx(ctx context.Context, tx pgx.Tx, cause string, now time.Time) error {
	return cancelHeldHumanTasksByQueryPostgresTx(ctx, tx, `run_id IN (SELECT id FROM stuck_runs)`, nil, cause, now)
}

func cancelHeldHumanTasksByQueryPostgresTx(ctx context.Context, tx pgx.Tx, predicate string, args []any, cause string, now time.Time) error {
	query := `SELECT ` + humanTaskColumns + ` FROM human_tasks WHERE mode='hold' AND state='pending' AND ` + predicate + ` FOR UPDATE`
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return err
	}
	tasks := make([]HumanTask, 0)
	for rows.Next() {
		task, err := scanHumanTask(rows)
		if err != nil {
			rows.Close()
			return err
		}
		tasks = append(tasks, task)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, task := range tasks {
		task.State = HumanTaskCanceled
		task.TerminalCause = cause
		task.UpdatedAt = now
		if _, err := tx.Exec(ctx, `UPDATE human_tasks SET state=$1, terminal_cause=$2, updated_at=$3 WHERE id=$4`, string(task.State), task.TerminalCause, now, task.ID); err != nil {
			return err
		}
		if err := insertHumanTaskTerminalEvent(ctx, tx, task, "human_task.canceled"); err != nil {
			return err
		}
	}
	return nil
}

func insertHumanTaskTerminalEvent(ctx context.Context, tx pgx.Tx, task HumanTask, eventType string) error {
	run, err := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id=$1`, task.RunID))
	if err != nil {
		return err
	}
	return insertEvent(ctx, tx, task.RunID, eventType, eventPayload(run.CorrelationID, map[string]any{
		"jobId": task.JobID, "taskId": task.ID, "cause": task.TerminalCause,
	}))
}
