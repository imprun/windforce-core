package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/secretbackend"
	"github.com/imprun/windforce-core/internal/secretmask"
	"github.com/imprun/windforce-core/internal/state"
)

const secretMaskRegistrationHeader = "X-Windforce-Secret-Mask-Registered"

func (h *Handler) handleGetState(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	statePath := r.URL.Query().Get("path")
	if statePath == "" {
		writeError(w, http.StatusBadRequest, "path query required")
		return
	}
	value, _, err := h.store.GetState(r.Context(), workspaceID, statePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(rawOrNull(value))
}

func (h *Handler) handleSetState(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	statePath := r.URL.Query().Get("path")
	if statePath == "" {
		writeError(w, http.StatusBadRequest, "path query required")
		return
	}
	defer r.Body.Close()
	body, _ := io.ReadAll(r.Body)
	if err := h.store.SetState(r.Context(), workspaceID, statePath, body); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": statePath})
}

func (h *Handler) handleListVariables(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	variables, err := h.store.ListVariables(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range variables {
		if variables[i].IsSecret {
			variables[i].Value = ""
		}
	}
	writeJSON(w, http.StatusOK, variables)
}

func (h *Handler) handleSetVariable(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	var request struct {
		Path        string `json:"path"`
		Value       string `json:"value"`
		Description string `json:"description"`
		IsSecret    bool   `json:"is_secret"`
		AppKey      string `json:"app_key"`
		Scope       string `json:"scope"`
	}
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "path required")
		return
	}
	if err := json.Unmarshal(body, &request); err != nil || request.Path == "" {
		writeError(w, http.StatusBadRequest, "path required")
		return
	}
	if request.AppKey != "" && !validAppKey(request.AppKey) {
		writeError(w, http.StatusBadRequest, "invalid app key")
		return
	}
	normalizedPath, err := contract.NormalizeRuntimeConfigPath(request.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	request.Path = normalizedPath
	responsePath := request.Path
	if strings.TrimSpace(request.Scope) == string(contract.RuntimeConfigScopeActor) {
		if request.AppKey == "" {
			writeError(w, http.StatusBadRequest, "actor scope requires an app key")
			return
		}
		subject := requestActorSubject(r)
		if subject == "" {
			writeError(w, http.StatusUnauthorized, "actor scope requires an authenticated subject")
			return
		}
		request.Path, err = contract.ActorRuntimeConfigPath(subject, request.Path)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else if strings.TrimSpace(request.Scope) != "" && strings.TrimSpace(request.Scope) != string(contract.RuntimeConfigScopeApp) && strings.TrimSpace(request.Scope) != string(contract.RuntimeConfigScopeWorkspace) {
		writeError(w, http.StatusBadRequest, "scope must be workspace, app, or actor")
		return
	}
	value := request.Value
	if request.IsSecret {
		reference := secretbackend.Reference{
			WorkspaceID: workspaceID,
			Kind:        "variable",
			Path:        request.Path,
		}
		if request.AppKey != "" {
			reference.Kind = "variable-app"
			reference.Path = request.AppKey + "/" + request.Path
		}
		encrypted, err := h.secretBackend.Store(r.Context(), reference, request.Value)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		value = encrypted
	}
	if err := h.store.SetVariable(r.Context(), workspaceID, request.AppKey, request.Path, value, request.IsSecret, request.Description); err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": responsePath, "app_key": request.AppKey})
}

func (h *Handler) handleGetVariable(w http.ResponseWriter, r *http.Request, workspaceID string, variablePath string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	job, runtime, scopeErr := h.jobRuntimeScope(r, workspaceID)
	if scopeErr != nil {
		writeStateError(w, scopeErr)
		return
	}
	if runtime {
		scope, err := runtimeConfigScopeQuery(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		value, secret, err := h.runtimeResolver.ResolveJobVariableScoped(r.Context(), job, scope, variablePath)
		if err != nil {
			writeStateError(w, err)
			return
		}
		appendSecretMaskDigests(w.Header(), []string{value}, secret)
		writeJSON(w, http.StatusOK, map[string]any{"path": variablePath, "value": value, "is_secret": secret})
		return
	}

	variable, found, err := h.store.GetVariableExact(r.Context(), workspaceID, r.URL.Query().Get("app"), variablePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "variable not found")
		return
	}
	if variable.IsSecret {
		writeJSON(w, http.StatusOK, map[string]any{
			"path":       variable.Path,
			"is_secret":  true,
			"configured": variable.Value != "",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": variable.Path, "value": variable.Value, "is_secret": false})
}

func (h *Handler) encryptSecretVariable(ctx context.Context, workspaceID string, value string) (string, error) {
	return h.secretBackend.Store(ctx, secretbackend.Reference{
		WorkspaceID: workspaceID,
		Kind:        "variable",
	}, value)
}

func (h *Handler) decryptSecretVariable(ctx context.Context, workspaceID string, path string, value string) (string, error) {
	return h.secretBackend.Resolve(ctx, secretbackend.Reference{
		WorkspaceID: workspaceID,
		Kind:        "variable",
		Path:        path,
	}, value)
}

func (h *Handler) jobRuntimeScope(r *http.Request, workspaceID string) (state.Job, bool, error) {
	principal := jobPrincipalFrom(r.Context())
	if principal == nil || principal.JobID == "" {
		return state.Job{}, false, nil
	}
	job, _, found, err := h.store.GetJob(r.Context(), workspaceID, principal.JobID)
	if err != nil {
		return state.Job{}, true, err
	}
	if !found {
		return state.Job{}, true, state.ErrNotFound
	}
	if principal.Attempt <= 0 || job.Attempt != principal.Attempt {
		return state.Job{}, true, state.ErrInvalidState
	}
	if job.State != state.JobRunning || job.LeaseOwner == "" ||
		job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.After(time.Now()) {
		return state.Job{}, true, state.ErrInvalidState
	}
	return job, true, nil
}

func (h *Handler) handleDeleteVariable(w http.ResponseWriter, r *http.Request, workspaceID string, variablePath string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	if err := h.store.DeleteVariable(r.Context(), workspaceID, r.URL.Query().Get("app"), variablePath); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleRuntimeSetVariable(w http.ResponseWriter, r *http.Request, workspaceID string, variablePath string) {
	job, runtime, err := h.jobRuntimeScope(r, workspaceID)
	if err != nil {
		writeStateError(w, err)
		return
	}
	if !runtime {
		writeError(w, http.StatusForbidden, "runtime Variable write requires a live Job attempt")
		return
	}
	var request struct {
		Value            string `json:"value"`
		OperationID      string `json:"operationId"`
		ExpectedRevision *int64 `json:"expectedRevision,omitempty"`
	}
	body, err := readJSONBody(r)
	if err != nil || json.Unmarshal(body, &request) != nil {
		writeError(w, http.StatusBadRequest, "valid runtime Variable write body required")
		return
	}
	if len(request.Value) > state.RuntimeConfigMaxValueBytes {
		writeRuntimeConfigError(w, state.RuntimeConfigCodeLimitExceeded, http.StatusRequestEntityTooLarge, "Variable value exceeds runtime limit", 0)
		return
	}
	scope, err := runtimeMutationScopeQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	storage, allowed := pinnedVariableWriteStorage(job.Payload.RuntimeAccess, scope, variablePath)
	if !allowed {
		writeRuntimeConfigError(w, state.RuntimeConfigCodeForbidden, http.StatusForbidden, "Variable write target is not pinned", 0)
		return
	}
	isSecret := storage == contract.RuntimeVariableStorageSecret
	if isSecret && !validSecretMaskRegistration(r, body) {
		writeRuntimeConfigError(w, state.RuntimeConfigCodeForbidden, http.StatusForbidden, "Secret write requires Worker mask registration", 0)
		return
	}
	logicalPath := variablePath
	mutationAccess := job.Payload.RuntimeAccess
	if scope == contract.RuntimeConfigScopeActor {
		variablePath, err = contract.ActorRuntimeConfigPath(job.Payload.PermissionedAs, logicalPath)
		if err != nil {
			writeRuntimeConfigError(w, state.RuntimeConfigCodeForbidden, http.StatusForbidden, "Actor-scoped Variable write requires an authenticated subject", 0)
			return
		}
		mutationAccess, err = materializeActorRuntimeAccess(mutationAccess, job.Payload.PermissionedAs)
		if err != nil {
			writeRuntimeConfigError(w, state.RuntimeConfigCodeForbidden, http.StatusForbidden, "Actor-scoped Variable write could not be authorized", 0)
			return
		}
	}
	fingerprint := runtimeMutationFingerprint(struct {
		Kind             string `json:"kind"`
		Path             string `json:"path"`
		Value            string `json:"value"`
		ExpectedRevision *int64 `json:"expectedRevision,omitempty"`
	}{"variable", variablePath, request.Value, request.ExpectedRevision})
	storedValue := request.Value
	if isSecret {
		candidateID := runtimeSecretCandidateID(job, request.OperationID, fingerprint)
		storedValue, err = h.secretBackend.Store(r.Context(), secretbackend.Reference{
			WorkspaceID: workspaceID,
			Kind:        "variable-app",
			// A failed CAS/idempotency conflict must never overwrite the value
			// referenced by the current row. Content-address each candidate and
			// publish only its returned reference in the atomic state mutation.
			Path: job.Payload.App + "/" + variablePath + "/" + candidateID,
		}, request.Value)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		storedValue, err = secretbackend.SealRuntimeCandidate(candidateID, storedValue)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	job.Payload.RuntimeAccess = mutationAccess
	result, err := h.store.MutateRuntimeVariable(r.Context(), state.RuntimeVariableMutationRequest{
		WorkspaceID: workspaceID, AppKey: job.Payload.App, Path: variablePath,
		Value: storedValue, IsSecret: isSecret, PlaintextBytes: len(request.Value), OperationID: request.OperationID,
		RequestFingerprint: fingerprint, ExpectedRevision: request.ExpectedRevision,
		JobID: job.ID, Attempt: job.Attempt,
	})
	if err != nil {
		writeRuntimeConfigStateError(w, err)
		return
	}
	result.Path = logicalPath
	writeJSON(w, http.StatusOK, result)
}

func runtimeSecretCandidateID(job state.Job, operationID string, fingerprint string) string {
	value := sha256.Sum256([]byte(strings.Join([]string{
		job.ID,
		fmt.Sprintf("%d", job.Attempt),
		strings.TrimSpace(operationID),
		fingerprint,
	}, "\x00")))
	return hex.EncodeToString(value[:16])
}

func validSecretMaskRegistration(r *http.Request, body []byte) bool {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}
	provided, err := hex.DecodeString(strings.TrimSpace(r.Header.Get(secretMaskRegistrationHeader)))
	if err != nil {
		return false
	}
	digest := sha256.Sum256(body)
	mac := hmac.New(sha256.New, []byte(parts[1]))
	_, _ = mac.Write(digest[:])
	return hmac.Equal(provided, mac.Sum(nil))
}

func (h *Handler) handleSetResource(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	var request struct {
		Path         string          `json:"path"`
		Value        json.RawMessage `json:"value"`
		ResourceType string          `json:"resource_type"`
		Description  string          `json:"description"`
		AppKey       string          `json:"app_key"`
	}
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "path required")
		return
	}
	if err := json.Unmarshal(body, &request); err != nil || request.Path == "" {
		writeError(w, http.StatusBadRequest, "path required")
		return
	}
	if request.AppKey != "" && !validAppKey(request.AppKey) {
		writeError(w, http.StatusBadRequest, "invalid app key")
		return
	}
	if err := h.store.SetResourceScoped(r.Context(), workspaceID, request.AppKey, request.Path, request.Value, request.ResourceType, request.Description); err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": request.Path, "app_key": request.AppKey})
}

func (h *Handler) handleListResources(w http.ResponseWriter, r *http.Request, workspaceID string) {
	resources, err := h.store.ListResources(r.Context(), workspaceID)
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resources)
}

func (h *Handler) handleGetResource(w http.ResponseWriter, r *http.Request, workspaceID string, resourcePath string) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}
	job, runtime, scopeErr := h.jobRuntimeScope(r, workspaceID)
	if scopeErr != nil {
		writeStateError(w, scopeErr)
		return
	}
	var value json.RawMessage
	if runtime {
		scope, err := runtimeConfigScopeQuery(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		resolved, err := h.runtimeResolver.ResolveJobResourceScoped(r.Context(), job, scope, resourcePath)
		if err != nil {
			writeStateError(w, err)
			return
		}
		appendSecretMaskDigests(w.Header(), resolved.SecretValues, true)
		value = resolved.Value
	} else {
		scope, err := runtimeConfigScopeQuery(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		resource, found, err := h.store.GetResourceScoped(r.Context(), workspaceID, scope, r.URL.Query().Get("app"), resourcePath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !found {
			writeError(w, http.StatusNotFound, "resource not found")
			return
		}
		value = resource.Value
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(rawOrNull(value))
}

func appendSecretMaskDigests(header http.Header, values []string, enabled bool) {
	if !enabled {
		return
	}
	seen := map[string]struct{}{}
	for _, value := range values {
		if value == "" {
			continue
		}
		digest := secretmask.Digest(value)
		if _, exists := seen[digest]; exists {
			continue
		}
		seen[digest] = struct{}{}
		header.Add(secretmask.ResponseDigestHeader, digest)
	}
}

func runtimeConfigScopeQuery(r *http.Request) (contract.RuntimeConfigScope, error) {
	value := strings.TrimSpace(r.URL.Query().Get("scope"))
	if value == "" || value == string(contract.RuntimeConfigScopeWorkspace) {
		return contract.RuntimeConfigScopeWorkspace, nil
	}
	if value == string(contract.RuntimeConfigScopeApp) {
		return contract.RuntimeConfigScopeApp, nil
	}
	if value == string(contract.RuntimeConfigScopeActor) {
		return contract.RuntimeConfigScopeActor, nil
	}
	return "", fmt.Errorf("scope must be workspace, app, or actor")
}

func runtimeMutationScopeQuery(r *http.Request) (contract.RuntimeConfigScope, error) {
	if strings.TrimSpace(r.URL.Query().Get("scope")) == "" {
		return contract.RuntimeConfigScopeApp, nil
	}
	return runtimeConfigScopeQuery(r)
}

func (h *Handler) handleDeleteResource(w http.ResponseWriter, r *http.Request, workspaceID string, resourcePath string) {
	if err := h.store.DeleteResourceScoped(r.Context(), workspaceID, r.URL.Query().Get("app"), resourcePath); err != nil {
		writeStateError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleRuntimeSetResource(w http.ResponseWriter, r *http.Request, workspaceID string, resourcePath string) {
	job, runtime, err := h.jobRuntimeScope(r, workspaceID)
	if err != nil {
		writeStateError(w, err)
		return
	}
	if !runtime {
		writeError(w, http.StatusForbidden, "runtime Resource write requires a live Job attempt")
		return
	}
	var request struct {
		Value            json.RawMessage `json:"value"`
		ResourceType     string          `json:"resourceType"`
		Description      string          `json:"description,omitempty"`
		OperationID      string          `json:"operationId"`
		ExpectedRevision *int64          `json:"expectedRevision,omitempty"`
	}
	body, err := readJSONBody(r)
	if err != nil || json.Unmarshal(body, &request) != nil {
		writeError(w, http.StatusBadRequest, "valid runtime Resource write body required")
		return
	}
	scope, err := runtimeMutationScopeQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	logicalPath := resourcePath
	mutationAccess := job.Payload.RuntimeAccess
	if scope == contract.RuntimeConfigScopeActor {
		allowed := false
		for _, target := range mutationAccess.WriteResources {
			if target.Scope == scope && target.Path == logicalPath {
				allowed = true
				break
			}
		}
		if !allowed {
			writeRuntimeConfigError(w, state.RuntimeConfigCodeForbidden, http.StatusForbidden, "Resource write target is not pinned", 0)
			return
		}
		resourcePath, err = contract.ActorRuntimeConfigPath(job.Payload.PermissionedAs, logicalPath)
		if err != nil {
			writeRuntimeConfigError(w, state.RuntimeConfigCodeForbidden, http.StatusForbidden, "Actor-scoped Resource write requires an authenticated subject", 0)
			return
		}
		mutationAccess, err = materializeActorRuntimeAccess(mutationAccess, job.Payload.PermissionedAs)
		if err != nil {
			writeRuntimeConfigError(w, state.RuntimeConfigCodeForbidden, http.StatusForbidden, "Actor-scoped Resource write could not be authorized", 0)
			return
		}
	}
	fingerprint := runtimeResourceMutationFingerprint(
		resourcePath, request.Value, request.ResourceType, request.Description, request.ExpectedRevision,
	)
	job.Payload.RuntimeAccess = mutationAccess
	result, err := h.store.MutateRuntimeResource(r.Context(), state.RuntimeResourceMutationRequest{
		WorkspaceID: workspaceID, AppKey: job.Payload.App, Path: resourcePath,
		Value: request.Value, ResourceType: request.ResourceType, Description: request.Description,
		OperationID: request.OperationID, RequestFingerprint: fingerprint,
		ExpectedRevision: request.ExpectedRevision, JobID: job.ID, Attempt: job.Attempt,
	})
	if err != nil {
		writeRuntimeConfigStateError(w, err)
		return
	}
	result.Path = logicalPath
	writeJSON(w, http.StatusOK, result)
}

func runtimeResourceMutationFingerprint(path string, value json.RawMessage, resourceType, description string, expectedRevision *int64) string {
	return runtimeMutationFingerprint(struct {
		Kind             string          `json:"kind"`
		Path             string          `json:"path"`
		Value            json.RawMessage `json:"value"`
		ResourceType     string          `json:"resourceType"`
		Description      string          `json:"description,omitempty"`
		ExpectedRevision *int64          `json:"expectedRevision,omitempty"`
	}{"resource", path, value, resourceType, description, expectedRevision})
}

func pinnedVariableWriteStorage(access contract.RuntimeAccess, scope contract.RuntimeConfigScope, path string) (contract.RuntimeVariableStorage, bool) {
	path, err := contract.NormalizeRuntimeConfigPath(path)
	if err != nil {
		return "", false
	}
	for _, target := range access.WriteVariables {
		if target.Scope == scope && target.Path == path {
			return target.Storage, true
		}
	}
	return "", false
}

func materializeActorRuntimeAccess(access contract.RuntimeAccess, subject string) (contract.RuntimeAccess, error) {
	result := contract.CloneRuntimeAccess(access)
	materializeTarget := func(target contract.RuntimeConfigTarget) (contract.RuntimeConfigTarget, error) {
		if target.Scope != contract.RuntimeConfigScopeActor {
			return target, nil
		}
		path, err := contract.ActorRuntimeConfigPath(subject, target.Path)
		if err != nil {
			return contract.RuntimeConfigTarget{}, err
		}
		return contract.RuntimeConfigTarget{Scope: contract.RuntimeConfigScopeApp, Path: path}, nil
	}
	for index, target := range result.VariableTargets {
		mapped, err := materializeTarget(target)
		if err != nil {
			return contract.RuntimeAccess{}, err
		}
		result.VariableTargets[index] = mapped
	}
	for index, target := range result.ResourceTargets {
		mapped, err := materializeTarget(target)
		if err != nil {
			return contract.RuntimeAccess{}, err
		}
		result.ResourceTargets[index] = mapped
	}
	for index, target := range result.WriteVariables {
		mapped, err := materializeTarget(target.RuntimeConfigTarget)
		if err != nil {
			return contract.RuntimeAccess{}, err
		}
		result.WriteVariables[index].RuntimeConfigTarget = mapped
	}
	for index, target := range result.WriteResources {
		mapped, err := materializeTarget(target)
		if err != nil {
			return contract.RuntimeAccess{}, err
		}
		result.WriteResources[index] = mapped
	}
	return contract.NormalizeRuntimeAccess(result)
}

func runtimeMutationFingerprint(value any) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("sha256:%x", digest[:])
}

func writeRuntimeConfigStateError(w http.ResponseWriter, err error) {
	var typed *state.RuntimeConfigError
	if !errors.As(err, &typed) {
		writeStateError(w, err)
		return
	}
	status := http.StatusConflict
	if typed.Code == state.RuntimeConfigCodeForbidden || typed.Code == state.RuntimeConfigCodeStorageClassMismatch {
		status = http.StatusForbidden
	} else if typed.Code == state.RuntimeConfigCodeLimitExceeded {
		status = http.StatusRequestEntityTooLarge
	}
	writeRuntimeConfigError(w, typed.Code, status, typed.Error(), typed.CurrentRevision)
}

func writeRuntimeConfigError(w http.ResponseWriter, code string, status int, message string, currentRevision int64) {
	payload := map[string]any{"code": code, "message": message}
	if currentRevision > 0 {
		payload["currentRevision"] = currentRevision
	}
	writeJSON(w, status, map[string]any{"error": payload})
}
