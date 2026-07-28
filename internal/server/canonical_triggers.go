package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	executionpkg "github.com/imprun/windforce-core/internal/execution"
	"github.com/imprun/windforce-core/internal/state"
	triggerpkg "github.com/imprun/windforce-core/internal/trigger"
)

type canonicalTriggerRequest struct {
	Name          string                         `json:"name"`
	Kind          string                         `json:"kind"`
	Enabled       *bool                          `json:"enabled,omitempty"`
	AppKey        string                         `json:"app"`
	ActionKey     string                         `json:"action"`
	CredentialRef string                         `json:"credential_ref,omitempty"`
	Config        json.RawMessage                `json:"config"`
	Completion    *state.TriggerCompletionPolicy `json:"completion"`
	Response      *state.TriggerResponsePolicy   `json:"response,omitempty"`
	SecretConfig  json.RawMessage                `json:"secret_config,omitempty"`
}

type canonicalTrigger struct {
	ID            string                        `json:"id"`
	WorkspaceID   string                        `json:"workspace_id"`
	Name          string                        `json:"name"`
	Kind          string                        `json:"kind"`
	Enabled       bool                          `json:"enabled"`
	AppKey        string                        `json:"app"`
	ActionKey     string                        `json:"action"`
	CredentialRef string                        `json:"credential_ref,omitempty"`
	Config        json.RawMessage               `json:"config"`
	Completion    state.TriggerCompletionPolicy `json:"completion"`
	Response      state.TriggerResponsePolicy   `json:"response"`
	HasSecret     bool                          `json:"has_secret"`
	CreatedBy     string                        `json:"created_by"`
	UpdatedBy     string                        `json:"updated_by"`
	CreatedAt     time.Time                     `json:"created_at"`
	UpdatedAt     time.Time                     `json:"updated_at"`
}

func (h *Handler) handleCanonicalTriggerAPI(w http.ResponseWriter, r *http.Request, parts []string) bool {
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "w" || parts[3] != "triggers" {
		return false
	}
	workspaceID := parts[2]
	switch {
	case len(parts) == 4 && r.Method == http.MethodGet:
		h.handleCanonicalTriggers(w, r, workspaceID)
	case len(parts) == 4 && r.Method == http.MethodPost:
		h.handleCanonicalCreateTrigger(w, r, workspaceID)
	case len(parts) == 5 && r.Method == http.MethodGet:
		h.handleCanonicalTrigger(w, r, workspaceID, parts[4])
	case len(parts) == 5 && r.Method == http.MethodPut:
		h.handleCanonicalUpdateTrigger(w, r, workspaceID, parts[4])
	case len(parts) == 5 && r.Method == http.MethodDelete:
		h.handleCanonicalDeleteTrigger(w, r, workspaceID, parts[4])
	case len(parts) == 6 && parts[5] == "enable" && r.Method == http.MethodPost:
		h.handleCanonicalSetTriggerEnabled(w, r, workspaceID, parts[4], true)
	case len(parts) == 6 && parts[5] == "disable" && r.Method == http.MethodPost:
		h.handleCanonicalSetTriggerEnabled(w, r, workspaceID, parts[4], false)
	case len(parts) == 6 && parts[5] == "audit" && r.Method == http.MethodGet:
		h.handleCanonicalTriggerAudit(w, r, workspaceID, parts[4])
	case len(parts) == 6 && parts[5] == "deliveries" && r.Method == http.MethodGet:
		h.handleCanonicalTriggerDeliveries(w, r, workspaceID, parts[4])
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
	return true
}

func (h *Handler) handleCanonicalTriggers(w http.ResponseWriter, r *http.Request, workspaceID string) {
	definitions, err := h.store.ListTriggers(r.Context(), workspaceID)
	if err != nil {
		writeStateError(w, err)
		return
	}
	items := make([]canonicalTrigger, 0, len(definitions))
	for _, definition := range definitions {
		items = append(items, canonicalTriggerFrom(definition))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) handleCanonicalTrigger(w http.ResponseWriter, r *http.Request, workspaceID string, id string) {
	definition, err := h.store.GetTrigger(r.Context(), workspaceID, id)
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, canonicalTriggerFrom(definition))
}

func (h *Handler) handleCanonicalCreateTrigger(w http.ResponseWriter, r *http.Request, workspaceID string) {
	request, ok := readCanonicalTriggerRequest(w, r)
	if !ok {
		return
	}
	enabled := false
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	definition := triggerDefinitionFromRequest(workspaceID, "", request, enabled)
	definition.SecretConfig = sanitizeTriggerCompletionSecrets(definition.SecretConfig, definition.Completion.Mode)
	if err := triggerpkg.ValidateDefinition(definition); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := h.store.CreateTrigger(r.Context(), definition, requestActorSubjectUTF8(r))
	if err != nil {
		writeStateError(w, err)
		return
	}
	h.reconcileTriggers()
	writeJSON(w, http.StatusCreated, canonicalTriggerFrom(created))
}

func (h *Handler) handleCanonicalUpdateTrigger(w http.ResponseWriter, r *http.Request, workspaceID string, id string) {
	existing, err := h.store.GetTrigger(r.Context(), workspaceID, id)
	if err != nil {
		writeStateError(w, err)
		return
	}
	request, ok := readCanonicalTriggerRequest(w, r)
	if !ok {
		return
	}
	enabled := existing.Enabled
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	definition := triggerDefinitionFromRequest(workspaceID, id, request, enabled)
	existingSecret := existing.SecretConfig
	if definition.Kind != existing.Kind {
		existingSecret = nil
	}
	mergedSecret, err := mergeTriggerSecretConfig(existingSecret, request.SecretConfig)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	definition.SecretConfig = sanitizeTriggerCompletionSecrets(mergedSecret, definition.Completion.Mode)
	if err := triggerpkg.ValidateDefinition(definition); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := h.store.UpdateTrigger(r.Context(), definition, requestActorSubjectUTF8(r))
	if err != nil {
		writeStateError(w, err)
		return
	}
	h.reconcileTriggers()
	writeJSON(w, http.StatusOK, canonicalTriggerFrom(updated))
}

func (h *Handler) handleCanonicalSetTriggerEnabled(w http.ResponseWriter, r *http.Request, workspaceID string, id string, enabled bool) {
	updated, err := h.store.SetTriggerEnabled(r.Context(), workspaceID, id, enabled, requestActorSubjectUTF8(r))
	if err != nil {
		writeStateError(w, err)
		return
	}
	h.reconcileTriggers()
	writeJSON(w, http.StatusOK, canonicalTriggerFrom(updated))
}

func (h *Handler) handleCanonicalDeleteTrigger(w http.ResponseWriter, r *http.Request, workspaceID string, id string) {
	if err := h.store.DeleteTrigger(r.Context(), workspaceID, id, requestActorSubjectUTF8(r)); err != nil {
		writeStateError(w, err)
		return
	}
	h.reconcileTriggers()
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleCanonicalTriggerAudit(w http.ResponseWriter, r *http.Request, workspaceID string, id string) {
	items, err := h.store.ListTriggerAudit(r.Context(), workspaceID, id)
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) handleCanonicalTriggerDeliveries(w http.ResponseWriter, r *http.Request, workspaceID string, id string) {
	items, err := h.store.ListTriggerDeliveries(r.Context(), workspaceID, id, 100)
	if err != nil {
		writeStateError(w, err)
		return
	}
	safe := make([]state.TriggerDelivery, 0, len(items))
	for _, item := range items {
		item.CompletionLeaseOwner = ""
		item.CompletionLeaseExpiresAt = nil
		safe = append(safe, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": safe})
}

func (h *Handler) handleTriggerIngress(w http.ResponseWriter, r *http.Request) bool {
	parts := splitPath(r.URL.Path)
	if len(parts) != 7 ||
		parts[0] != "api" ||
		parts[1] != "v1" ||
		parts[2] != "workspaces" ||
		parts[4] != "triggers" ||
		parts[6] != "events" {
		return false
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return true
	}
	if h.triggerManager == nil {
		writeError(w, http.StatusServiceUnavailable, "trigger manager is not configured")
		return true
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "could not read request body")
		}
		return true
	}
	headers := make(map[string][]string, len(r.Header))
	for name, values := range r.Header {
		headers[name] = append([]string(nil), values...)
	}
	submission := h.triggerManager.DeliverWebhook(r.Context(), parts[3], parts[5], triggerpkg.WebhookRequest{
		Headers: headers,
		Body:    body,
	})
	switch {
	case submission.State == triggerpkg.DeliveryAdmitted:
		statusURL := "/api/v1/workspaces/" + parts[3] + "/runs/" + submission.RunID
		resultURL := statusURL + "/result"
		w.Header().Set("Location", statusURL)
		w.Header().Set("X-WF-Run-Id", submission.RunID)
		definition, err := h.store.GetTrigger(r.Context(), parts[3], parts[5])
		if err == nil && definition.Response.Mode == state.TriggerResponseWait {
			principal := executionpkg.TriggerPrincipal(definition.WorkspaceID, definition.ID, definition.AppKey, definition.ActionKey)
			h.waitForInvocationRun(w, r, definition.WorkspaceID, submission.RunID, principal, time.Duration(definition.Response.TimeoutSeconds)*time.Second)
			return true
		}
		writeJSON(w, http.StatusAccepted, map[string]any{
			"run_id":     submission.RunID,
			"replayed":   submission.Replayed,
			"status_url": statusURL,
			"result_url": resultURL,
		})
	case errors.Is(submission.Err, triggerpkg.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, "invalid webhook signature")
	case errors.Is(submission.Err, triggerpkg.ErrNotFound):
		writeError(w, http.StatusNotFound, "webhook trigger not found")
	case submission.State == triggerpkg.DeliveryRetryable:
		writeError(w, http.StatusServiceUnavailable, "trigger admission unavailable")
	default:
		message := "trigger event rejected"
		if submission.Err != nil {
			message = submission.Err.Error()
		}
		writeError(w, http.StatusBadRequest, message)
	}
	return true
}

func readCanonicalTriggerRequest(w http.ResponseWriter, r *http.Request) (canonicalTriggerRequest, bool) {
	var request canonicalTriggerRequest
	if err := readRequiredJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return canonicalTriggerRequest{}, false
	}
	request.Config = bytes.TrimSpace(request.Config)
	request.SecretConfig = bytes.TrimSpace(request.SecretConfig)
	if len(request.Config) == 0 {
		request.Config = json.RawMessage("{}")
	}
	if request.Completion == nil {
		writeError(w, http.StatusBadRequest, "completion is required; choose none, poll, callback, or publish")
		return canonicalTriggerRequest{}, false
	}
	return request, true
}

func triggerDefinitionFromRequest(workspaceID string, id string, request canonicalTriggerRequest, enabled bool) state.TriggerDefinition {
	definition := state.TriggerDefinition{
		ID:            id,
		WorkspaceID:   workspaceID,
		Name:          strings.TrimSpace(request.Name),
		Kind:          strings.ToLower(strings.TrimSpace(request.Kind)),
		Enabled:       enabled,
		AppKey:        strings.TrimSpace(request.AppKey),
		ActionKey:     strings.TrimSpace(request.ActionKey),
		CredentialRef: strings.TrimSpace(request.CredentialRef),
		Config:        append(json.RawMessage(nil), request.Config...),
		SecretConfig:  append(json.RawMessage(nil), request.SecretConfig...),
	}
	if request.Completion != nil {
		definition.Completion = *request.Completion
	}
	if request.Response != nil {
		definition.Response = *request.Response
	}
	return definition
}

func canonicalTriggerFrom(definition state.TriggerDefinition) canonicalTrigger {
	return canonicalTrigger{
		ID:            definition.ID,
		WorkspaceID:   definition.WorkspaceID,
		Name:          definition.Name,
		Kind:          definition.Kind,
		Enabled:       definition.Enabled,
		AppKey:        definition.AppKey,
		ActionKey:     definition.ActionKey,
		CredentialRef: definition.CredentialRef,
		Config:        append(json.RawMessage(nil), definition.Config...),
		Completion:    definition.Completion,
		Response:      definition.Response,
		HasSecret:     hasTriggerSecret(definition.SecretConfig),
		CreatedBy:     definition.CreatedBy,
		UpdatedBy:     definition.UpdatedBy,
		CreatedAt:     definition.CreatedAt,
		UpdatedAt:     definition.UpdatedAt,
	}
}

func hasTriggerSecret(value json.RawMessage) bool {
	value = bytes.TrimSpace(value)
	return len(value) > 0 && !bytes.Equal(value, []byte("null")) && !bytes.Equal(value, []byte("{}"))
}

func mergeTriggerSecretConfig(existing json.RawMessage, patch json.RawMessage) (json.RawMessage, error) {
	var target map[string]any
	if len(bytes.TrimSpace(existing)) == 0 || bytes.Equal(bytes.TrimSpace(existing), []byte("null")) {
		target = map[string]any{}
	} else if err := json.Unmarshal(existing, &target); err != nil {
		return nil, errors.New("stored trigger secret config is invalid")
	}
	if len(bytes.TrimSpace(patch)) == 0 {
		if len(target) == 0 {
			return json.RawMessage("null"), nil
		}
		return json.Marshal(target)
	}
	var updates map[string]any
	if err := json.Unmarshal(patch, &updates); err != nil {
		return nil, errors.New("secret_config must be a JSON object")
	}
	mergeSecretObject(target, updates)
	if len(target) == 0 {
		return json.RawMessage("null"), nil
	}
	return json.Marshal(target)
}

func mergeSecretObject(target map[string]any, updates map[string]any) {
	for key, update := range updates {
		if update == nil {
			delete(target, key)
			continue
		}
		updateObject, updateIsObject := update.(map[string]any)
		currentObject, currentIsObject := target[key].(map[string]any)
		if updateIsObject {
			if !currentIsObject {
				currentObject = map[string]any{}
			}
			mergeSecretObject(currentObject, updateObject)
			if len(currentObject) == 0 {
				delete(target, key)
			} else {
				target[key] = currentObject
			}
			continue
		}
		target[key] = update
	}
}

func sanitizeTriggerCompletionSecrets(raw json.RawMessage, mode string) json.RawMessage {
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return raw
	}
	completionSecrets, ok := value["completion"].(map[string]any)
	if !ok {
		return raw
	}
	switch mode {
	case state.TriggerCompletionModeCallback:
		delete(completionSecrets, "rabbitmq_url")
	case state.TriggerCompletionModePublish:
		delete(completionSecrets, "signing_secret")
	default:
		delete(value, "completion")
	}
	if len(completionSecrets) == 0 {
		delete(value, "completion")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return encoded
}

func (h *Handler) reconcileTriggers() {
	if h.triggerManager != nil {
		_ = h.triggerManager.Reconcile(context.Background())
	}
}
