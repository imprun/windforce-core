package state

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

func (s *LocalStore) CreateHeldHumanTask(ctx context.Context, candidate HumanTask) (HumanTask, bool, error) {
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
	err = s.update(ctx, func(snapshot *Snapshot, txNow time.Time) error {
		job, ok := snapshot.Jobs[task.JobID]
		if !ok || normalizedJobWorkspace("", job) != task.WorkspaceID || job.RunID != task.RunID {
			return fmt.Errorf("%w: running job %q", ErrNotFound, task.JobID)
		}
		if job.State != JobRunning || job.Attempt != task.Attempt || job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.After(txNow) {
			return fmt.Errorf("%w: job %q is not actively leased", ErrInvalidLease, task.JobID)
		}
		for _, existing := range snapshot.HumanTasks {
			if !humanTaskMatchesHoldKey(existing, task) {
				continue
			}
			if existing.RequestFingerprint != task.RequestFingerprint {
				return fmt.Errorf("%w: human task key %q was reused with a different request", ErrConflict, task.Key)
			}
			stored = existing
			return nil
		}
		if _, exists := snapshot.HumanTasks[task.ID]; exists {
			return fmt.Errorf("%w: human task %q already exists", ErrConflict, task.ID)
		}
		task.CreatedAt = txNow
		task.UpdatedAt = txNow
		snapshot.HumanTasks[task.ID] = task
		run := snapshot.Runs[task.RunID]
		appendEvent(snapshot, task.RunID, "human_task.created", eventPayload(run.CorrelationID, map[string]any{
			"jobId": task.JobID, "taskId": task.ID, "mode": string(task.Mode), "kind": task.Kind,
		}), txNow)
		stored = task
		created = true
		return nil
	})
	return humanTaskMetadata(stored), created, err
}

func (s *LocalStore) ListHumanTasks(ctx context.Context, query HumanTaskListQuery) ([]HumanTask, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	workspaceID := contract.NormalizeWorkspace(query.WorkspaceID)
	limit := query.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	tasks := make([]HumanTask, 0)
	for _, task := range snapshot.HumanTasks {
		if task.Mode != HumanTaskModeHold || task.WorkspaceID != workspaceID || query.State != "" && task.State != query.State {
			continue
		}
		tasks = append(tasks, humanTaskMetadata(task))
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].CreatedAt.Equal(tasks[j].CreatedAt) {
			return tasks[i].ID > tasks[j].ID
		}
		return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
	})
	if len(tasks) > limit {
		tasks = tasks[:limit]
	}
	return tasks, nil
}

func (s *LocalStore) GetHumanTaskForWorkspace(ctx context.Context, workspaceID string, taskID string) (HumanTask, error) {
	task, err := s.GetHumanTask(ctx, strings.TrimSpace(taskID))
	if err != nil {
		return HumanTask{}, err
	}
	if err := validateHeldTaskOwnership(task, workspaceID); err != nil {
		return HumanTask{}, err
	}
	return humanTaskMetadata(task), nil
}

func (s *LocalStore) GetHeldHumanTaskDecision(ctx context.Context, workspaceID string, taskID string) (HumanTaskDecisionResult, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	task, err := s.GetHumanTask(ctx, strings.TrimSpace(taskID))
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
	result.Decision = HumanTaskDecision{
		Outcome:   task.DecisionOutcome,
		Value:     plain,
		DecidedAt: valueOrZero(task.DecidedAt),
	}
	return result, nil
}

func (s *LocalStore) DecideHeldHumanTask(ctx context.Context, workspaceID string, taskID string, candidate HumanTaskDecision) (HumanTaskDecisionResult, error) {
	decision, err := normalizeHumanTaskDecision(candidate)
	if err != nil {
		return HumanTaskDecisionResult{}, err
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	encrypted, err := s.encryptInput(ctx, workspaceID, decision.Value)
	if err != nil {
		return HumanTaskDecisionResult{}, err
	}
	var result HumanTaskDecisionResult
	expired := false
	replayed := false
	var storedDecisionEncrypted json.RawMessage
	err = s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		task, ok := snapshot.HumanTasks[strings.TrimSpace(taskID)]
		if !ok || validateHeldTaskOwnership(task, workspaceID) != nil {
			return fmt.Errorf("%w: human task %q", ErrNotFound, taskID)
		}
		if task.State == HumanTaskDecided {
			if task.DecisionIdempotencyKey != decision.IdempotencyKey || task.DecisionFingerprint != decision.Fingerprint {
				return fmt.Errorf("%w: human task %q already has a decision", ErrConflict, task.ID)
			}
			result.Task = humanTaskMetadata(task)
			result.Decision = HumanTaskDecision{Outcome: task.DecisionOutcome, DecidedAt: valueOrZero(task.DecidedAt)}
			result.Replayed = true
			replayed = true
			storedDecisionEncrypted = cloneRaw(task.DecisionEncrypted)
			return nil
		}
		if task.State != HumanTaskPending {
			return fmt.Errorf("%w: human task %q is %s", ErrInvalidState, task.ID, task.State)
		}
		if task.ExpiresAt != nil && !task.ExpiresAt.After(now) {
			expireHeldTask(snapshot, &task, HumanTaskCauseDeadline, now)
			result.Task = humanTaskMetadata(task)
			expired = true
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
		snapshot.HumanTasks[task.ID] = task
		run := snapshot.Runs[task.RunID]
		appendEvent(snapshot, task.RunID, "human_task.decided", eventPayload(run.CorrelationID, map[string]any{
			"jobId": task.JobID, "taskId": task.ID, "outcome": string(task.DecisionOutcome), "actor": task.DecidedBy,
		}), now)
		result.Task = humanTaskMetadata(task)
		result.Decision = HumanTaskDecision{Outcome: task.DecisionOutcome, DecidedAt: now}
		return nil
	})
	if err != nil {
		return HumanTaskDecisionResult{}, err
	}
	if expired {
		return result, nil
	}
	if replayed {
		plain, err := s.decryptInput(ctx, workspaceID, storedDecisionEncrypted)
		if err != nil {
			return HumanTaskDecisionResult{}, err
		}
		result.Decision.Value = plain
	} else {
		result.Decision.Value = cloneRaw(decision.Value)
	}
	result.Decision.IdempotencyKey = decision.IdempotencyKey
	return result, nil
}

func (s *LocalStore) ExpireHeldHumanTask(ctx context.Context, workspaceID string, taskID string, cause string) (HumanTask, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	var result HumanTask
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		task, ok := snapshot.HumanTasks[strings.TrimSpace(taskID)]
		if !ok || validateHeldTaskOwnership(task, workspaceID) != nil {
			return fmt.Errorf("%w: human task %q", ErrNotFound, taskID)
		}
		if task.State == HumanTaskExpired {
			result = humanTaskMetadata(task)
			return nil
		}
		if task.State != HumanTaskPending {
			return fmt.Errorf("%w: human task %q is %s", ErrInvalidState, task.ID, task.State)
		}
		expireHeldTask(snapshot, &task, strings.TrimSpace(cause), now)
		result = humanTaskMetadata(task)
		return nil
	})
	return result, err
}

func (s *LocalStore) ExpireDueHeldHumanTasks(ctx context.Context, now time.Time, limit int) (int64, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	preflight, err := s.Load(ctx)
	if err != nil {
		return 0, err
	}
	if !localSnapshotHasDueHeldHumanTask(preflight, now) {
		return 0, nil
	}
	type dueTask struct {
		id        string
		expiresAt time.Time
	}
	var expired int64
	err = s.updateWithClock(ctx, func() time.Time { return now }, func(snapshot *Snapshot, txNow time.Time) error {
		due := make([]dueTask, 0)
		for id, task := range snapshot.HumanTasks {
			if task.Mode != HumanTaskModeHold || task.State != HumanTaskPending || task.ExpiresAt == nil || task.ExpiresAt.After(txNow) {
				continue
			}
			due = append(due, dueTask{id: id, expiresAt: task.ExpiresAt.UTC()})
		}
		sort.Slice(due, func(i, j int) bool {
			if due[i].expiresAt.Equal(due[j].expiresAt) {
				return due[i].id < due[j].id
			}
			return due[i].expiresAt.Before(due[j].expiresAt)
		})
		if len(due) > limit {
			due = due[:limit]
		}
		if len(due) == 0 {
			return errSkipLocalStateWrite
		}
		for _, candidate := range due {
			task := snapshot.HumanTasks[candidate.id]
			expireHeldTask(snapshot, &task, HumanTaskCauseDeadline, txNow)
			expired++
		}
		return nil
	})
	return expired, err
}

func localSnapshotHasDueHeldHumanTask(snapshot Snapshot, now time.Time) bool {
	// The sweep uses this only as a lock-free negative preflight. A possible due
	// task is rechecked under the write lock; a concurrently created task
	// linearizes after this empty snapshot and is observed by the next sweep.
	for _, task := range snapshot.HumanTasks {
		if task.Mode == HumanTaskModeHold && task.State == HumanTaskPending && task.ExpiresAt != nil && !task.ExpiresAt.After(now) {
			return true
		}
	}
	return false
}

func (s *LocalStore) CancelHeldHumanTasksForJob(ctx context.Context, workspaceID string, jobID string, cause string) error {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	return s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		cancelHeldHumanTasksInSnapshot(snapshot, workspaceID, jobID, strings.TrimSpace(cause), now)
		return nil
	})
}

func cancelHeldHumanTasksInSnapshot(snapshot *Snapshot, workspaceID string, jobID string, cause string, now time.Time) {
	for id, task := range snapshot.HumanTasks {
		if task.Mode != HumanTaskModeHold || task.WorkspaceID != workspaceID || task.JobID != jobID || task.State != HumanTaskPending {
			continue
		}
		cancelHeldTask(snapshot, &task, cause, now)
		snapshot.HumanTasks[id] = task
	}
}

func valueOrZero(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func expireHeldTask(snapshot *Snapshot, task *HumanTask, cause string, now time.Time) {
	task.State = HumanTaskExpired
	task.TerminalCause = cause
	task.UpdatedAt = now
	snapshot.HumanTasks[task.ID] = *task
	run := snapshot.Runs[task.RunID]
	appendEvent(snapshot, task.RunID, "human_task.expired", eventPayload(run.CorrelationID, map[string]any{
		"jobId": task.JobID, "taskId": task.ID, "cause": task.TerminalCause,
	}), now)
}

func cancelHeldTask(snapshot *Snapshot, task *HumanTask, cause string, now time.Time) {
	task.State = HumanTaskCanceled
	task.TerminalCause = cause
	task.UpdatedAt = now
	snapshot.HumanTasks[task.ID] = *task
	run := snapshot.Runs[task.RunID]
	appendEvent(snapshot, task.RunID, "human_task.canceled", eventPayload(run.CorrelationID, map[string]any{
		"jobId": task.JobID, "taskId": task.ID, "cause": task.TerminalCause,
	}), now)
}
