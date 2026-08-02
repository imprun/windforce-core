package server

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/state"
)

type workspaceView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	CreatedBy string `json:"created_by"`
	UpdatedBy string `json:"updated_by"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func workspaceResponse(workspace state.Workspace) workspaceView {
	return workspaceView{
		ID: workspace.ID, Name: workspace.Name, Status: workspace.Status,
		CreatedBy: workspace.CreatedBy, UpdatedBy: workspace.UpdatedBy,
		CreatedAt: workspace.CreatedAt.UTC().Format(timeLayout), UpdatedAt: workspace.UpdatedAt.UTC().Format(timeLayout),
	}
}

type workspaceTokenView struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Status      string  `json:"status"`
	CreatedBy   string  `json:"created_by"`
	UpdatedBy   string  `json:"updated_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	RevokedAt   *string `json:"revoked_at,omitempty"`
}

func workspaceTokenResponse(token state.WorkspaceToken) workspaceTokenView {
	view := workspaceTokenView{
		ID: token.ID, WorkspaceID: token.WorkspaceID, Name: token.Name,
		Status: "active", CreatedBy: token.CreatedBy, UpdatedBy: token.UpdatedBy,
		CreatedAt: token.CreatedAt.UTC().Format(timeLayout), UpdatedAt: token.UpdatedAt.UTC().Format(timeLayout),
	}
	if token.RevokedAt != nil {
		value := token.RevokedAt.UTC().Format(timeLayout)
		view.Status = "revoked"
		view.RevokedAt = &value
	}
	return view
}

const timeLayout = "2006-01-02T15:04:05.000000000Z07:00"

func (h *Handler) handleWorkspaceAPI(w http.ResponseWriter, r *http.Request, parts []string) bool {
	if len(parts) < 2 || parts[0] != "api" || parts[1] != "workspaces" {
		return false
	}
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return true
	}
	if len(parts) == 2 && r.Method == http.MethodGet {
		items, err := h.store.ListWorkspaces(r.Context())
		if err != nil {
			writeStateError(w, err)
			return true
		}
		views := make([]workspaceView, 0, len(items))
		for _, item := range items {
			views = append(views, workspaceResponse(item))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": views})
		return true
	}
	if len(parts) == 2 && r.Method == http.MethodPost {
		var request struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := readRequiredJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "valid workspace JSON is required")
			return true
		}
		request.ID = strings.TrimSpace(request.ID)
		request.Name = strings.TrimSpace(request.Name)
		if !contract.ValidWorkspaceID(request.ID) {
			writeError(w, http.StatusBadRequest, "workspace id must start with a lowercase letter and contain only lowercase letters, digits, or hyphens (2-48 characters)")
			return true
		}
		if request.Name == "" || len(request.Name) > 100 {
			writeError(w, http.StatusBadRequest, "workspace name is required and must be at most 100 characters")
			return true
		}
		workspace, err := h.store.CreateWorkspace(r.Context(), request.ID, request.Name, requestActorOrSystem(r))
		if err != nil {
			writeStateError(w, err)
			return true
		}
		writeJSON(w, http.StatusCreated, workspaceResponse(workspace))
		return true
	}
	if len(parts) < 3 {
		return false
	}
	workspaceID := parts[2]
	if len(parts) == 3 && r.Method == http.MethodGet {
		workspace, err := h.store.GetWorkspace(r.Context(), workspaceID)
		if err != nil {
			writeStateError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, workspaceResponse(workspace))
		return true
	}
	if len(parts) == 3 && r.Method == http.MethodPatch {
		var request struct {
			Name string `json:"name"`
		}
		if err := readRequiredJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "valid workspace JSON is required")
			return true
		}
		request.Name = strings.TrimSpace(request.Name)
		if request.Name == "" || len(request.Name) > 100 {
			writeError(w, http.StatusBadRequest, "workspace name is required and must be at most 100 characters")
			return true
		}
		workspace, err := h.store.UpdateWorkspace(r.Context(), workspaceID, request.Name, requestActorOrSystem(r))
		if err != nil {
			writeStateError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, workspaceResponse(workspace))
		return true
	}
	if len(parts) == 3 && r.Method == http.MethodDelete {
		if err := h.store.DeleteWorkspace(r.Context(), workspaceID, requestActorOrSystem(r)); err != nil {
			writeStateError(w, err)
			return true
		}
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	if len(parts) == 4 && parts[3] == "archive" && r.Method == http.MethodPost {
		workspace, err := h.store.ArchiveWorkspace(r.Context(), workspaceID, requestActorOrSystem(r))
		if err != nil {
			writeStateError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, workspaceResponse(workspace))
		return true
	}
	if len(parts) == 4 && parts[3] == "tokens" && r.Method == http.MethodGet {
		items, err := h.store.ListWorkspaceTokens(r.Context(), workspaceID)
		if err != nil {
			writeStateError(w, err)
			return true
		}
		views := make([]workspaceTokenView, 0, len(items))
		for _, item := range items {
			views = append(views, workspaceTokenResponse(item))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": views})
		return true
	}
	if len(parts) == 4 && parts[3] == "tokens" && r.Method == http.MethodPost {
		var request struct {
			Name string `json:"name"`
		}
		if err := readRequiredJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "valid token JSON is required")
			return true
		}
		request.Name = strings.TrimSpace(request.Name)
		if request.Name == "" || len(request.Name) > 100 {
			writeError(w, http.StatusBadRequest, "token name is required and must be at most 100 characters")
			return true
		}
		token, err := newWorkspaceToken()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not generate workspace token")
			return true
		}
		created, err := h.store.CreateWorkspaceToken(
			r.Context(), workspaceID, request.Name, state.HashWorkspaceToken(token), requestActorOrSystem(r),
		)
		if err != nil {
			writeStateError(w, err)
			return true
		}
		writeJSON(w, http.StatusCreated, map[string]any{"token": workspaceTokenResponse(created), "api_token": token})
		return true
	}
	if len(parts) == 6 && parts[3] == "tokens" && parts[5] == "rotate" && r.Method == http.MethodPost {
		token, err := newWorkspaceToken()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not generate workspace token")
			return true
		}
		updated, err := h.store.RotateWorkspaceToken(
			r.Context(), workspaceID, strings.TrimSpace(parts[4]), state.HashWorkspaceToken(token), requestActorOrSystem(r),
		)
		if err != nil {
			writeStateError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{"token": workspaceTokenResponse(updated), "api_token": token})
		return true
	}
	if len(parts) == 5 && parts[3] == "tokens" && r.Method == http.MethodDelete {
		updated, err := h.store.RevokeWorkspaceToken(
			r.Context(), workspaceID, strings.TrimSpace(parts[4]), requestActorOrSystem(r),
		)
		if err != nil {
			writeStateError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, workspaceTokenResponse(updated))
		return true
	}
	if len(parts) == 4 && parts[3] == "audit" && r.Method == http.MethodGet {
		if _, err := h.store.GetWorkspace(r.Context(), workspaceID); err != nil {
			writeStateError(w, err)
			return true
		}
		items, err := h.store.ListWorkspaceAudit(r.Context(), workspaceID)
		if err != nil {
			writeStateError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return true
	}
	return false
}

func newWorkspaceToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return contract.WorkspaceTokenPrefix + base64.RawURLEncoding.EncodeToString(data), nil
}

func requestActorOrSystem(r *http.Request) string {
	if actor := requestActorSubject(r); actor != "" {
		return actor
	}
	return "system"
}

func workspaceNotFound(err error) bool {
	return errors.Is(err, state.ErrNotFound)
}
