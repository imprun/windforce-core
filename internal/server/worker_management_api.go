package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/state"
)

type workerCredentialResponse struct {
	ID              string     `json:"id"`
	Group           string     `json:"group"`
	Generation      int64      `json:"generation"`
	WorkspaceIDs    []string   `json:"workspace_ids"`
	Labels          []string   `json:"labels"`
	Status          string     `json:"status"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	DrainDeadlineAt *time.Time `json:"drain_deadline_at,omitempty"`
	CreatedBy       string     `json:"created_by"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type workerGroupRunStateResponse struct {
	Group       string     `json:"group"`
	State       string     `json:"state"`
	OperationID string     `json:"operation_id,omitempty"`
	Revision    int64      `json:"revision"`
	DeadlineAt  *time.Time `json:"deadline_at,omitempty"`
	UpdatedBy   string     `json:"updated_by,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at,omitempty"`
}

func projectWorkerCredential(credential state.WorkerCredential) workerCredentialResponse {
	return workerCredentialResponse{
		ID: credential.ID, Group: credential.Group, Generation: credential.Generation,
		WorkspaceIDs: append([]string(nil), credential.WorkspaceIDs...), Labels: append([]string{}, credential.Labels...),
		Status: credential.Status, ExpiresAt: credential.ExpiresAt, RevokedAt: credential.RevokedAt,
		DrainDeadlineAt: credential.DrainDeadlineAt, CreatedBy: credential.CreatedBy,
		CreatedAt: credential.CreatedAt, UpdatedAt: credential.UpdatedAt,
	}
}

func projectWorkerGroupRunState(runState state.WorkerGroupRunState) workerGroupRunStateResponse {
	return workerGroupRunStateResponse{
		Group: runState.Group, State: runState.State, OperationID: runState.OperationID,
		Revision: runState.Revision, DeadlineAt: runState.DeadlineAt,
		UpdatedBy: runState.UpdatedBy, UpdatedAt: runState.UpdatedAt,
	}
}

func (h *Handler) handleWorkerManagementAPI(w http.ResponseWriter, r *http.Request, parts []string) bool {
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "worker-groups" {
		return false
	}
	principal := workspacePrincipalFrom(r.Context())
	if principal == nil || !principal.Admin {
		writeError(w, http.StatusForbidden, "instance admin authorization is required")
		return true
	}
	store, ok := h.workerControlStore()
	if !ok {
		writeError(w, http.StatusNotImplemented, "worker management is not supported by this store")
		return true
	}
	group, err := state.NormalizeWorkerGroup(parts[2])
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return true
	}
	switch {
	case len(parts) == 4 && parts[3] == "credentials" && r.Method == http.MethodGet:
		h.handleListWorkerCredentials(w, r, store, group)
		return true
	case len(parts) == 4 && parts[3] == "credentials" && r.Method == http.MethodPost:
		h.handleCreateWorkerCredential(w, r, store, group)
		return true
	case len(parts) == 6 && parts[3] == "credentials" && parts[5] == "revoke" && r.Method == http.MethodPost:
		h.handleRevokeWorkerCredential(w, r, store, group, parts[4])
		return true
	case len(parts) == 4 && parts[3] == "run-state" && r.Method == http.MethodGet:
		h.handleGetWorkerGroupRunState(w, r, store, group)
		return true
	case len(parts) == 4 && parts[3] == "run-state" && r.Method == http.MethodPut:
		h.handlePutWorkerGroupRunState(w, r, store, group)
		return true
	case len(parts) == 4 && parts[3] == "observation" && r.Method == http.MethodGet:
		h.handleGetWorkerGroupObservation(w, r, store, group)
		return true
	default:
		return false
	}
}

func (h *Handler) handleGetWorkerGroupObservation(
	w http.ResponseWriter,
	r *http.Request,
	store state.WorkerControlStore,
	group string,
) {
	observation, err := store.GetWorkerGroupObservation(r.Context(), group)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "worker group observation unavailable")
		return
	}
	writeJSON(w, http.StatusOK, observation)
}

func (h *Handler) handleListWorkerCredentials(w http.ResponseWriter, r *http.Request, store state.WorkerControlStore, group string) {
	credentials, err := store.ListWorkerCredentials(r.Context(), group)
	if err != nil {
		writeStateError(w, err)
		return
	}
	response := make([]workerCredentialResponse, 0, len(credentials))
	for _, credential := range credentials {
		response = append(response, projectWorkerCredential(credential))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleCreateWorkerCredential(w http.ResponseWriter, r *http.Request, store state.WorkerControlStore, group string) {
	var request struct {
		OperationID        string     `json:"operation_id"`
		ExpectedGeneration int64      `json:"expected_generation"`
		WorkspaceIDs       []string   `json:"workspace_ids"`
		Labels             []string   `json:"labels"`
		ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	}
	if err := readRequiredJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	group, err := state.NormalizeWorkerGroup(group)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !validOperationID(request.OperationID) || request.ExpectedGeneration < 0 {
		writeError(w, http.StatusBadRequest, "operation_id and a non-negative expected_generation are required")
		return
	}
	workspaces, labels, err := state.NormalizeWorkerCredentialScope(request.WorkspaceIDs, request.Labels)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.ExpiresAt != nil {
		expiresAt := request.ExpiresAt.UTC()
		request.ExpiresAt = &expiresAt
	}
	fingerprint := requestFingerprint(struct {
		Group              string     `json:"group"`
		OperationID        string     `json:"operation_id"`
		ExpectedGeneration int64      `json:"expected_generation"`
		WorkspaceIDs       []string   `json:"workspace_ids"`
		Labels             []string   `json:"labels"`
		ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	}{group, request.OperationID, request.ExpectedGeneration, workspaces, labels, request.ExpiresAt})
	workerToken, err := newWorkerToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate worker token")
		return
	}
	credential, replayed, err := store.CreateWorkerCredential(r.Context(), state.CreateWorkerCredentialRequest{
		Group: group, ExpectedGeneration: request.ExpectedGeneration, WorkspaceIDs: workspaces,
		Labels: labels, ExpiresAt: request.ExpiresAt, TokenHash: state.HashBearerToken(workerToken),
		OperationID: request.OperationID, RequestFingerprint: fingerprint, Actor: workerManagementActor(r),
	})
	if err != nil {
		writeStateError(w, err)
		return
	}
	response := map[string]any{"credential": projectWorkerCredential(credential), "replayed": replayed}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	} else {
		response["worker_token"] = workerToken
	}
	writeJSON(w, status, response)
}

func (h *Handler) handleRevokeWorkerCredential(w http.ResponseWriter, r *http.Request, store state.WorkerControlStore, group string, credentialID string) {
	var request struct {
		OperationID     string    `json:"operation_id"`
		DrainDeadlineAt time.Time `json:"drain_deadline_at"`
	}
	if err := readRequiredJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	group, err := state.NormalizeWorkerGroup(group)
	if err != nil || !validOperationID(request.OperationID) || request.DrainDeadlineAt.IsZero() {
		writeError(w, http.StatusBadRequest, "valid group, operation_id, and drain_deadline_at are required")
		return
	}
	deadline := request.DrainDeadlineAt.UTC()
	fingerprint := requestFingerprint(struct {
		Group           string    `json:"group"`
		CredentialID    string    `json:"credential_id"`
		OperationID     string    `json:"operation_id"`
		DrainDeadlineAt time.Time `json:"drain_deadline_at"`
	}{group, strings.TrimSpace(credentialID), request.OperationID, deadline})
	credential, replayed, err := store.RevokeWorkerCredential(r.Context(), state.RevokeWorkerCredentialRequest{
		Group: group, CredentialID: credentialID, OperationID: request.OperationID,
		RequestFingerprint: fingerprint, DrainDeadlineAt: deadline, Actor: workerManagementActor(r),
	})
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"credential": projectWorkerCredential(credential), "replayed": replayed})
}

func (h *Handler) handleGetWorkerGroupRunState(w http.ResponseWriter, r *http.Request, store state.WorkerControlStore, group string) {
	runState, err := store.GetWorkerGroupRunState(r.Context(), group)
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projectWorkerGroupRunState(runState))
}

func (h *Handler) handlePutWorkerGroupRunState(w http.ResponseWriter, r *http.Request, store state.WorkerControlStore, group string) {
	var request struct {
		OperationID      string     `json:"operation_id"`
		ExpectedRevision int64      `json:"expected_revision"`
		State            string     `json:"state"`
		DeadlineAt       *time.Time `json:"deadline_at,omitempty"`
	}
	if err := readRequiredJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	group, err := state.NormalizeWorkerGroup(group)
	if err != nil || !validOperationID(request.OperationID) || request.ExpectedRevision < 0 {
		writeError(w, http.StatusBadRequest, "valid group, operation_id, and a non-negative expected_revision are required")
		return
	}
	stateValue, err := state.NormalizeWorkerGroupRunState(request.State)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if stateValue == state.WorkerGroupDraining && request.DeadlineAt == nil {
		writeError(w, http.StatusBadRequest, "deadline_at is required while draining")
		return
	}
	if stateValue == state.WorkerGroupRunning {
		request.DeadlineAt = nil
	} else if request.DeadlineAt != nil {
		deadline := request.DeadlineAt.UTC()
		request.DeadlineAt = &deadline
	}
	fingerprint := requestFingerprint(struct {
		Group            string     `json:"group"`
		OperationID      string     `json:"operation_id"`
		ExpectedRevision int64      `json:"expected_revision"`
		State            string     `json:"state"`
		DeadlineAt       *time.Time `json:"deadline_at,omitempty"`
	}{group, request.OperationID, request.ExpectedRevision, stateValue, request.DeadlineAt})
	runState, replayed, err := store.PutWorkerGroupRunState(r.Context(), state.PutWorkerGroupRunStateRequest{
		Group: group, State: stateValue, OperationID: request.OperationID,
		ExpectedRevision: request.ExpectedRevision, DeadlineAt: request.DeadlineAt,
		RequestFingerprint: fingerprint, Actor: workerManagementActor(r),
	})
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run_state": projectWorkerGroupRunState(runState), "replayed": replayed})
}

func validOperationID(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 128 && state.CleanID(value) == value
}

func requestFingerprint(value any) string {
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func newWorkerToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return contract.RemoteWorkerTokenPrefix + base64.RawURLEncoding.EncodeToString(data), nil
}

func workerManagementActor(r *http.Request) string {
	actor := strings.TrimSpace(requestActorSubject(r))
	if actor == "" {
		return "instance-admin"
	}
	return actor
}
