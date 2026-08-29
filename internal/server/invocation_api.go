package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	executionpkg "github.com/imprun/windforce-core/internal/execution"
	"github.com/imprun/windforce-core/internal/state"
)

const (
	invocationRunIDHeader             = "X-WF-Run-Id"
	invocationRunStateHeader          = "X-WF-Run-State"
	invocationIdempotencyReusedHeader = "X-WF-Idempotency-Reused"
	maxInvocationIdempotencyKeyBytes  = 200
	defaultInvocationWaitTimeout      = 30 * time.Second
	maxInvocationWaitTimeout          = 30 * time.Second
)

type invocationCreateRunRequest struct {
	App           string          `json:"app"`
	Action        string          `json:"action"`
	Input         json.RawMessage `json:"input"`
	CorrelationID string          `json:"correlation_id,omitempty"`
}

type invocationRunView struct {
	RunID         string    `json:"run_id"`
	State         string    `json:"state"`
	App           string    `json:"app"`
	Action        string    `json:"action"`
	CorrelationID string    `json:"correlation_id,omitempty"`
	Replayed      bool      `json:"replayed,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type invocationAppView struct {
	App     string                          `json:"app"`
	Release invocationReleaseView           `json:"release"`
	Actions map[string]invocationActionView `json:"actions"`
}

type invocationReleaseView struct {
	DeploymentID *string `json:"deployment_id,omitempty"`
	APIVersion   string  `json:"api_version,omitempty"`
	Commit       string  `json:"commit"`
	BundleDigest string  `json:"bundle_digest"`
}

type invocationActionView struct {
	InputSchema      json.RawMessage   `json:"input_schema"`
	OutputSchema     json.RawMessage   `json:"output_schema"`
	PublicInterfaces []json.RawMessage `json:"public_interfaces,omitempty"`
	Timeout          *int32            `json:"timeout,omitempty"`
	RunsOn           []string          `json:"runs_on,omitempty"`
}

func (h *Handler) handleInvocationAPI(w http.ResponseWriter, r *http.Request) bool {
	parts := splitPath(r.URL.Path)
	if len(parts) == 3 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "openapi.json" {
		if r.Method != http.MethodGet {
			writeInvocationError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return true
		}
		h.handleInvocationOpenAPI(w, r)
		return true
	}
	if len(parts) < 5 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "workspaces" {
		return false
	}
	workspaceID := contract.NormalizeWorkspace(parts[3])
	if !contract.ValidWorkspaceID(workspaceID) {
		writeInvocationError(w, http.StatusBadRequest, string(executionpkg.FaultInvalidRequest), "invalid workspace id")
		return true
	}
	principal, status, message := h.authenticateInvocation(r, workspaceID)
	if status != 0 {
		writeInvocationError(w, status, http.StatusText(status), message)
		return true
	}
	if h.execution == nil || h.store == nil {
		writeInvocationError(w, http.StatusServiceUnavailable, string(executionpkg.FaultUnavailable), "Invocation API is not configured")
		return true
	}

	switch {
	case len(parts) == 5 && parts[4] == "runs" && r.Method == http.MethodPost:
		h.handleInvocationCreateRun(w, r, workspaceID, principal, false)
	case len(parts) == 6 && parts[4] == "runs" && parts[5] == "wait" && r.Method == http.MethodPost:
		h.handleInvocationCreateRun(w, r, workspaceID, principal, true)
	case len(parts) == 6 && parts[4] == "runs" && r.Method == http.MethodGet:
		h.handleInvocationGetRun(w, r, workspaceID, parts[5], principal)
	case len(parts) == 7 && parts[4] == "runs" && parts[6] == "result" && r.Method == http.MethodGet:
		h.handleInvocationRunResult(w, r, workspaceID, parts[5], principal)
	case len(parts) == 7 && parts[4] == "runs" && parts[6] == "cancel" && r.Method == http.MethodPost:
		h.handleInvocationCancelRun(w, r, workspaceID, parts[5], principal)
	case len(parts) == 6 && parts[4] == "apps" && r.Method == http.MethodGet:
		h.handleInvocationDescribeApp(w, r, workspaceID, parts[5], principal)
	default:
		writeInvocationError(w, http.StatusNotFound, "not_found", "not found")
	}
	return true
}

func (h *Handler) authenticateInvocation(r *http.Request, workspaceID string) (executionpkg.Principal, int, string) {
	tokenValue := bearer(r)
	if tokenValue == "" {
		return executionpkg.Principal{}, http.StatusUnauthorized, "missing bearer token"
	}
	if h.adminToken != "" && authorized(r, h.adminToken) {
		subject := strings.TrimSpace(r.Header.Get("X-Windforce-Actor"))
		if subject == "" {
			subject = "operator:admin"
		}
		return executionpkg.OperatorPrincipal(workspaceID, subject), 0, ""
	}
	isEngineCredential := strings.HasPrefix(tokenValue, contract.WorkspaceTokenPrefix) ||
		strings.HasPrefix(tokenValue, contract.ClientTokenPrefix) ||
		strings.HasPrefix(tokenValue, contract.ServiceTokenPrefix)
	if !isEngineCredential && h.adminToken == "" {
		subject := strings.TrimSpace(r.Header.Get("X-Windforce-Actor"))
		if subject == "" {
			subject = "operator:admin"
		}
		return executionpkg.OperatorPrincipal(workspaceID, subject), 0, ""
	}
	if h.store == nil {
		return executionpkg.Principal{}, http.StatusServiceUnavailable, "Invocation API is not configured"
	}
	if strings.HasPrefix(tokenValue, contract.WorkspaceTokenPrefix) {
		token, err := h.store.GetWorkspaceTokenByTokenHash(r.Context(), workspaceID, state.HashWorkspaceToken(tokenValue))
		if err != nil {
			return executionpkg.Principal{}, http.StatusUnauthorized, "unauthorized"
		}
		return executionpkg.OperatorPrincipal(workspaceID, "workspace-token:"+token.ID), 0, ""
	}
	if strings.HasPrefix(tokenValue, contract.ClientTokenPrefix) {
		client, err := h.store.GetClientByTokenHash(r.Context(), workspaceID, state.HashClientToken(tokenValue))
		if err != nil {
			return executionpkg.Principal{}, http.StatusUnauthorized, "unauthorized"
		}
		return executionpkg.DefaultClientPrincipal(workspaceID, client), 0, ""
	}
	if strings.HasPrefix(tokenValue, contract.ServiceTokenPrefix) {
		principal, err := h.store.GetServicePrincipalByTokenHash(r.Context(), workspaceID, state.HashBearerToken(tokenValue))
		if err != nil {
			return executionpkg.Principal{}, http.StatusUnauthorized, "unauthorized"
		}
		return executionpkg.ServicePrincipal(workspaceID, principal), 0, ""
	}
	return executionpkg.Principal{}, http.StatusUnauthorized, "unauthorized"
}

func (h *Handler) handleInvocationCreateRun(w http.ResponseWriter, r *http.Request, workspaceID string, principal executionpkg.Principal, wait bool) {
	workspace, err := h.store.GetWorkspace(r.Context(), workspaceID)
	if err != nil {
		writeInvocationStateError(w, err)
		return
	}
	if workspace.Status == state.WorkspaceArchived {
		writeInvocationError(w, http.StatusConflict, string(executionpkg.FaultConflict), "workspace is archived")
		return
	}
	var request invocationCreateRunRequest
	if err := decodeStrictInvocationJSON(w, r, &request, false); err != nil {
		writeInvocationError(w, http.StatusBadRequest, string(executionpkg.FaultInvalidRequest), err.Error())
		return
	}
	if len(request.Input) == 0 {
		writeInvocationError(w, http.StatusBadRequest, string(executionpkg.FaultInvalidRequest), "input is required")
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(idempotencyKey) > maxInvocationIdempotencyKeyBytes {
		writeInvocationError(w, http.StatusBadRequest, string(executionpkg.FaultInvalidRequest), "Idempotency-Key is too long")
		return
	}
	admission, err := h.execution.CreateRun(r.Context(), executionpkg.CreateRunRequest{
		Workspace:      workspaceID,
		App:            request.App,
		Action:         request.Action,
		Input:          request.Input,
		CorrelationID:  request.CorrelationID,
		IdempotencyKey: idempotencyKey,
		Adapter:        "http",
		TriggerKind:    "http",
		Principal:      principal,
	})
	if err != nil {
		writeInvocationFault(w, err)
		return
	}
	setInvocationRunHeaders(w, r, workspaceID, admission.Run.ID)
	setInvocationAdmissionHeaders(w, admission.Run, admission.Replayed)
	if wait {
		timeout, ok := parseInvocationWaitTimeout(w, r)
		if !ok {
			return
		}
		h.waitForInvocationRun(w, r, workspaceID, admission.Run.ID, principal, timeout, admission.Replayed)
		return
	}
	status := http.StatusCreated
	if admission.Replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, newInvocationRunView(admission.Run, admission.Replayed))
}

func (h *Handler) handleInvocationGetRun(w http.ResponseWriter, r *http.Request, workspaceID string, runID string, principal executionpkg.Principal) {
	run, err := h.execution.GetRunForPrincipal(r.Context(), principal, workspaceID, runID)
	if err != nil {
		writeInvocationFault(w, err)
		return
	}
	setInvocationRunHeaders(w, r, workspaceID, run.ID)
	writeJSON(w, http.StatusOK, newInvocationRunView(run, false))
}

func (h *Handler) handleInvocationRunResult(w http.ResponseWriter, r *http.Request, workspaceID string, runID string, principal executionpkg.Principal) {
	run, err := h.execution.GetRunForPrincipal(r.Context(), principal, workspaceID, runID)
	if err != nil {
		writeInvocationFault(w, err)
		return
	}
	setInvocationRunHeaders(w, r, workspaceID, run.ID)
	if !state.TerminalRunState(run.State) {
		writeJSON(w, http.StatusAccepted, newInvocationRunView(run, false))
		return
	}
	writeInvocationRawResult(w, run)
}

func (h *Handler) handleInvocationCancelRun(w http.ResponseWriter, r *http.Request, workspaceID string, runID string, principal executionpkg.Principal) {
	var request struct {
		Reason string `json:"reason,omitempty"`
	}
	if err := decodeStrictInvocationJSON(w, r, &request, true); err != nil {
		writeInvocationError(w, http.StatusBadRequest, string(executionpkg.FaultInvalidRequest), err.Error())
		return
	}
	run, err := h.execution.CancelRunForPrincipal(r.Context(), principal, workspaceID, runID, request.Reason)
	if err != nil {
		writeInvocationFault(w, err)
		return
	}
	setInvocationRunHeaders(w, r, workspaceID, run.ID)
	writeJSON(w, http.StatusOK, newInvocationRunView(run, false))
}

func (h *Handler) handleInvocationDescribeApp(w http.ResponseWriter, r *http.Request, workspaceID string, app string, principal executionpkg.Principal) {
	description, err := h.execution.DescribeAppForPrincipal(r.Context(), principal, workspaceID, app)
	if err != nil {
		writeInvocationFault(w, err)
		return
	}
	actions := make(map[string]invocationActionView, len(description.Actions))
	for key, action := range description.Actions {
		timeout := action.Spec.TimeoutS
		runsOn := action.Spec.RunsOn
		if runsOn == nil {
			runsOn = action.Spec.Capabilities
		}
		var resolvedRunsOn []string
		if runsOn != nil {
			resolvedRunsOn = append([]string(nil), (*runsOn)...)
		}
		actions[key] = invocationActionView{
			InputSchema:      action.InputSchema,
			OutputSchema:     action.OutputSchema,
			PublicInterfaces: contract.ClonePublicInterfaces(action.Spec.PublicInterfaces),
			Timeout:          timeout,
			RunsOn:           resolvedRunsOn,
		}
	}
	writeJSON(w, http.StatusOK, invocationAppView{
		App: description.Deployment.App,
		Release: invocationReleaseView{
			DeploymentID: description.Deployment.DeploymentID,
			APIVersion:   description.Deployment.APIVersion,
			Commit:       description.Deployment.Commit,
			BundleDigest: description.Deployment.BundleDigest,
		},
		Actions: actions,
	})
}

func (h *Handler) waitForInvocationRun(w http.ResponseWriter, r *http.Request, workspaceID string, runID string, principal executionpkg.Principal, timeout time.Duration, replayed bool) {
	deadline := time.Now().Add(timeout)
	for {
		run, err := h.execution.GetRunForPrincipal(r.Context(), principal, workspaceID, runID)
		if err != nil {
			writeInvocationFault(w, err)
			return
		}
		w.Header().Set(invocationRunStateHeader, strings.ToLower(string(run.State)))
		if state.TerminalRunState(run.State) {
			writeInvocationRawResult(w, run)
			return
		}
		if !time.Now().Before(deadline) {
			writeJSON(w, http.StatusAccepted, newInvocationRunView(run, replayed))
			return
		}
		sleep := 50 * time.Millisecond
		if remaining := time.Until(deadline); remaining < sleep {
			sleep = remaining
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(sleep):
		}
	}
}

func parseInvocationWaitTimeout(w http.ResponseWriter, r *http.Request) (time.Duration, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("timeout"))
	if raw == "" {
		return defaultInvocationWaitTimeout, true
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil || timeout < 0 {
		writeInvocationError(w, http.StatusBadRequest, string(executionpkg.FaultInvalidRequest), "timeout must be a non-negative duration")
		return 0, false
	}
	if timeout > maxInvocationWaitTimeout {
		timeout = maxInvocationWaitTimeout
	}
	return timeout, true
}

func decodeStrictInvocationJSON(w http.ResponseWriter, r *http.Request, target any, optional bool) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	defer r.Body.Close()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return errors.New("could not read request body")
	}
	if len(bytes.TrimSpace(data)) == 0 {
		if optional {
			return nil
		}
		return errors.New("request body must be a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("request body does not match the Invocation API specification")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func setInvocationRunHeaders(w http.ResponseWriter, r *http.Request, workspaceID string, runID string) {
	w.Header().Set(invocationRunIDHeader, runID)
	w.Header().Set("Location", "/api/v1/workspaces/"+workspaceID+"/runs/"+runID)
}

func setInvocationAdmissionHeaders(w http.ResponseWriter, run state.Run, replayed bool) {
	w.Header().Set(invocationRunStateHeader, strings.ToLower(string(run.State)))
	w.Header().Set(invocationIdempotencyReusedHeader, strconv.FormatBool(replayed))
}

func writeInvocationRawResult(w http.ResponseWriter, run state.Run) {
	result := run.Output
	if len(result) == 0 && run.Result != nil && len(run.Result.Output) > 0 {
		result = run.Result.Output
	}
	if len(result) == 0 {
		result = run.Error
	}
	if len(result) == 0 || !json.Valid(result) {
		result = json.RawMessage("null")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result)
}

func newInvocationRunView(run state.Run, replayed bool) invocationRunView {
	return invocationRunView{
		RunID:         run.ID,
		State:         strings.ToLower(string(run.State)),
		App:           run.App,
		Action:        run.Action,
		CorrelationID: run.CorrelationID,
		Replayed:      replayed,
		CreatedAt:     run.CreatedAt,
		UpdatedAt:     run.UpdatedAt,
	}
}

func writeInvocationFault(w http.ResponseWriter, err error) {
	status, kind := invocationFaultStatus(err)
	writeInvocationError(w, status, string(kind), err.Error())
}

func invocationFaultStatus(err error) (int, executionpkg.FaultKind) {
	status := http.StatusInternalServerError
	kind := executionpkg.FaultKindOf(err)
	switch kind {
	case executionpkg.FaultUnavailable:
		status = http.StatusServiceUnavailable
	case executionpkg.FaultInvalidRequest:
		status = http.StatusBadRequest
	case executionpkg.FaultForbidden:
		status = http.StatusForbidden
	case executionpkg.FaultAppNotFound, executionpkg.FaultActionNotFound:
		status = http.StatusNotFound
	case executionpkg.FaultRoutingConflict, executionpkg.FaultConflict:
		status = http.StatusConflict
	}
	return status, kind
}

func writeInvocationStateError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := executionpkg.FaultInternal
	if errors.Is(err, state.ErrNotFound) {
		status = http.StatusNotFound
		writeInvocationError(w, status, "not_found", err.Error())
		return
	} else if errors.Is(err, state.ErrInvalidState) || errors.Is(err, state.ErrConflict) {
		status = http.StatusConflict
		code = executionpkg.FaultConflict
	}
	writeInvocationError(w, status, string(code), err.Error())
}

func writeInvocationError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    strings.ToLower(strings.ReplaceAll(code, " ", "_")),
			"message": message,
		},
	})
}
