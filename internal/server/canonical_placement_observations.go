package server

import (
	"errors"
	"net/http"

	catalogpkg "github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/state"
)

func (h *Handler) placementObservationStore() (state.PlacementObservationStore, bool) {
	if h.store == nil {
		return nil, false
	}
	store, ok := h.store.(state.PlacementObservationStore)
	return store, ok
}

func (h *Handler) handleCanonicalWorkerGroupInventory(
	w http.ResponseWriter,
	r *http.Request,
	workspaceID string,
) {
	store, ok := h.placementObservationStore()
	if !ok {
		writeError(w, http.StatusNotImplemented, "placement observations are not supported by this store")
		return
	}
	principal := workspacePrincipalFrom(r.Context())
	result, err := store.GetWorkerGroupInventory(r.Context(), workspaceID, principal != nil && principal.Admin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) handleCanonicalPlacementCandidates(
	w http.ResponseWriter,
	r *http.Request,
	workspaceID string,
	app string,
	action string,
) {
	if !validAppKey(app) || (action != "" && !validActionKey(action)) {
		writeError(w, http.StatusBadRequest, "invalid App or Action key")
		return
	}
	store, ok := h.placementObservationStore()
	if !ok {
		writeError(w, http.StatusNotImplemented, "placement observations are not supported by this store")
		return
	}
	principal := workspacePrincipalFrom(r.Context())
	result, err := store.GetPlacementCandidates(
		r.Context(), workspaceID, app, action, principal != nil && principal.Admin,
	)
	if errors.Is(err, catalogpkg.ErrDeploymentNotFound) {
		writeError(w, http.StatusNotFound, "app not found")
		return
	}
	if errors.Is(err, catalogpkg.ErrActionNotFound) {
		writeError(w, http.StatusNotFound, "action not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
