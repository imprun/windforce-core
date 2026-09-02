package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/state"
)

type admissionOutcomeResolveRequest struct {
	RequestFingerprint string `json:"request_fingerprint"`
}

type admissionOutcomeView struct {
	WorkspaceID        string    `json:"workspace_id"`
	AdmissionID        string    `json:"admission_id"`
	RunID              string    `json:"run_id,omitempty"`
	State              string    `json:"state"`
	RequestFingerprint string    `json:"request_fingerprint,omitempty"`
	CreatedAt          time.Time `json:"created_at,omitempty"`
	UpdatedAt          time.Time `json:"updated_at,omitempty"`
}

func (h *Handler) handleAdmissionOutcome(w http.ResponseWriter, r *http.Request, workspaceID string, admissionID string, resolve bool) {
	principal := workspacePrincipalFrom(r.Context())
	if principal == nil || !principal.Admin || principal.Workspace != workspaceID {
		writeError(w, http.StatusForbidden, "administrator authority is required")
		return
	}
	if resolve {
		var request admissionOutcomeResolveRequest
		if err := decodeStrictControlJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		outcome, err := h.store.ResolveAdmissionOutcome(
			r.Context(), workspaceID, admissionID, request.RequestFingerprint, requestActorSubject(r),
		)
		if err != nil {
			writeStateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, admissionOutcomeViewOf(outcome))
		return
	}
	outcome, found, err := h.store.GetAdmissionOutcome(r.Context(), workspaceID, admissionID)
	if err != nil {
		writeStateError(w, err)
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, admissionOutcomeView{
			WorkspaceID: workspaceID, AdmissionID: strings.TrimSpace(admissionID), State: "unknown",
		})
		return
	}
	writeJSON(w, http.StatusOK, admissionOutcomeViewOf(outcome))
}

func admissionOutcomeViewOf(outcome state.AdmissionOutcome) admissionOutcomeView {
	return admissionOutcomeView{
		WorkspaceID: outcome.WorkspaceID, AdmissionID: outcome.AdmissionID, RunID: outcome.RunID,
		State: string(outcome.State), RequestFingerprint: outcome.RequestFingerprint,
		CreatedAt: outcome.CreatedAt, UpdatedAt: outcome.UpdatedAt,
	}
}

func decodeStrictControlJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("request body does not match the control API specification")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}
