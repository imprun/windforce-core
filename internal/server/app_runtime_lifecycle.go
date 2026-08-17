package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/imprun/windforce-core/internal/state"
)

func (h *Handler) handleAppRuntimeLifecycle(w http.ResponseWriter, r *http.Request, workspaceID, appKey string) {
	switch r.Method {
	case http.MethodGet:
		lifecycle, err := h.store.GetAppRuntimeLifecycle(r.Context(), workspaceID, appKey)
		if err != nil {
			writeStateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, lifecycle)
	case http.MethodPut:
		var request struct {
			State            state.AppRuntimeState `json:"state"`
			Reason           string                `json:"reason"`
			ExpectedRevision *int64                `json:"expectedRevision,omitempty"`
		}
		body, err := readJSONBody(r)
		if err != nil || json.Unmarshal(body, &request) != nil {
			writeError(w, http.StatusBadRequest, "valid App runtime lifecycle body required")
			return
		}
		lifecycle, err := h.store.SetAppRuntimeLifecycle(r.Context(), state.SetAppRuntimeLifecycleRequest{
			WorkspaceID: workspaceID, AppKey: appKey, State: request.State, Reason: request.Reason,
			Actor: firstNonEmpty(strings.TrimSpace(requestActorSubject(r)), "system"), ExpectedRevision: request.ExpectedRevision,
		})
		if err != nil {
			writeRuntimeConfigStateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, lifecycle)
	default:
		w.Header().Set("Allow", "GET, PUT")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleAppRuntimeLifecycleAudit(w http.ResponseWriter, r *http.Request, workspaceID, appKey string) {
	audits, err := h.store.ListAppRuntimeLifecycleAudit(r.Context(), workspaceID, appKey)
	if err != nil {
		writeStateError(w, err)
		return
	}
	if audits == nil {
		audits = make([]state.AppRuntimeLifecycleAudit, 0)
	}
	writeJSON(w, http.StatusOK, map[string]any{"audits": audits})
}

func (h *Handler) handlePurgeAppRuntimeConfig(w http.ResponseWriter, r *http.Request, workspaceID, appKey string) {
	force, err := strconv.ParseBool(firstNonEmpty(r.URL.Query().Get("force"), "false"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "force must be a boolean")
		return
	}
	if force && r.Header.Get("X-Windforce-Confirm-Force-Purge") != appKey {
		writeError(w, http.StatusBadRequest, "force purge requires the App key confirmation header")
		return
	}
	var request struct {
		Reason string `json:"reason"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		body, readErr := readJSONBody(r)
		if readErr != nil || json.Unmarshal(body, &request) != nil {
			writeError(w, http.StatusBadRequest, "valid purge body required")
			return
		}
	}
	err = h.store.PurgeAppRuntimeConfig(r.Context(), state.PurgeAppRuntimeConfigRequest{
		WorkspaceID: workspaceID, AppKey: appKey, Actor: firstNonEmpty(strings.TrimSpace(requestActorSubject(r)), "system"),
		Reason: request.Reason, Force: force,
	})
	if err != nil {
		writeRuntimeConfigStateError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
