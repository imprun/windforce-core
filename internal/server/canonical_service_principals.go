package server

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sort"
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
	executionpkg "github.com/imprun/windforce-core/internal/execution"
	"github.com/imprun/windforce-core/internal/state"
)

type canonicalServicePrincipalRequest struct {
	Name           *string   `json:"name"`
	Scopes         *[]string `json:"scopes"`
	AllowedTargets *[]string `json:"allowed_targets"`
}

type servicePrincipalView struct {
	ID             string   `json:"id"`
	WorkspaceID    string   `json:"workspace_id"`
	Name           string   `json:"name"`
	HasToken       bool     `json:"has_token"`
	Scopes         []string `json:"scopes"`
	AllowedTargets []string `json:"allowed_targets"`
	CreatedBy      string   `json:"created_by"`
	UpdatedBy      string   `json:"updated_by"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

func servicePrincipalResponse(principal state.ServicePrincipal) servicePrincipalView {
	return servicePrincipalView{
		ID:             principal.ID,
		WorkspaceID:    principal.WorkspaceID,
		Name:           principal.Name,
		HasToken:       principal.TokenHash != "",
		Scopes:         append([]string(nil), principal.Scopes...),
		AllowedTargets: append([]string(nil), principal.AllowedTargets...),
		CreatedBy:      principal.CreatedBy,
		UpdatedBy:      principal.UpdatedBy,
		CreatedAt:      principal.CreatedAt.UTC().Format(timeLayout),
		UpdatedAt:      principal.UpdatedAt.UTC().Format(timeLayout),
	}
}

func (h *Handler) handleCanonicalServicePrincipals(w http.ResponseWriter, r *http.Request, workspaceID string) {
	principals, err := h.store.ListServicePrincipals(r.Context(), workspaceID)
	if err != nil {
		writeStateError(w, err)
		return
	}
	views := make([]servicePrincipalView, 0, len(principals))
	for _, principal := range principals {
		views = append(views, servicePrincipalResponse(principal))
	}
	writeJSON(w, http.StatusOK, views)
}

func (h *Handler) handleCanonicalServicePrincipal(w http.ResponseWriter, r *http.Request, workspaceID string, id string) {
	principal, err := h.store.GetServicePrincipal(r.Context(), workspaceID, strings.TrimSpace(id))
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, servicePrincipalResponse(principal))
}

func (h *Handler) handleCanonicalCreateServicePrincipal(w http.ResponseWriter, r *http.Request, workspaceID string) {
	var request canonicalServicePrincipalRequest
	if err := readRequiredJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if request.Name == nil || request.Scopes == nil {
		writeError(w, http.StatusBadRequest, "name and scopes are required")
		return
	}
	name, ok := normalizeClientName(w, *request.Name)
	if !ok {
		return
	}
	scopes, ok := normalizeServicePrincipalScopes(w, *request.Scopes)
	if !ok {
		return
	}
	targets, ok := normalizeServicePrincipalTargets(w, request.AllowedTargets)
	if !ok {
		return
	}
	value, err := newServicePrincipalToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate service principal token")
		return
	}
	principal, err := h.store.CreateServicePrincipal(r.Context(), state.ServicePrincipal{
		WorkspaceID:    workspaceID,
		Name:           name,
		Scopes:         scopes,
		AllowedTargets: targets,
	}, state.HashBearerToken(value), clientActor(r))
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"service_principal": servicePrincipalResponse(principal),
		"api_token":         value,
	})
}

func (h *Handler) handleCanonicalUpdateServicePrincipal(w http.ResponseWriter, r *http.Request, workspaceID string, id string) {
	principal, err := h.store.GetServicePrincipal(r.Context(), workspaceID, strings.TrimSpace(id))
	if err != nil {
		writeStateError(w, err)
		return
	}
	var request canonicalServicePrincipalRequest
	if err := readRequiredJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if request.Name == nil && request.Scopes == nil && request.AllowedTargets == nil {
		writeError(w, http.StatusBadRequest, "at least one field is required")
		return
	}
	if request.Name != nil {
		principal.Name, _ = normalizeClientName(w, *request.Name)
		if principal.Name == "" {
			return
		}
	}
	if request.Scopes != nil {
		principal.Scopes, _ = normalizeServicePrincipalScopes(w, *request.Scopes)
		if principal.Scopes == nil {
			return
		}
	}
	if request.AllowedTargets != nil {
		principal.AllowedTargets, _ = normalizeServicePrincipalTargets(w, request.AllowedTargets)
		if principal.AllowedTargets == nil && len(*request.AllowedTargets) > 0 {
			return
		}
	}
	principal, err = h.store.UpdateServicePrincipal(r.Context(), principal, clientActor(r))
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, servicePrincipalResponse(principal))
}

func (h *Handler) handleCanonicalRotateServicePrincipalToken(w http.ResponseWriter, r *http.Request, workspaceID string, id string) {
	value, err := newServicePrincipalToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate service principal token")
		return
	}
	principal, err := h.store.RotateServicePrincipalToken(
		r.Context(), workspaceID, strings.TrimSpace(id), state.HashBearerToken(value), clientActor(r),
	)
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service_principal": servicePrincipalResponse(principal),
		"api_token":         value,
	})
}

func (h *Handler) handleCanonicalRevokeServicePrincipalToken(w http.ResponseWriter, r *http.Request, workspaceID string, id string) {
	principal, err := h.store.RevokeServicePrincipalToken(r.Context(), workspaceID, strings.TrimSpace(id), clientActor(r))
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, servicePrincipalResponse(principal))
}

func (h *Handler) handleCanonicalDeleteServicePrincipal(w http.ResponseWriter, r *http.Request, workspaceID string, id string) {
	if err := h.store.DeleteServicePrincipal(r.Context(), workspaceID, strings.TrimSpace(id), clientActor(r)); err != nil {
		writeStateError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleCanonicalServicePrincipalAudit(w http.ResponseWriter, r *http.Request, workspaceID string, id string) {
	records, err := h.store.ListServicePrincipalAudit(r.Context(), workspaceID, strings.TrimSpace(id))
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, records)
}

func normalizeServicePrincipalScopes(w http.ResponseWriter, values []string) ([]string, bool) {
	seen := map[string]struct{}{}
	scopes := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		scopes = append(scopes, value)
	}
	if !executionpkg.ValidScopeSet(scopes) {
		writeError(w, http.StatusBadRequest, "invalid service principal scope")
		return nil, false
	}
	sort.Strings(scopes)
	return scopes, true
}

func normalizeServicePrincipalTargets(w http.ResponseWriter, values *[]string) ([]string, bool) {
	if values == nil {
		return []string{}, true
	}
	seen := map[string]struct{}{}
	targets := make([]string, 0, len(*values))
	for _, value := range *values {
		value = strings.TrimSpace(value)
		parts := strings.Split(value, "/")
		valid := len(parts) == 1 && contract.ValidAppKey(parts[0]) ||
			len(parts) == 2 && contract.ValidAppKey(parts[0]) && contract.ValidActionKey(parts[1])
		if !valid {
			writeError(w, http.StatusBadRequest, "allowed_targets must contain app or app/action keys")
			return nil, false
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		targets = append(targets, value)
	}
	sort.Strings(targets)
	return targets, true
}

func newServicePrincipalToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return contract.ServiceTokenPrefix + base64.RawURLEncoding.EncodeToString(data), nil
}
