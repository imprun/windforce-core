package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/imprun/windforce-core/internal/catalog"
)

type releaseRollbackStore interface {
	RollbackRelease(context.Context, catalog.ReleaseRollbackRequest) (catalog.ReleaseRollbackResult, error)
}

type canonicalReleaseRollbackRequest struct {
	Confirm bool   `json:"confirm"`
	Reason  string `json:"reason"`
}

type canonicalReleaseRollbackResult struct {
	App               string `json:"app"`
	ActiveReleaseID   string `json:"active_release_id"`
	PreviousReleaseID string `json:"previous_release_id"`
	Commit            string `json:"commit"`
	BundleDigest      string `json:"bundle_digest"`
	Actor             string `json:"actor"`
	Reason            string `json:"reason"`
	RolledBackAt      string `json:"rolled_back_at"`
}

func (h *Handler) handleCanonicalReleaseRollback(w http.ResponseWriter, r *http.Request, workspaceID string, app string, releaseID string) {
	app = strings.TrimSpace(app)
	releaseID = strings.TrimSpace(releaseID)
	if !validAppKey(app) || releaseID == "" {
		writeError(w, http.StatusBadRequest, "invalid app/release id")
		return
	}
	var request canonicalReleaseRollbackRequest
	if err := readOptionalJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if !request.Confirm {
		writeError(w, http.StatusBadRequest, "rollback confirmation is required")
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" {
		writeError(w, http.StatusBadRequest, "rollback reason is required")
		return
	}
	actor := strings.TrimSpace(requestActorSubject(r))
	if actor == "" {
		writeError(w, http.StatusBadRequest, "rollback actor is required")
		return
	}
	snapshot, ok := h.loadCatalogSnapshot(w, r)
	if !ok {
		return
	}
	target, err := catalog.FindRelease(snapshot, workspaceID, app, releaseID)
	if errors.Is(err, catalog.ErrReleaseNotFound) {
		writeError(w, http.StatusNotFound, "release not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if snapshot.ActiveHistoryIDs[catalog.DeploymentKey(workspaceID, app)] == target.ID {
		writeError(w, http.StatusConflict, "release is already active")
		return
	}
	if target.Deployment.BundleDigest == "" {
		writeError(w, http.StatusUnprocessableEntity, "release has no execution bundle and cannot be activated")
		return
	}
	if h.executionBundles == nil {
		writeError(w, http.StatusServiceUnavailable, "execution bundle manager is not configured")
		return
	}
	if err := h.executionBundles.ValidateExecutionBundle(r.Context(), target.Deployment); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "execution bundle validation failed: "+err.Error())
		return
	}
	rollbackStore, ok := h.catalog.(releaseRollbackStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "transactional release rollback is not configured")
		return
	}
	result, err := rollbackStore.RollbackRelease(r.Context(), catalog.ReleaseRollbackRequest{
		Workspace: workspaceID,
		App:       app,
		ReleaseID: releaseID,
		Actor:     actor,
		Reason:    request.Reason,
	})
	if err != nil {
		switch {
		case errors.Is(err, catalog.ErrReleaseNotFound):
			writeError(w, http.StatusNotFound, "release not found")
		case errors.Is(err, catalog.ErrReleaseAlreadyActive):
			writeError(w, http.StatusConflict, "release is already active")
		case errors.Is(err, catalog.ErrInvalidRollback):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, canonicalReleaseRollbackResult{
		App:               result.Target.App,
		ActiveReleaseID:   result.Target.ID,
		PreviousReleaseID: result.PreviousReleaseID,
		Commit:            result.Target.Commit,
		BundleDigest:      result.Target.Deployment.BundleDigest,
		Actor:             result.Audit.Actor,
		Reason:            result.Reason,
		RolledBackAt:      result.RolledBackAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
	})
}
