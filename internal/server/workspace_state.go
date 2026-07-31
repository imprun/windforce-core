package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/secretbackend"
	"github.com/imprun/windforce-core/internal/state"
)

func (h *Handler) handleGetState(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	statePath := r.URL.Query().Get("path")
	if statePath == "" {
		writeError(w, http.StatusBadRequest, "path query required")
		return
	}
	value, _, err := h.store.GetState(r.Context(), workspaceID, statePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(rawOrNull(value))
}

func (h *Handler) handleSetState(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	statePath := r.URL.Query().Get("path")
	if statePath == "" {
		writeError(w, http.StatusBadRequest, "path query required")
		return
	}
	defer r.Body.Close()
	body, _ := io.ReadAll(r.Body)
	if err := h.store.SetState(r.Context(), workspaceID, statePath, body); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": statePath})
}

func (h *Handler) handleListVariables(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	variables, err := h.store.ListVariables(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range variables {
		if variables[i].IsSecret {
			variables[i].Value = ""
		}
	}
	writeJSON(w, http.StatusOK, variables)
}

func (h *Handler) handleSetVariable(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	var request struct {
		Path        string `json:"path"`
		Value       string `json:"value"`
		Description string `json:"description"`
		IsSecret    bool   `json:"is_secret"`
		AppKey      string `json:"app_key"`
	}
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "path required")
		return
	}
	if err := json.Unmarshal(body, &request); err != nil || request.Path == "" {
		writeError(w, http.StatusBadRequest, "path required")
		return
	}
	if request.AppKey != "" && !validAppKey(request.AppKey) {
		writeError(w, http.StatusBadRequest, "invalid app key")
		return
	}
	normalizedPath, err := contract.NormalizeRuntimeConfigPath(request.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	request.Path = normalizedPath
	value := request.Value
	if request.IsSecret {
		encrypted, err := h.secretBackend.Store(r.Context(), secretbackend.Reference{
			WorkspaceID: workspaceID,
			Kind:        "variable",
			Path:        request.Path,
		}, request.Value)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		value = encrypted
	}
	if err := h.store.SetVariable(r.Context(), workspaceID, request.AppKey, request.Path, value, request.IsSecret, request.Description); err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": request.Path, "app_key": request.AppKey})
}

func (h *Handler) handleGetVariable(w http.ResponseWriter, r *http.Request, workspaceID string, variablePath string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	job, runtime, scopeErr := h.jobRuntimeScope(r, workspaceID)
	if scopeErr != nil {
		writeStateError(w, scopeErr)
		return
	}
	if runtime {
		value, secret, err := h.runtimeResolver.ResolveJobVariable(r.Context(), job, variablePath)
		if err != nil {
			writeStateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"path": variablePath, "value": value, "is_secret": secret})
		return
	}

	variable, found, err := h.store.GetVariableExact(r.Context(), workspaceID, r.URL.Query().Get("app"), variablePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "variable not found")
		return
	}
	if variable.IsSecret {
		writeJSON(w, http.StatusOK, map[string]any{
			"path":       variable.Path,
			"is_secret":  true,
			"configured": variable.Value != "",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": variable.Path, "value": variable.Value, "is_secret": false})
}

func (h *Handler) encryptSecretVariable(ctx context.Context, workspaceID string, value string) (string, error) {
	return h.secretBackend.Store(ctx, secretbackend.Reference{
		WorkspaceID: workspaceID,
		Kind:        "variable",
	}, value)
}

func (h *Handler) decryptSecretVariable(ctx context.Context, workspaceID string, value string) (string, error) {
	return h.secretBackend.Resolve(ctx, secretbackend.Reference{
		WorkspaceID: workspaceID,
		Kind:        "variable",
	}, value)
}

func (h *Handler) jobRuntimeScope(r *http.Request, workspaceID string) (state.Job, bool, error) {
	principal := jobPrincipalFrom(r.Context())
	if principal == nil || principal.JobID == "" {
		return state.Job{}, false, nil
	}
	job, _, found, err := h.store.GetJob(r.Context(), workspaceID, principal.JobID)
	if err != nil {
		return state.Job{}, true, err
	}
	if !found {
		return state.Job{}, true, state.ErrNotFound
	}
	if principal.Attempt <= 0 || job.Attempt != principal.Attempt {
		return state.Job{}, true, state.ErrInvalidState
	}
	if job.State != state.JobRunning || job.LeaseOwner == "" ||
		job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.After(time.Now()) {
		return state.Job{}, true, state.ErrInvalidState
	}
	return job, true, nil
}

func (h *Handler) handleDeleteVariable(w http.ResponseWriter, r *http.Request, workspaceID string, variablePath string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	if err := h.store.DeleteVariable(r.Context(), workspaceID, r.URL.Query().Get("app"), variablePath); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleSetResource(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	var request struct {
		Path         string          `json:"path"`
		Value        json.RawMessage `json:"value"`
		ResourceType string          `json:"resource_type"`
		Description  string          `json:"description"`
	}
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "path required")
		return
	}
	if err := json.Unmarshal(body, &request); err != nil || request.Path == "" {
		writeError(w, http.StatusBadRequest, "path required")
		return
	}
	if err := h.store.SetResource(r.Context(), workspaceID, request.Path, request.Value, request.ResourceType, request.Description); err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": request.Path})
}

func (h *Handler) handleListResources(w http.ResponseWriter, r *http.Request, workspaceID string) {
	resources, err := h.store.ListResources(r.Context(), workspaceID)
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resources)
}

func (h *Handler) handleGetResource(w http.ResponseWriter, r *http.Request, workspaceID string, resourcePath string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	job, runtime, scopeErr := h.jobRuntimeScope(r, workspaceID)
	if scopeErr != nil {
		writeStateError(w, scopeErr)
		return
	}
	var value json.RawMessage
	if runtime {
		resolved, err := h.runtimeResolver.ResolveJobResource(r.Context(), job, resourcePath)
		if err != nil {
			writeStateError(w, err)
			return
		}
		value = resolved.Value
	} else {
		resource, found, err := h.store.GetResource(r.Context(), workspaceID, resourcePath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !found {
			writeError(w, http.StatusNotFound, "resource not found")
			return
		}
		value = resource.Value
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(rawOrNull(value))
}

func (h *Handler) handleDeleteResource(w http.ResponseWriter, r *http.Request, workspaceID string, resourcePath string) {
	if err := h.store.DeleteResource(r.Context(), workspaceID, resourcePath); err != nil {
		writeStateError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
