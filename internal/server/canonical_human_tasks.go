package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/resourceconfig"
	"github.com/imprun/windforce-core/internal/state"
)

const (
	defaultHumanTaskTimeout    = 2 * time.Minute
	maxHumanTaskTimeout        = 24 * time.Hour
	humanTaskReconcileInterval = 15 * time.Second
)

type humanTaskWaitRequest struct {
	Key            string          `json:"key"`
	Kind           string          `json:"kind"`
	Title          string          `json:"title"`
	Description    string          `json:"description,omitempty"`
	InputSchema    json.RawMessage `json:"input_schema"`
	Presentation   json.RawMessage `json:"presentation,omitempty"`
	PrivateContext json.RawMessage `json:"private_context,omitempty"`
	TimeoutMs      int64           `json:"timeout_ms,omitempty"`
}

type humanTaskDecisionRequest struct {
	Outcome state.HumanTaskOutcome `json:"outcome"`
	Value   json.RawMessage        `json:"value,omitempty"`
}

type humanTaskView struct {
	ID                string                 `json:"id"`
	WorkspaceID       string                 `json:"workspace_id"`
	RunID             string                 `json:"run_id"`
	JobID             string                 `json:"job_id"`
	Attempt           int                    `json:"attempt"`
	App               string                 `json:"app,omitempty"`
	Action            string                 `json:"action,omitempty"`
	Key               string                 `json:"key"`
	Mode              state.HumanTaskMode    `json:"mode"`
	Kind              string                 `json:"kind"`
	State             state.HumanTaskState   `json:"state"`
	Title             string                 `json:"title"`
	Description       string                 `json:"description,omitempty"`
	InputSchema       json.RawMessage        `json:"input_schema,omitempty"`
	Presentation      json.RawMessage        `json:"presentation,omitempty"`
	HasPrivateContext bool                   `json:"has_private_context"`
	DecisionOutcome   state.HumanTaskOutcome `json:"decision_outcome,omitempty"`
	DecidedBy         string                 `json:"decided_by,omitempty"`
	TerminalCause     string                 `json:"terminal_cause,omitempty"`
	CreatedAt         time.Time              `json:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at"`
	DecidedAt         *time.Time             `json:"decided_at,omitempty"`
	ExpiresAt         *time.Time             `json:"expires_at,omitempty"`
}

func (h *Handler) humanTaskView(r *http.Request, workspaceID string, task state.HumanTask) humanTaskView {
	view := humanTaskResponse(task)
	job, run, found, err := h.store.GetJob(r.Context(), workspaceID, task.JobID)
	if err == nil && found && job.RunID == run.ID {
		view.App = run.App
		view.Action = run.Action
	}
	return view
}

func humanTaskResponse(task state.HumanTask) humanTaskView {
	return humanTaskView{
		ID:                task.ID,
		WorkspaceID:       task.WorkspaceID,
		RunID:             task.RunID,
		JobID:             task.JobID,
		Attempt:           task.Attempt,
		Key:               task.Key,
		Mode:              task.Mode,
		Kind:              task.Kind,
		State:             task.State,
		Title:             task.Title,
		Description:       task.Description,
		InputSchema:       append(json.RawMessage(nil), task.Schema...),
		Presentation:      append(json.RawMessage(nil), task.Presentation...),
		HasPrivateContext: task.HasPrivateContext,
		DecisionOutcome:   task.DecisionOutcome,
		DecidedBy:         task.DecidedBy,
		TerminalCause:     task.TerminalCause,
		CreatedAt:         task.CreatedAt,
		UpdatedAt:         task.UpdatedAt,
		DecidedAt:         task.DecidedAt,
		ExpiresAt:         task.ExpiresAt,
	}
}

func (h *Handler) handleCanonicalHumanTaskAPI(w http.ResponseWriter, r *http.Request, parts []string) bool {
	if len(parts) == 4 && parts[0] == "api" && parts[1] == "w" && parts[3] == "human-tasks" && r.Method == http.MethodGet {
		handleState := state.HumanTaskState(strings.TrimSpace(r.URL.Query().Get("state")))
		if handleState != "" && !validHumanTaskState(handleState) {
			writeError(w, http.StatusBadRequest, "invalid HumanTask state")
			return true
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		tasks, err := h.store.ListHumanTasks(r.Context(), state.HumanTaskListQuery{WorkspaceID: parts[2], State: handleState, Limit: limit})
		if err != nil {
			writeStateError(w, err)
			return true
		}
		views := make([]humanTaskView, 0, len(tasks))
		for _, task := range tasks {
			if !h.humanTaskTargetAllowed(r, parts[2], task) {
				continue
			}
			views = append(views, h.humanTaskView(r, parts[2], task))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": views})
		return true
	}
	if len(parts) == 5 && parts[0] == "api" && parts[1] == "w" && parts[3] == "human-tasks" && r.Method == http.MethodGet {
		task, err := h.store.GetHumanTaskForWorkspace(r.Context(), parts[2], parts[4])
		if err != nil {
			writeStateError(w, err)
			return true
		}
		if !h.humanTaskTargetAllowed(r, parts[2], task) {
			writeError(w, http.StatusForbidden, "service principal target does not allow this HumanTask")
			return true
		}
		writeJSON(w, http.StatusOK, h.humanTaskView(r, parts[2], task))
		return true
	}
	if len(parts) == 6 && parts[0] == "api" && parts[1] == "w" && parts[3] == "human-tasks" && parts[5] == "decision" && r.Method == http.MethodPost {
		h.handleHumanTaskDecision(w, r, parts[2], parts[4])
		return true
	}
	return false
}

func (h *Handler) handleHumanTaskWait(w http.ResponseWriter, r *http.Request, workspaceID string) {
	principal := jobPrincipalFrom(r.Context())
	if principal == nil || principal.Workspace != workspaceID {
		writeError(w, http.StatusForbidden, "HumanTask wait requires a Job-scoped token")
		return
	}
	var request humanTaskWaitRequest
	if err := readRequiredJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid HumanTask request")
		return
	}
	request.Key = strings.TrimSpace(request.Key)
	request.Kind = strings.TrimSpace(request.Kind)
	request.Title = strings.TrimSpace(request.Title)
	if request.Key == "" || len(request.Key) > 200 {
		writeError(w, http.StatusBadRequest, "key is required and must not exceed 200 characters")
		return
	}
	if request.Kind == "" {
		request.Kind = "form"
	}
	if request.Kind != "form" || request.Title == "" || len(request.Title) > 200 || len(request.Description) > 2000 {
		writeError(w, http.StatusBadRequest, "kind must be form and title/description must be within limits")
		return
	}
	if len(request.InputSchema) == 0 {
		request.InputSchema = json.RawMessage(`{"type":"object"}`)
	}
	if err := resourceconfig.ValidateSchema(request.InputSchema); err != nil {
		writeError(w, http.StatusBadRequest, "input_schema is not a valid JSON Schema")
		return
	}
	if len(request.Presentation) > 0 && !json.Valid(request.Presentation) || len(request.PrivateContext) > 0 && !json.Valid(request.PrivateContext) {
		writeError(w, http.StatusBadRequest, "presentation and private_context must be valid JSON")
		return
	}
	timeout := time.Duration(request.TimeoutMs) * time.Millisecond
	if request.TimeoutMs == 0 {
		timeout = defaultHumanTaskTimeout
	}
	if timeout < time.Second || timeout > maxHumanTaskTimeout {
		writeError(w, http.StatusBadRequest, "timeout_ms must be between 1000 and 86400000")
		return
	}
	job, run, found, err := h.store.GetJob(r.Context(), workspaceID, principal.JobID)
	if err != nil {
		writeStateError(w, err)
		return
	}
	if !found || job.RunID != run.ID || job.State != state.JobRunning || job.Attempt != principal.Attempt {
		writeError(w, http.StatusConflict, "Job is not running under this token attempt")
		return
	}
	expiresAt := time.Now().UTC().Add(timeout)
	fingerprint := humanTaskFingerprint(struct {
		Key            string          `json:"key"`
		Kind           string          `json:"kind"`
		Title          string          `json:"title"`
		Description    string          `json:"description,omitempty"`
		InputSchema    json.RawMessage `json:"input_schema"`
		Presentation   json.RawMessage `json:"presentation,omitempty"`
		PrivateContext json.RawMessage `json:"private_context,omitempty"`
		TimeoutMs      int64           `json:"timeout_ms"`
	}{request.Key, request.Kind, request.Title, request.Description, request.InputSchema, request.Presentation, request.PrivateContext, timeout.Milliseconds()})
	task, _, err := h.store.CreateHeldHumanTask(r.Context(), state.HumanTask{
		WorkspaceID:        workspaceID,
		RunID:              run.ID,
		JobID:              job.ID,
		Attempt:            job.Attempt,
		Key:                request.Key,
		RequestFingerprint: fingerprint,
		Mode:               state.HumanTaskModeHold,
		Kind:               request.Kind,
		Title:              request.Title,
		Description:        request.Description,
		Schema:             request.InputSchema,
		Presentation:       request.Presentation,
		PrivateContext:     request.PrivateContext,
		ExpiresAt:          &expiresAt,
	})
	if err != nil {
		writeStateError(w, err)
		return
	}
	h.waitForHumanTask(w, r, task)
}

func (h *Handler) waitForHumanTask(w http.ResponseWriter, r *http.Request, task state.HumanTask) {
	subscriber, _ := h.store.(state.HumanTaskChangeSubscriber)
	for {
		var changed <-chan struct{}
		cancelSubscription := func() {}
		if subscriber != nil {
			changed, cancelSubscription = subscriber.SubscribeHumanTaskChanges(task.ID)
		}
		result, err := h.store.GetHeldHumanTaskDecision(r.Context(), task.WorkspaceID, task.ID)
		if err != nil {
			cancelSubscription()
			if r.Context().Err() == nil {
				writeStateError(w, err)
			}
			return
		}
		switch result.Task.State {
		case state.HumanTaskDecided:
			cancelSubscription()
			writeJSON(w, http.StatusOK, map[string]any{
				"task_id": result.Task.ID,
				"outcome": result.Decision.Outcome,
				"value":   result.Decision.Value,
			})
			return
		case state.HumanTaskExpired:
			cancelSubscription()
			writeHumanTaskTerminalError(w, http.StatusRequestTimeout, result.Task)
			return
		case state.HumanTaskCanceled:
			cancelSubscription()
			writeHumanTaskTerminalError(w, http.StatusConflict, result.Task)
			return
		}
		now := time.Now().UTC()
		if result.Task.ExpiresAt != nil && !result.Task.ExpiresAt.After(now) {
			cancelSubscription()
			_, _ = h.store.ExpireHeldHumanTask(r.Context(), task.WorkspaceID, task.ID, state.HumanTaskCauseDeadline)
			continue
		}
		job, _, found, jobErr := h.store.GetJob(r.Context(), task.WorkspaceID, task.JobID)
		if jobErr != nil || !found || job.State != state.JobRunning || job.Attempt != task.Attempt || job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.After(now) {
			cancelSubscription()
			cause := state.HumanTaskCauseWorkerLost
			if found && job.State != state.JobRunning {
				cause = state.HumanTaskCauseJobCompleted
			}
			if found && job.CanceledBy != nil {
				cause = state.HumanTaskCauseRunCanceled
			}
			_ = h.store.CancelHeldHumanTasksForJob(r.Context(), task.WorkspaceID, task.JobID, cause)
			continue
		}
		waitFor := humanTaskReconcileInterval
		if result.Task.ExpiresAt != nil {
			untilDeadline := time.Until(result.Task.ExpiresAt.UTC())
			if untilDeadline < waitFor {
				waitFor = untilDeadline
			}
		}
		if waitFor <= 0 {
			cancelSubscription()
			continue
		}
		timer := time.NewTimer(waitFor)
		select {
		case <-r.Context().Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			cancelSubscription()
			return
		case <-changed:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		}
		cancelSubscription()
	}
}

func (h *Handler) handleHumanTaskDecision(w http.ResponseWriter, r *http.Request, workspaceID string, taskID string) {
	task, err := h.store.GetHumanTaskForWorkspace(r.Context(), workspaceID, taskID)
	if err != nil {
		writeStateError(w, err)
		return
	}
	if !h.humanTaskTargetAllowed(r, workspaceID, task) {
		writeError(w, http.StatusForbidden, "service principal target does not allow this HumanTask")
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" || len(idempotencyKey) > 200 {
		writeError(w, http.StatusBadRequest, "Idempotency-Key is required and must not exceed 200 characters")
		return
	}
	var request humanTaskDecisionRequest
	if err := readRequiredJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid HumanTask decision")
		return
	}
	if request.Outcome == state.HumanTaskOutcomeSubmit {
		if len(request.Value) == 0 {
			request.Value = json.RawMessage("null")
		}
		if err := resourceconfig.ValidateValue(task.Schema, request.Value); err != nil {
			writeError(w, http.StatusBadRequest, "decision value does not satisfy input_schema")
			return
		}
	} else if request.Outcome != state.HumanTaskOutcomeCancel {
		writeError(w, http.StatusBadRequest, "outcome must be submit or cancel")
		return
	}
	fingerprint := humanTaskFingerprint(struct {
		Outcome state.HumanTaskOutcome `json:"outcome"`
		Value   json.RawMessage        `json:"value,omitempty"`
	}{request.Outcome, request.Value})
	actor := requestActorSubject(r)
	if actor == "" {
		actor = "operator"
	}
	result, err := h.store.DecideHeldHumanTask(r.Context(), workspaceID, taskID, state.HumanTaskDecision{
		Outcome:        request.Outcome,
		Value:          request.Value,
		IdempotencyKey: idempotencyKey,
		Fingerprint:    fingerprint,
		Actor:          actor,
	})
	if err != nil {
		writeStateError(w, err)
		return
	}
	if result.Task.State == state.HumanTaskExpired {
		writeHumanTaskTerminalError(w, http.StatusRequestTimeout, result.Task)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": humanTaskResponse(result.Task), "replayed": result.Replayed})
}

func (h *Handler) humanTaskTargetAllowed(r *http.Request, workspaceID string, task state.HumanTask) bool {
	principal := workspacePrincipalFrom(r.Context())
	if principal == nil || principal.Service == nil {
		return true
	}
	job, run, found, err := h.store.GetJob(r.Context(), workspaceID, task.JobID)
	if err != nil || !found || job.RunID != run.ID {
		return false
	}
	return principal.Service.AllowsTarget(run.App, run.Action)
}

func humanTaskFingerprint(value any) string {
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func validHumanTaskState(value state.HumanTaskState) bool {
	switch value {
	case state.HumanTaskPending, state.HumanTaskDecided, state.HumanTaskExpired, state.HumanTaskCanceled, state.HumanTaskCompleted:
		return true
	default:
		return false
	}
}

func writeHumanTaskTerminalError(w http.ResponseWriter, status int, task state.HumanTask) {
	cause := strings.TrimSpace(task.TerminalCause)
	if cause == "" {
		cause = string(task.State)
	}
	writeJSON(w, status, map[string]any{
		"error":   fmt.Sprintf("HumanTask is %s", task.State),
		"code":    cause,
		"task_id": task.ID,
	})
}
