package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/state"
)

type canonicalHTTPRouteBindingRequest struct {
	Hostname   string `json:"hostname,omitempty"`
	Path       string `json:"path"`
	Visibility string `json:"visibility,omitempty"`
	Provider   string `json:"provider,omitempty"`
}

type canonicalHTTPRouteBindingStatusRequest struct {
	State              string `json:"state"`
	PublicURL          string `json:"public_url,omitempty"`
	ErrorSummary       string `json:"error_summary,omitempty"`
	ObservedGeneration int64  `json:"observed_generation"`
}

type canonicalHTTPRouteBinding struct {
	ID                 string     `json:"id"`
	WorkspaceID        string     `json:"workspace_id"`
	TriggerID          string     `json:"trigger_id"`
	Hostname           string     `json:"hostname,omitempty"`
	Path               string     `json:"path"`
	Visibility         string     `json:"visibility"`
	Provider           string     `json:"provider"`
	State              string     `json:"state"`
	PublicURL          string     `json:"public_url,omitempty"`
	ErrorSummary       string     `json:"error_summary,omitempty"`
	Generation         int64      `json:"generation"`
	ObservedGeneration int64      `json:"observed_generation"`
	CreatedBy          string     `json:"created_by"`
	UpdatedBy          string     `json:"updated_by"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	DeleteRequestedAt  *time.Time `json:"delete_requested_at,omitempty"`
	DeletedAt          *time.Time `json:"deleted_at,omitempty"`
}

func (h *Handler) handleCanonicalHTTPRouteBindingAPI(w http.ResponseWriter, r *http.Request, parts []string) bool {
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "w" {
		return false
	}
	workspaceID := parts[2]
	if parts[3] == "http-route-bindings" {
		switch {
		case len(parts) == 4 && r.Method == http.MethodGet:
			h.handleCanonicalProviderHTTPRouteBindings(w, r, workspaceID)
		case len(parts) == 6 && parts[5] == "status" && r.Method == http.MethodPut:
			h.handleCanonicalHTTPRouteBindingStatus(w, r, workspaceID, parts[4])
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return true
	}
	if parts[3] != "triggers" || len(parts) < 6 || parts[5] != "routes" {
		return false
	}
	triggerID := parts[4]
	switch {
	case len(parts) == 6 && r.Method == http.MethodGet:
		h.handleCanonicalHTTPRouteBindings(w, r, workspaceID, triggerID)
	case len(parts) == 6 && r.Method == http.MethodPost:
		h.handleCanonicalCreateHTTPRouteBinding(w, r, workspaceID, triggerID)
	case len(parts) == 7 && r.Method == http.MethodGet:
		h.handleCanonicalHTTPRouteBinding(w, r, workspaceID, triggerID, parts[6])
	case len(parts) == 7 && r.Method == http.MethodPut:
		h.handleCanonicalUpdateHTTPRouteBinding(w, r, workspaceID, triggerID, parts[6])
	case len(parts) == 7 && r.Method == http.MethodDelete:
		h.handleCanonicalDeleteHTTPRouteBinding(w, r, workspaceID, triggerID, parts[6])
	case len(parts) == 8 && parts[7] == "audit" && r.Method == http.MethodGet:
		h.handleCanonicalHTTPRouteBindingAudit(w, r, workspaceID, triggerID, parts[6])
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
	return true
}

func (h *Handler) handleCanonicalHTTPRouteBindings(w http.ResponseWriter, r *http.Request, workspaceID string, triggerID string) {
	items, err := h.store.ListHTTPRouteBindings(r.Context(), workspaceID, triggerID, false)
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": canonicalHTTPRouteBindingsFrom(items)})
}

func (h *Handler) handleCanonicalProviderHTTPRouteBindings(w http.ResponseWriter, r *http.Request, workspaceID string) {
	includeDeletedRaw := r.URL.Query().Get("include_deleted")
	if includeDeletedRaw == "" {
		includeDeletedRaw = "false"
	}
	includeDeleted, err := strconv.ParseBool(includeDeletedRaw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "include_deleted must be true or false")
		return
	}
	items, err := h.store.ListHTTPRouteBindings(r.Context(), workspaceID, "", includeDeleted)
	if err != nil {
		writeStateError(w, err)
		return
	}
	provider := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider")))
	stateFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("state")))
	filtered := make([]state.HTTPRouteBinding, 0, len(items))
	for _, item := range items {
		if provider != "" && item.Provider != provider && item.Provider != "auto" {
			continue
		}
		if stateFilter != "" && item.State != stateFilter {
			continue
		}
		filtered = append(filtered, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":               canonicalHTTPRouteBindingsFrom(filtered),
		"configured_provider": h.httpRouteProvider,
	})
}

func (h *Handler) handleCanonicalHTTPRouteBinding(w http.ResponseWriter, r *http.Request, workspaceID string, triggerID string, id string) {
	binding, err := h.store.GetHTTPRouteBinding(r.Context(), workspaceID, triggerID, id)
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, canonicalHTTPRouteBindingFrom(binding))
}

func (h *Handler) handleCanonicalCreateHTTPRouteBinding(w http.ResponseWriter, r *http.Request, workspaceID string, triggerID string) {
	var request canonicalHTTPRouteBindingRequest
	if err := readRequiredJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := h.store.CreateHTTPRouteBinding(r.Context(), httpRouteBindingFromRequest(workspaceID, triggerID, "", request), requestActorSubjectUTF8(r))
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, canonicalHTTPRouteBindingFrom(created))
}

func (h *Handler) handleCanonicalUpdateHTTPRouteBinding(w http.ResponseWriter, r *http.Request, workspaceID string, triggerID string, id string) {
	var request canonicalHTTPRouteBindingRequest
	if err := readRequiredJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := h.store.UpdateHTTPRouteBinding(r.Context(), httpRouteBindingFromRequest(workspaceID, triggerID, id, request), requestActorSubjectUTF8(r))
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, canonicalHTTPRouteBindingFrom(updated))
}

func (h *Handler) handleCanonicalDeleteHTTPRouteBinding(w http.ResponseWriter, r *http.Request, workspaceID string, triggerID string, id string) {
	updated, err := h.store.RequestDeleteHTTPRouteBinding(r.Context(), workspaceID, triggerID, id, requestActorSubjectUTF8(r))
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, canonicalHTTPRouteBindingFrom(updated))
}

func (h *Handler) handleCanonicalHTTPRouteBindingStatus(w http.ResponseWriter, r *http.Request, workspaceID string, id string) {
	var request canonicalHTTPRouteBindingStatusRequest
	if err := readRequiredJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := h.store.UpdateHTTPRouteBindingStatus(r.Context(), workspaceID, id, state.HTTPRouteBindingStatus{
		State:              request.State,
		PublicURL:          request.PublicURL,
		ErrorSummary:       request.ErrorSummary,
		ObservedGeneration: request.ObservedGeneration,
	}, requestActorSubjectUTF8(r))
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, canonicalHTTPRouteBindingFrom(updated))
}

func (h *Handler) handleCanonicalHTTPRouteBindingAudit(w http.ResponseWriter, r *http.Request, workspaceID string, triggerID string, id string) {
	items, err := h.store.ListHTTPRouteBindingAudit(r.Context(), workspaceID, triggerID, id)
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func httpRouteBindingFromRequest(workspaceID string, triggerID string, id string, request canonicalHTTPRouteBindingRequest) state.HTTPRouteBinding {
	return state.HTTPRouteBinding{
		ID:          id,
		WorkspaceID: workspaceID,
		TriggerID:   triggerID,
		Hostname:    request.Hostname,
		Path:        request.Path,
		Visibility:  request.Visibility,
		Provider:    request.Provider,
	}
}

func canonicalHTTPRouteBindingsFrom(bindings []state.HTTPRouteBinding) []canonicalHTTPRouteBinding {
	items := make([]canonicalHTTPRouteBinding, 0, len(bindings))
	for _, binding := range bindings {
		items = append(items, canonicalHTTPRouteBindingFrom(binding))
	}
	return items
}

func canonicalHTTPRouteBindingFrom(binding state.HTTPRouteBinding) canonicalHTTPRouteBinding {
	return canonicalHTTPRouteBinding{
		ID:                 binding.ID,
		WorkspaceID:        binding.WorkspaceID,
		TriggerID:          binding.TriggerID,
		Hostname:           binding.Hostname,
		Path:               binding.Path,
		Visibility:         binding.Visibility,
		Provider:           binding.Provider,
		State:              binding.State,
		PublicURL:          binding.PublicURL,
		ErrorSummary:       binding.ErrorSummary,
		Generation:         binding.Generation,
		ObservedGeneration: binding.ObservedGeneration,
		CreatedBy:          binding.CreatedBy,
		UpdatedBy:          binding.UpdatedBy,
		CreatedAt:          binding.CreatedAt,
		UpdatedAt:          binding.UpdatedAt,
		DeleteRequestedAt:  binding.DeleteRequestedAt,
		DeletedAt:          binding.DeletedAt,
	}
}
