package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/imprun/windforce-lite/internal/contract"
	gitsourcepkg "github.com/imprun/windforce-lite/internal/gitsource"
	"github.com/imprun/windforce-lite/internal/state"
)

const deploymentRequestsStatePath = "control-plane/deployment-requests/v1"

type canonicalDeploymentRequest struct {
	ID              string     `json:"id"`
	WorkspaceID     string     `json:"workspace_id"`
	GitSourceID     int64      `json:"git_source_id"`
	SourceName      string     `json:"source_name"`
	RepoURL         string     `json:"repo_url"`
	Branch          string     `json:"branch"`
	Subpath         string     `json:"subpath"`
	Status          string     `json:"status"`
	AppKey          string     `json:"app_key"`
	Entrypoint      string     `json:"entrypoint"`
	TargetCommit    string     `json:"target_commit"`
	CurrentCommit   string     `json:"current_commit,omitempty"`
	ActionsCount    int        `json:"actions_count"`
	RequestedBy     string     `json:"requested_by"`
	RequestMessage  string     `json:"request_message,omitempty"`
	OperatorMessage string     `json:"operator_message,omitempty"`
	ReviewedBy      *string    `json:"reviewed_by,omitempty"`
	DeployedBy      *string    `json:"deployed_by,omitempty"`
	DeploymentID    *string    `json:"deployment_id,omitempty"`
	DeployedCommit  *string    `json:"deployed_commit,omitempty"`
	DeployedAt      *time.Time `json:"deployed_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type deploymentRequestStore struct {
	Requests []canonicalDeploymentRequest `json:"requests"`
}

type canonicalDeploymentRequestCreateRequest struct {
	GitSourceID      string  `json:"git_source_id"`
	GitSourceIDCamel string  `json:"GitSourceID"`
	Message          *string `json:"message"`
	MessageCamel     *string `json:"Message"`
}

type canonicalDeploymentRequestActionRequest struct {
	Confirm        bool    `json:"confirm"`
	Confirmed      bool    `json:"confirmed"`
	ConfirmCamel   bool    `json:"Confirm"`
	ConfirmedCamel bool    `json:"Confirmed"`
	Message        *string `json:"message"`
	MessageCamel   *string `json:"Message"`
}

func (h *Handler) handleCanonicalDeploymentRequests(w http.ResponseWriter, r *http.Request, workspaceID string) {
	requests, ok := h.loadCanonicalDeploymentRequests(w, r.Context(), workspaceID)
	if !ok {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	sourceID := strings.TrimSpace(r.URL.Query().Get("git_source_id"))
	filtered := make([]canonicalDeploymentRequest, 0, len(requests))
	for _, request := range requests {
		if status != "" && request.Status != status {
			continue
		}
		if sourceID != "" && fmt.Sprintf("%d", request.GitSourceID) != sourceID {
			continue
		}
		filtered = append(filtered, request)
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": filtered})
}

func (h *Handler) handleCanonicalCreateDeploymentRequest(w http.ResponseWriter, r *http.Request, workspaceID string) {
	var request canonicalDeploymentRequestCreateRequest
	if err := readOptionalJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	actor := strings.TrimSpace(requestActorSubject(r))
	if actor == "" {
		writeError(w, http.StatusBadRequest, "request actor is required")
		return
	}
	sourceID := strings.TrimSpace(firstNonEmpty(request.GitSourceID, request.GitSourceIDCamel))
	sourceID, ok := requireCanonicalGitSourceRouteID(w, sourceID)
	if !ok {
		return
	}
	source, ok := h.requireDeploymentRequestSource(w, r, workspaceID, sourceID)
	if !ok {
		return
	}
	token, err := h.resolveGitSourceCreds(r.Context(), workspaceID, source.TokenEnv)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	deployment, ok := h.validateGitSourceContract(w, r, source, token)
	if !ok {
		return
	}
	now := time.Now().UTC()
	next := canonicalDeploymentRequest{
		ID:             state.NewID("dreq"),
		WorkspaceID:    contract.NormalizeWorkspace(workspaceID),
		GitSourceID:    parseCanonicalGitSourceID(source.ID),
		SourceName:     source.Name,
		RepoURL:        source.RepoURL,
		Branch:         firstNonEmpty(source.Branch, "main"),
		Subpath:        source.Subpath,
		Status:         "pending",
		AppKey:         deployment.App,
		Entrypoint:     deployment.Entrypoint,
		TargetCommit:   deployment.Commit,
		ActionsCount:   len(deployment.Actions),
		RequestedBy:    actor,
		RequestMessage: deploymentRequestMessage(request.Message, request.MessageCamel),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if source.LastSyncedCommit != nil {
		next.CurrentCommit = *source.LastSyncedCommit
	}
	requests, ok := h.loadCanonicalDeploymentRequests(w, r.Context(), workspaceID)
	if !ok {
		return
	}
	for _, existing := range requests {
		if existing.Status == "pending" && existing.GitSourceID == next.GitSourceID {
			writeError(w, http.StatusConflict, "pending deployment request already exists for this source")
			return
		}
	}
	requests = append([]canonicalDeploymentRequest{next}, requests...)
	if !h.saveCanonicalDeploymentRequests(w, r.Context(), workspaceID, trimDeploymentRequests(requests)) {
		return
	}
	writeJSON(w, http.StatusCreated, next)
}

func (h *Handler) handleCanonicalDeployDeploymentRequest(w http.ResponseWriter, r *http.Request, workspaceID string, requestID string) {
	var action canonicalDeploymentRequestActionRequest
	if err := readOptionalJSON(r, &action); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if !deploymentRequestConfirmed(action) {
		writeError(w, http.StatusBadRequest, "deploy confirmation is required")
		return
	}
	actor := strings.TrimSpace(requestActorSubject(r))
	if actor == "" {
		writeError(w, http.StatusBadRequest, "deploy actor is required")
		return
	}
	requests, request, index, ok := h.requireDeploymentRequest(w, r.Context(), workspaceID, requestID)
	if !ok {
		return
	}
	if request.Status != "pending" {
		writeError(w, http.StatusConflict, "deployment request is not pending")
		return
	}
	source, ok := h.requireDeploymentRequestSource(w, r, workspaceID, fmt.Sprintf("%d", request.GitSourceID))
	if !ok {
		return
	}
	deploymentID := newDeploymentOperationID()
	message := deployRequestMessage(canonicalGitSourceDeployRequest{
		Message:      action.Message,
		MessageCamel: action.MessageCamel,
	})
	if message == nil && strings.TrimSpace(request.RequestMessage) != "" {
		msg := request.RequestMessage
		message = &msg
	}
	deployment, ok := h.syncGitSource(w, r, workspaceID, source, gitSourceOperationAudit{
		Source:       "deployment_request",
		Commit:       request.TargetCommit,
		DeploymentID: &deploymentID,
		Message:      message,
		CreatedBy:    &actor,
	})
	if !ok {
		return
	}
	now := time.Now().UTC()
	request.Status = "deployed"
	request.OperatorMessage = deploymentRequestMessage(action.Message, action.MessageCamel)
	request.ReviewedBy = &actor
	request.DeployedBy = &actor
	request.DeploymentID = &deploymentID
	request.DeployedCommit = &deployment.Commit
	request.DeployedAt = &now
	request.UpdatedAt = now
	requests[index] = request
	if !h.saveCanonicalDeploymentRequests(w, r.Context(), workspaceID, requests) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"request":     request,
		"sync_result": newCanonicalSyncResult(deployment),
	})
}

func (h *Handler) handleCanonicalRejectDeploymentRequest(w http.ResponseWriter, r *http.Request, workspaceID string, requestID string) {
	var action canonicalDeploymentRequestActionRequest
	if err := readOptionalJSON(r, &action); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	actor := strings.TrimSpace(requestActorSubject(r))
	if actor == "" {
		writeError(w, http.StatusBadRequest, "operator actor is required")
		return
	}
	requests, request, index, ok := h.requireDeploymentRequest(w, r.Context(), workspaceID, requestID)
	if !ok {
		return
	}
	if request.Status != "pending" {
		writeError(w, http.StatusConflict, "deployment request is not pending")
		return
	}
	now := time.Now().UTC()
	request.Status = "rejected"
	request.OperatorMessage = deploymentRequestMessage(action.Message, action.MessageCamel)
	request.ReviewedBy = &actor
	request.UpdatedAt = now
	requests[index] = request
	if !h.saveCanonicalDeploymentRequests(w, r.Context(), workspaceID, requests) {
		return
	}
	writeJSON(w, http.StatusOK, request)
}

func (h *Handler) requireDeploymentRequestSource(w http.ResponseWriter, r *http.Request, workspaceID string, sourceID string) (gitsourcepkg.Source, bool) {
	if h.gitSources == nil {
		writeError(w, http.StatusServiceUnavailable, "git source registry is not configured")
		return gitsourcepkg.Source{}, false
	}
	source, err := h.gitSources.Get(r.Context(), workspaceID, sourceID)
	if err != nil {
		if errors.Is(err, gitsourcepkg.ErrGitSourceNotFound) {
			writeError(w, http.StatusNotFound, "git source not found")
			return gitsourcepkg.Source{}, false
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return gitsourcepkg.Source{}, false
	}
	return source, true
}

func (h *Handler) requireDeploymentRequest(w http.ResponseWriter, ctx context.Context, workspaceID string, requestID string) ([]canonicalDeploymentRequest, canonicalDeploymentRequest, int, bool) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		writeError(w, http.StatusBadRequest, "deployment request id is required")
		return nil, canonicalDeploymentRequest{}, -1, false
	}
	requests, ok := h.loadCanonicalDeploymentRequests(w, ctx, workspaceID)
	if !ok {
		return nil, canonicalDeploymentRequest{}, -1, false
	}
	for index, request := range requests {
		if request.ID == requestID {
			return requests, request, index, true
		}
	}
	writeError(w, http.StatusNotFound, "deployment request not found")
	return nil, canonicalDeploymentRequest{}, -1, false
}

func (h *Handler) loadCanonicalDeploymentRequests(w http.ResponseWriter, ctx context.Context, workspaceID string) ([]canonicalDeploymentRequest, bool) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return nil, false
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	value, found, err := h.store.GetState(ctx, workspaceID, deploymentRequestsStatePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if !found || len(value) == 0 || string(value) == "null" {
		return []canonicalDeploymentRequest{}, true
	}
	var store deploymentRequestStore
	if err := json.Unmarshal(value, &store); err != nil {
		writeError(w, http.StatusInternalServerError, "deployment request state is invalid JSON")
		return nil, false
	}
	requests := append([]canonicalDeploymentRequest(nil), store.Requests...)
	sort.SliceStable(requests, func(i, j int) bool {
		return requests[i].CreatedAt.After(requests[j].CreatedAt)
	})
	return requests, true
}

func (h *Handler) saveCanonicalDeploymentRequests(w http.ResponseWriter, ctx context.Context, workspaceID string, requests []canonicalDeploymentRequest) bool {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return false
	}
	payload, err := json.Marshal(deploymentRequestStore{Requests: requests})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	if err := h.store.SetState(ctx, workspaceID, deploymentRequestsStatePath, payload); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	return true
}

func deploymentRequestMessage(values ...*string) string {
	value, ok := firstPresentString(values...)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func deploymentRequestConfirmed(request canonicalDeploymentRequestActionRequest) bool {
	return request.Confirm || request.Confirmed || request.ConfirmCamel || request.ConfirmedCamel
}

func trimDeploymentRequests(requests []canonicalDeploymentRequest) []canonicalDeploymentRequest {
	const maxRequests = 200
	if len(requests) <= maxRequests {
		return requests
	}
	return requests[:maxRequests]
}
