package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

const (
	HumanTaskCauseDeadline      = "human_task_deadline"
	HumanTaskCauseRunCanceled   = "run_canceled"
	HumanTaskCauseActionTimeout = "action_timeout"
	HumanTaskCauseWorkerLost    = "worker_lost"
	HumanTaskCauseJobCompleted  = "job_completed"
)

func normalizeHeldHumanTask(task HumanTask, now time.Time) (HumanTask, error) {
	task.WorkspaceID = contract.NormalizeWorkspace(task.WorkspaceID)
	task.Key = strings.TrimSpace(task.Key)
	task.Kind = strings.TrimSpace(task.Kind)
	task.Title = strings.TrimSpace(task.Title)
	task.Description = strings.TrimSpace(task.Description)
	task.RequestFingerprint = strings.TrimSpace(task.RequestFingerprint)
	if task.Mode == "" {
		task.Mode = HumanTaskModeHold
	}
	if task.Kind == "" {
		task.Kind = "form"
	}
	if task.ID == "" {
		task.ID = NewID("human")
	}
	if task.Mode != HumanTaskModeHold {
		return HumanTask{}, fmt.Errorf("%w: human task mode must be hold", ErrInvalidState)
	}
	if task.RunID == "" || task.JobID == "" || task.Attempt <= 0 {
		return HumanTask{}, errors.New("run_id, job_id, and a positive attempt are required")
	}
	if task.Key == "" || task.RequestFingerprint == "" {
		return HumanTask{}, errors.New("key and request fingerprint are required")
	}
	if len(task.Key) > 200 || len(task.Title) > 500 || len(task.Description) > 4000 {
		return HumanTask{}, errors.New("key, title, or description exceeds the HumanTask limit")
	}
	if task.Title == "" {
		return HumanTask{}, errors.New("title is required")
	}
	if task.Kind != "form" {
		return HumanTask{}, errors.New("kind must be form")
	}
	if len(task.Schema) == 0 {
		task.Schema = json.RawMessage(`{"type":"object"}`)
	}
	if !json.Valid(task.Schema) {
		return HumanTask{}, errors.New("schema is not valid JSON")
	}
	if len(task.Schema) > 1<<20 || len(task.Presentation) > 256<<10 || len(task.PrivateContext) > 1<<20 {
		return HumanTask{}, errors.New("HumanTask schema, presentation, or private context exceeds the size limit")
	}
	if len(task.Presentation) > 0 && !json.Valid(task.Presentation) {
		return HumanTask{}, errors.New("presentation is not valid JSON")
	}
	if len(task.PrivateContext) > 0 && !json.Valid(task.PrivateContext) {
		return HumanTask{}, errors.New("private context is not valid JSON")
	}
	if task.ExpiresAt != nil && !task.ExpiresAt.After(now) {
		return HumanTask{}, errors.New("expires_at must be in the future")
	}
	task.State = HumanTaskPending
	task.CreatedAt = nonZeroTime(task.CreatedAt, now)
	task.UpdatedAt = nonZeroTime(task.UpdatedAt, task.CreatedAt)
	return task, nil
}

func normalizeHumanTaskDecision(decision HumanTaskDecision) (HumanTaskDecision, error) {
	decision.IdempotencyKey = strings.TrimSpace(decision.IdempotencyKey)
	decision.Fingerprint = strings.TrimSpace(decision.Fingerprint)
	decision.Actor = strings.TrimSpace(decision.Actor)
	if decision.IdempotencyKey == "" || decision.Fingerprint == "" {
		return HumanTaskDecision{}, errors.New("idempotency key and fingerprint are required")
	}
	switch decision.Outcome {
	case HumanTaskOutcomeSubmit:
		if len(decision.Value) == 0 {
			decision.Value = json.RawMessage("null")
		}
	case HumanTaskOutcomeCancel:
		decision.Value = json.RawMessage("null")
	default:
		return HumanTaskDecision{}, errors.New("outcome must be submit or cancel")
	}
	if !json.Valid(decision.Value) {
		return HumanTaskDecision{}, errors.New("decision value is not valid JSON")
	}
	if len(decision.Value) > 1<<20 {
		return HumanTaskDecision{}, errors.New("decision value exceeds the size limit")
	}
	return decision, nil
}

func humanTaskMetadata(task HumanTask) HumanTask {
	task.HasPrivateContext = len(task.PrivateContextEncrypted) > 0
	task.HasDecision = len(task.DecisionEncrypted) > 0
	task.PrivateContext = nil
	task.PrivateContextEncrypted = nil
	task.Decision = nil
	task.DecisionEncrypted = nil
	task.ResumeInput = nil
	task.DecisionIdempotencyKey = ""
	task.DecisionFingerprint = ""
	task.RequestFingerprint = ""
	return task
}

func humanTaskMatchesHoldKey(task HumanTask, candidate HumanTask) bool {
	return task.Mode == HumanTaskModeHold &&
		task.WorkspaceID == candidate.WorkspaceID &&
		task.JobID == candidate.JobID &&
		task.Attempt == candidate.Attempt &&
		task.Key == candidate.Key
}

func validateHeldTaskOwnership(task HumanTask, workspaceID string) error {
	if task.Mode != HumanTaskModeHold || task.WorkspaceID != contract.NormalizeWorkspace(workspaceID) {
		return fmt.Errorf("%w: human task %q", ErrNotFound, task.ID)
	}
	return nil
}

func humanTaskTerminalCause(result contract.JobResult) string {
	if result.Interruption == nil {
		return HumanTaskCauseJobCompleted
	}
	switch result.Interruption.Cause {
	case contract.InterruptionActionTimeout:
		return HumanTaskCauseActionTimeout
	case contract.InterruptionRunCanceled:
		return HumanTaskCauseRunCanceled
	case contract.InterruptionLeaseLost, contract.InterruptionWorkerShutdown:
		return HumanTaskCauseWorkerLost
	default:
		return HumanTaskCauseJobCompleted
	}
}
