package server

import (
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxClientNameRunes  = 200
	maxExternalKeyRunes = 512
)

type canonicalClientRequest struct {
	Name        *string `json:"name"`
	ExternalKey *string `json:"external_key"`
}

func (h *Handler) handleCanonicalClients(w http.ResponseWriter, r *http.Request, workspaceID string) {
	clients, err := h.store.ListClients(r.Context(), workspaceID)
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, clients)
}

func (h *Handler) handleCanonicalClient(w http.ResponseWriter, r *http.Request, workspaceID string, id string) {
	client, err := h.store.GetClient(r.Context(), workspaceID, strings.TrimSpace(id))
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, client)
}

func (h *Handler) handleCanonicalCreateClient(w http.ResponseWriter, r *http.Request, workspaceID string) {
	var request canonicalClientRequest
	if err := readRequiredJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if request.Name == nil || request.ExternalKey == nil {
		writeError(w, http.StatusBadRequest, "name and external_key are required")
		return
	}
	name, externalKey, ok := normalizeClientValues(w, *request.Name, *request.ExternalKey)
	if !ok {
		return
	}
	client, err := h.store.CreateClient(r.Context(), workspaceID, name, externalKey, clientActor(r))
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, client)
}

func (h *Handler) handleCanonicalUpdateClient(w http.ResponseWriter, r *http.Request, workspaceID string, id string) {
	id = strings.TrimSpace(id)
	current, err := h.store.GetClient(r.Context(), workspaceID, id)
	if err != nil {
		writeStateError(w, err)
		return
	}
	var request canonicalClientRequest
	if err := readRequiredJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if request.Name == nil && request.ExternalKey == nil {
		writeError(w, http.StatusBadRequest, "name or external_key is required")
		return
	}
	name := current.Name
	externalKey := current.ExternalKey
	if request.Name != nil {
		name = *request.Name
	}
	if request.ExternalKey != nil {
		externalKey = *request.ExternalKey
	}
	name, externalKey, ok := normalizeClientValues(w, name, externalKey)
	if !ok {
		return
	}
	client, err := h.store.UpdateClient(r.Context(), workspaceID, id, name, externalKey, clientActor(r))
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, client)
}

func (h *Handler) handleCanonicalDeleteClient(w http.ResponseWriter, r *http.Request, workspaceID string, id string) {
	err := h.store.DeleteClient(r.Context(), workspaceID, strings.TrimSpace(id), clientActor(r))
	if err != nil {
		writeStateError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleCanonicalClientAudit(w http.ResponseWriter, r *http.Request, workspaceID string, id string) {
	records, err := h.store.ListClientAudit(r.Context(), workspaceID, strings.TrimSpace(id))
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, records)
}

func normalizeClientValues(w http.ResponseWriter, name string, externalKey string) (string, string, bool) {
	name = strings.TrimSpace(name)
	externalKey = strings.TrimSpace(externalKey)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return "", "", false
	}
	if utf8.RuneCountInString(name) > maxClientNameRunes {
		writeError(w, http.StatusBadRequest, "name is too long")
		return "", "", false
	}
	if externalKey == "" {
		writeError(w, http.StatusBadRequest, "external_key is required")
		return "", "", false
	}
	if utf8.RuneCountInString(externalKey) > maxExternalKeyRunes {
		writeError(w, http.StatusBadRequest, "external_key is too long")
		return "", "", false
	}
	if strings.IndexFunc(externalKey, unicode.IsSpace) >= 0 || strings.IndexFunc(externalKey, unicode.IsControl) >= 0 {
		writeError(w, http.StatusBadRequest, "external_key must not contain whitespace or control characters")
		return "", "", false
	}
	return name, externalKey, true
}

func clientActor(r *http.Request) string {
	actor := requestActorSubject(r)
	if actor == "" {
		return "system"
	}
	return actor
}
