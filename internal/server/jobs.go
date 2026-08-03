package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/state"
)

func (h *Handler) handleJobList(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	query, limit, ok := parseJobListQuery(w, r, workspaceID)
	if !ok {
		return
	}
	query.Limit = limit + 1
	items, err := h.store.ListJobs(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	pagination := map[string]any{
		"limit":    limit,
		"count":    len(items),
		"has_more": hasMore,
	}
	if hasMore {
		last := items[len(items)-1]
		pagination["next_cursor"] = encodeJobCursor(last.CreatedAt, last.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "pagination": pagination})
}

func (h *Handler) handleJobSummary(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	recent := 24 * time.Hour
	if raw := strings.TrimSpace(r.URL.Query().Get("recent_seconds")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 || value > 7*24*60*60 {
			writeError(w, http.StatusBadRequest, "recent_seconds must be between 1 and 604800")
			return
		}
		recent = time.Duration(value) * time.Second
	}
	summary, err := h.store.JobSummary(r.Context(), workspaceID, recent)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *Handler) handleJobStatus(w http.ResponseWriter, r *http.Request, workspaceID string, jobID string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	job, run, found, err := h.store.GetJob(r.Context(), workspaceID, jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, newJobStatus(workspaceID, job, run))
}

func (h *Handler) handleJobResult(w http.ResponseWriter, r *http.Request, workspaceID string, jobID string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	job, run, found, err := h.store.GetJob(r.Context(), workspaceID, jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	status, result, done := jobResult(job, run)
	if !done {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "pending"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status, "result": result})
}

func (h *Handler) handleJobCancel(w http.ResponseWriter, r *http.Request, workspaceID string, jobID string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	var request struct {
		Reason string `json:"reason"`
	}
	_ = readOptionalJSON(r, &request)
	result, err := h.store.CancelJob(r.Context(), workspaceID, jobID, requestActorSubject(r), request.Reason)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !result.Found {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) handleJobLogs(w http.ResponseWriter, r *http.Request, workspaceID string, jobID string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	logs, exists, err := h.store.GetLogs(r.Context(), workspaceID, jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	tailBytes, err := parseTailBytes(r.URL.Query().Get("tail_bytes"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	data := []byte(logs)
	if tailBytes >= 0 && len(data) > tailBytes {
		data = data[len(data)-tailBytes:]
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

const (
	jobLogStreamPollInterval = 250 * time.Millisecond
	jobLogStreamPingInterval = 15 * time.Second
	defaultJobLogStreamTTL   = 60 * time.Second
	maxJobLogStreamTTL       = 5 * time.Minute
	maxJobLogUpdateBytes     = 256 << 10
)

type jobLogStreamEvent struct {
	Type      string `json:"type"`
	Running   *bool  `json:"running,omitempty"`
	Completed *bool  `json:"completed,omitempty"`
	NewLogs   string `json:"new_logs,omitempty"`
	LogOffset int64  `json:"log_offset,omitempty"`
	Status    string `json:"status,omitempty"`
	Attempt   int    `json:"attempt,omitempty"`
	WorkerID  string `json:"worker_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (h *Handler) handleJobLogStream(w http.ResponseWriter, r *http.Request, workspaceID string, jobID string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	offset, err := parseLogOffset(r.URL.Query().Get("offset"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	streamTTL, err := parseLogStreamTTL(r.URL.Query().Get("timeout_seconds"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	job, run, exists, err := h.store.GetJob(r.Context(), workspaceID, jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	writeEvent := func(event jobLogStreamEvent) bool {
		data, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			return false
		}
		if _, writeErr := fmt.Fprintf(w, "data: %s\n\n", data); writeErr != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	poll := time.NewTicker(jobLogStreamPollInterval)
	defer poll.Stop()
	ping := time.NewTicker(jobLogStreamPingInterval)
	defer ping.Stop()
	timeout := time.NewTimer(streamTTL)
	defer timeout.Stop()
	lastStatus := ""
	first := true
	for {
		update, found, updateErr := h.store.GetLogUpdate(r.Context(), workspaceID, jobID, offset, maxJobLogUpdateBytes)
		if updateErr != nil {
			writeEvent(jobLogStreamEvent{Type: "error", Error: updateErr.Error()})
			return
		}
		if !found {
			writeEvent(jobLogStreamEvent{Type: "notfound"})
			return
		}
		if !first {
			job, run, found, updateErr = h.store.GetJob(r.Context(), workspaceID, jobID)
			if updateErr != nil {
				writeEvent(jobLogStreamEvent{Type: "error", Error: updateErr.Error()})
				return
			}
			if !found {
				writeEvent(jobLogStreamEvent{Type: "notfound"})
				return
			}
		}
		first = false
		completed := job.State == state.JobSucceeded || job.State == state.JobFailed
		running := job.State == state.JobRunning
		status := string(job.State)
		if completed {
			status = terminalJobStatus(job, run)
		}
		if update.NewLogs != "" || status != lastStatus {
			if !writeEvent(jobLogStreamEvent{
				Type:      "update",
				Running:   &running,
				Completed: &completed,
				NewLogs:   update.NewLogs,
				LogOffset: update.Offset,
				Status:    status,
				Attempt:   job.Attempt,
				WorkerID:  job.LeaseOwner,
			}) {
				return
			}
			lastStatus = status
		}
		offset = update.Offset
		if completed {
			return
		}

		select {
		case <-r.Context().Done():
			return
		case <-poll.C:
		case <-ping.C:
			if !writeEvent(jobLogStreamEvent{Type: "ping"}) {
				return
			}
		case <-timeout.C:
			writeEvent(jobLogStreamEvent{Type: "timeout"})
			return
		}
	}
}

func parseLogOffset(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	offset, err := strconv.ParseInt(value, 10, 64)
	if err != nil || offset < 0 {
		return 0, errors.New("offset must be a non-negative integer")
	}
	return offset, nil
}

func parseLogStreamTTL(value string) (time.Duration, error) {
	if value == "" {
		return defaultJobLogStreamTTL, nil
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 0, errors.New("timeout_seconds must be a positive integer")
	}
	ttl := time.Duration(seconds) * time.Second
	if ttl > maxJobLogStreamTTL {
		return 0, errors.New("timeout_seconds exceeds server limit")
	}
	return ttl, nil
}

type jobStatusResponse struct {
	ID             string          `json:"id"`
	WorkspaceID    string          `json:"workspace_id"`
	State          string          `json:"state"`
	Status         *string         `json:"status,omitempty"`
	Worker         *string         `json:"worker,omitempty"`
	AppKey         *string         `json:"app_key,omitempty"`
	ActionKey      *string         `json:"action_key,omitempty"`
	TriggerKind    *string         `json:"trigger_kind,omitempty"`
	Kind           *string         `json:"kind,omitempty"`
	GitSourceID    *int64          `json:"git_source_id,omitempty"`
	CommitSha      *string         `json:"commit_sha,omitempty"`
	Entrypoint     *string         `json:"entrypoint,omitempty"`
	InputSchema    json.RawMessage `json:"input_schema,omitempty"`
	OutputSchema   json.RawMessage `json:"output_schema,omitempty"`
	Tag            string          `json:"tag,omitempty"`
	TimeoutS       int32           `json:"timeout_s,omitempty"`
	CreatedBy      string          `json:"created_by,omitempty"`
	PermissionedAs string          `json:"permissioned_as,omitempty"`
	Input          json.RawMessage `json:"input,omitempty"`
	CreatedAt      *time.Time      `json:"created_at,omitempty"`
	StartedAt      *time.Time      `json:"started_at,omitempty"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
	DurationMs     int64           `json:"duration_ms,omitempty"`
	CanceledBy     *string         `json:"canceled_by,omitempty"`
	CanceledReason *string         `json:"canceled_reason,omitempty"`
	FlowRunID      *string         `json:"flow_run_id,omitempty"`
	FlowKey        *string         `json:"flow_key,omitempty"`
	FlowStepKey    *string         `json:"flow_step_key,omitempty"`
}

func newJobStatus(workspaceID string, job state.Job, run state.Run) jobStatusResponse {
	stateValue := "queued"
	var statusValue *string
	var worker *string
	startedAt := job.StartedAt
	var completedAt *time.Time
	if job.LeaseOwner != "" {
		worker = stringPtr(job.LeaseOwner)
	}
	switch job.State {
	case state.JobRunning:
		stateValue = "running"
		if startedAt == nil {
			startedAt = &job.UpdatedAt
		}
	case state.JobSucceeded, state.JobFailed:
		stateValue = "completed"
		status := jobDetailStatus(job, run)
		statusValue = &status
		completedAt = &run.UpdatedAt
	}
	app := job.Payload.App
	action := job.Payload.Action
	kind := job.Kind
	commit := job.Payload.Commit
	tag := strings.TrimSpace(job.Payload.Tag)
	if tag == "" {
		tag = contract.EffectiveRouteTagForAction(job.Payload.PinnedDeployment(), job.Payload.ActionSpec)
	}
	response := jobStatusResponse{
		ID:             job.ID,
		WorkspaceID:    contract.NormalizeWorkspace(workspaceID),
		State:          stateValue,
		Status:         statusValue,
		Worker:         worker,
		AppKey:         stringPtr(app),
		ActionKey:      stringPtr(action),
		TriggerKind:    stringPtr(jobStatusTriggerKind(job, run)),
		Kind:           stringPtr(kind),
		GitSourceID:    canonicalGitSourceIDPtr(job.Payload.GitSourceID),
		CommitSha:      stringPtr(commit),
		Entrypoint:     stringPtr(jobStatusEntrypoint(job)),
		InputSchema:    cloneRaw(job.Payload.InputSchema),
		OutputSchema:   cloneRaw(job.Payload.OutputSchema),
		Tag:            tag,
		TimeoutS:       timeoutSeconds(job.Payload.ActionSpec.TimeoutMs),
		CreatedBy:      firstNonEmpty(strings.TrimSpace(job.Payload.CreatedBy), strings.TrimSpace(run.CreatedBy)),
		PermissionedAs: firstNonEmpty(strings.TrimSpace(job.Payload.PermissionedAs), strings.TrimSpace(run.PermissionedAs), strings.TrimSpace(job.Payload.CreatedBy), strings.TrimSpace(run.CreatedBy)),
		Input:          cloneRaw(job.Payload.Input),
		CreatedAt:      &job.CreatedAt,
		StartedAt:      startedAt,
		CompletedAt:    completedAt,
		CanceledBy:     firstPresentStringPtr(job.CanceledBy, jobStatusCanceledBy(run)),
		CanceledReason: firstPresentStringPtr(job.CanceledReason, jobStatusCanceledReason(run)),
		FlowRunID:      stringPtr(job.Payload.FlowRunID),
		FlowKey:        stringPtr(job.Payload.FlowKey),
		FlowStepKey:    stringPtr(job.Payload.FlowStepKey),
	}
	if run.Result != nil {
		response.DurationMs = run.Result.DurationMs
	}
	return response
}

func jobStatusEntrypoint(job state.Job) string {
	if entrypoint := strings.TrimSpace(job.Payload.PinnedDeployment().Entrypoint); entrypoint != "" {
		return entrypoint
	}
	return strings.TrimSpace(job.Payload.ActionSpec.Entrypoint)
}

func jobStatusTriggerKind(job state.Job, run state.Run) string {
	if job.Payload.TriggerKind != "" {
		return job.Payload.TriggerKind
	}
	return run.Adapter
}

func jobStatusCanceledReason(run state.Run) *string {
	if run.State != state.RunCanceled || len(run.Error) == 0 {
		return nil
	}
	var payload struct {
		Message        string  `json:"message"`
		CanceledReason *string `json:"canceledReason"`
	}
	if json.Unmarshal(run.Error, &payload) == nil {
		if payload.CanceledReason != nil {
			return payload.CanceledReason
		}
		if strings.TrimSpace(payload.Message) != "" {
			return stringPtr(payload.Message)
		}
	}
	return nil
}

func jobStatusCanceledBy(run state.Run) *string {
	if run.State != state.RunCanceled || len(run.Error) == 0 {
		return nil
	}
	var payload struct {
		CanceledBy string `json:"canceledBy"`
	}
	if json.Unmarshal(run.Error, &payload) == nil {
		return stringPtr(strings.TrimSpace(payload.CanceledBy))
	}
	return nil
}

func timeoutSeconds(timeoutMs int64) int32 {
	if timeoutMs <= 0 {
		return 0
	}
	return int32((timeoutMs + 999) / 1000)
}

func jobResult(job state.Job, run state.Run) (string, json.RawMessage, bool) {
	if job.State == state.JobQueued || job.State == state.JobRunning {
		return "", nil, false
	}
	status := terminalJobStatus(job, run)
	switch status {
	case "success":
		return status, rawOrNull(run.Output), true
	case "canceled":
		message := runErrorMessage(run)
		if message == "" {
			message = "job canceled"
		}
		return status, mustRaw(map[string]string{"name": "Canceled", "message": message}), true
	default:
		if run.Result != nil && len(run.Result.Output) > 0 {
			return "failure", rawOrNull(run.Result.Output), true
		}
		message := runErrorMessage(run)
		if message == "" {
			message = "job failed"
		}
		return "failure", mustRaw(map[string]string{"name": "Error", "message": message}), true
	}
}

func terminalJobStatus(job state.Job, run state.Run) string {
	if run.State == state.RunCanceled {
		return "canceled"
	}
	if job.State == state.JobSucceeded || run.State == state.RunSucceeded || run.State == state.RunWaitingHuman {
		return "success"
	}
	return "failure"
}

func jobDetailStatus(job state.Job, run state.Run) string {
	return terminalJobStatus(job, run)
}

func runErrorMessage(run state.Run) string {
	if run.Result != nil && run.Result.Error != "" {
		return run.Result.Error
	}
	if len(run.Error) == 0 {
		return ""
	}
	var envelope struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(run.Error, &envelope) == nil {
		return envelope.Message
	}
	return string(run.Error)
}

func rawOrNull(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("null")
	}
	return cloneRaw(value)
}

func mustRaw(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("null")
	}
	return data
}

func cloneRaw(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

const maxTailBytes = 1048576

func parseTailBytes(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return -1, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, errors.New("tail_bytes must be a non-negative integer")
	}
	if value > maxTailBytes {
		return 0, errors.New("tail_bytes exceeds server limit")
	}
	return int(value), nil
}

const (
	defaultJobListLimit = 50
	maxJobListLimit     = 500
)

func parseJobListQuery(w http.ResponseWriter, r *http.Request, workspaceID string) (state.JobListQuery, int, bool) {
	query := r.URL.Query()
	status := strings.TrimSpace(query.Get("status"))
	if status == "" {
		status = "all"
	}
	if !validJobStatusFilter(status) {
		writeError(w, http.StatusBadRequest, "invalid status filter")
		return state.JobListQuery{}, 0, false
	}
	order := strings.TrimSpace(query.Get("order"))
	if order != "" && order != "created_at_desc" {
		writeError(w, http.StatusBadRequest, "unsupported order")
		return state.JobListQuery{}, 0, false
	}
	limit := defaultJobListLimit
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 || value > maxJobListLimit {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 500")
			return state.JobListQuery{}, 0, false
		}
		limit = value
	}
	var cursorCreatedAt *time.Time
	cursorID := ""
	if raw := strings.TrimSpace(query.Get("cursor")); raw != "" {
		createdAt, id, err := decodeJobCursor(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return state.JobListQuery{}, 0, false
		}
		cursorCreatedAt = &createdAt
		cursorID = id
	}
	since, ok := parseOptionalTime(w, query.Get("since"), "since")
	if !ok {
		return state.JobListQuery{}, 0, false
	}
	until, ok := parseOptionalTime(w, query.Get("until"), "until")
	if !ok {
		return state.JobListQuery{}, 0, false
	}
	return state.JobListQuery{
		WorkspaceID:     contract.NormalizeWorkspace(workspaceID),
		Status:          status,
		AppKey:          strings.TrimSpace(query.Get("app")),
		ActionKey:       strings.TrimSpace(query.Get("action")),
		TriggerKind:     strings.TrimSpace(query.Get("trigger_kind")),
		Limit:           limit,
		CursorCreatedAt: cursorCreatedAt,
		CursorID:        cursorID,
		Since:           since,
		Until:           until,
	}, limit, true
}

func validJobStatusFilter(status string) bool {
	switch status {
	case "queued", "running", "success", "failure", "completed", "canceled", "all":
		return true
	default:
		return false
	}
}

func parseOptionalTime(w http.ResponseWriter, raw string, name string) (*time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, name+" must be RFC3339")
		return nil, false
	}
	return &value, true
}

func encodeJobCursor(createdAt time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(createdAt.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeJobCursor(raw string) (time.Time, string, error) {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, "", err
	}
	createdRaw, id, ok := strings.Cut(string(data), "|")
	if !ok {
		return time.Time{}, "", fmt.Errorf("malformed cursor")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdRaw)
	if err != nil {
		return time.Time{}, "", err
	}
	if id == "" || !isCanonicalUUID(id) {
		return time.Time{}, "", fmt.Errorf("malformed cursor")
	}
	return createdAt, id, nil
}

func isCanonicalUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, r := range value {
		switch index {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstPresentStringPtr(values ...*string) *string {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
