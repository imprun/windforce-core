package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/imprun/windforce-core/internal/resourceconfig"
	"github.com/imprun/windforce-core/internal/state"
)

func (h *Handler) handleListResourceTypes(w http.ResponseWriter, r *http.Request, workspaceID string) {
	items, err := h.store.ListResourceTypes(r.Context(), workspaceID)
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) handleSetResourceType(w http.ResponseWriter, r *http.Request, workspaceID string) {
	var request state.ResourceType
	body, err := readJSONBody(r)
	if err != nil || json.Unmarshal(body, &request) != nil || strings.TrimSpace(request.Name) == "" {
		writeError(w, http.StatusBadRequest, "valid resource type name and schema are required")
		return
	}
	if err := resourceconfig.ValidateSchema(request.Schema); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(request.Version) == "" {
		request.Version = "1"
	}
	if err := h.store.SetResourceType(r.Context(), workspaceID, request); err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, request)
}

func (h *Handler) handleGetResourceType(w http.ResponseWriter, r *http.Request, workspaceID string, name string, version string) {
	item, found, err := h.store.GetResourceType(r.Context(), workspaceID, name, version)
	if err != nil {
		writeStateError(w, err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "resource type not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) handleDeleteResourceType(w http.ResponseWriter, r *http.Request, workspaceID string, name string, version string) {
	if err := h.store.DeleteResourceType(r.Context(), workspaceID, name, version); err != nil {
		writeStateError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
