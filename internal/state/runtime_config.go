package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

const (
	RuntimeConfigMaxValueBytes        = 1 << 20
	RuntimeConfigMaxStoredSecretBytes = 2 << 20
	RuntimeConfigMaxWritesPerAttempt  = 256
)

type AppRuntimeState string

const (
	AppRuntimeActive     AppRuntimeState = "active"
	AppRuntimeTombstoned AppRuntimeState = "tombstoned"
	AppRuntimeRevoked    AppRuntimeState = "revoked"
)

type AppRuntimeLifecycle struct {
	WorkspaceID string          `json:"workspaceId"`
	AppKey      string          `json:"appKey"`
	State       AppRuntimeState `json:"state"`
	Reason      string          `json:"reason,omitempty"`
	Actor       string          `json:"actor"`
	Revision    int64           `json:"revision"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

type AppRuntimeLifecycleAudit struct {
	WorkspaceID string          `json:"workspaceId"`
	AppKey      string          `json:"appKey"`
	State       AppRuntimeState `json:"state"`
	Reason      string          `json:"reason,omitempty"`
	Actor       string          `json:"actor"`
	Revision    int64           `json:"revision"`
	Purged      bool            `json:"purged,omitempty"`
	Forced      bool            `json:"forced,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
}

type SetAppRuntimeLifecycleRequest struct {
	WorkspaceID      string
	AppKey           string
	State            AppRuntimeState
	Reason           string
	Actor            string
	ExpectedRevision *int64
}

type PurgeAppRuntimeConfigRequest struct {
	WorkspaceID string
	AppKey      string
	Actor       string
	Reason      string
	Force       bool
}

type ProvisionedRuntimeVariable struct {
	AppKey      string
	Path        string
	Value       string
	IsSecret    bool
	Description string
	Revision    int64
}

type ProvisionedRuntimeResource struct {
	AppKey       string
	Path         string
	Value        json.RawMessage
	ResourceType string
	Description  string
	Revision     int64
}

type ProvisionedAppRuntimeLifecycle struct {
	AppKey   string
	State    AppRuntimeState
	Reason   string
	Revision int64
}

type RuntimeConfigProvisioningBatch struct {
	WorkspaceID string
	Actor       string
	DryRun      bool
	Variables   []ProvisionedRuntimeVariable
	Resources   []ProvisionedRuntimeResource
	Lifecycles  []ProvisionedAppRuntimeLifecycle
}

const (
	RuntimeConfigCodeForbidden            = "runtime_config_forbidden"
	RuntimeConfigCodeAttemptInvalid       = "runtime_config_attempt_invalid"
	RuntimeConfigCodeOperationConflict    = "runtime_config_operation_conflict"
	RuntimeConfigCodeRevisionConflict     = "runtime_config_revision_conflict"
	RuntimeConfigCodeLimitExceeded        = "runtime_config_limit_exceeded"
	RuntimeConfigCodeReferenceForbidden   = "runtime_config_reference_forbidden"
	RuntimeConfigCodeStorageClassMismatch = "runtime_config_storage_class_mismatch"
)

type RuntimeConfigError struct {
	Code            string
	CurrentRevision int64
	Err             error
}

func (e *RuntimeConfigError) Error() string {
	if e == nil || e.Err == nil {
		return "runtime configuration error"
	}
	return e.Err.Error()
}

func (e *RuntimeConfigError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type RuntimeVariableMutationRequest struct {
	WorkspaceID        string
	AppKey             string
	Path               string
	Value              string
	IsSecret           bool
	PlaintextBytes     int
	Description        string
	OperationID        string
	RequestFingerprint string
	ExpectedRevision   *int64
	JobID              string
	Attempt            int
	Actor              string
}

type RuntimeResourceMutationRequest struct {
	WorkspaceID        string
	AppKey             string
	Path               string
	Value              json.RawMessage
	ResourceType       string
	Description        string
	OperationID        string
	RequestFingerprint string
	ExpectedRevision   *int64
	JobID              string
	Attempt            int
	Actor              string
}

type RuntimeConfigMutationResult struct {
	Path     string `json:"path"`
	Revision int64  `json:"revision"`
	Replayed bool   `json:"replayed"`
}

type RuntimeConfigOperation struct {
	WorkspaceID        string    `json:"workspace_id"`
	JobID              string    `json:"job_id"`
	Attempt            int       `json:"attempt"`
	OperationID        string    `json:"operation_id"`
	RequestFingerprint string    `json:"request_fingerprint"`
	ObjectKind         string    `json:"object_kind"`
	AppKey             string    `json:"app_key"`
	Path               string    `json:"path"`
	Revision           int64     `json:"revision"`
	CreatedAt          time.Time `json:"created_at"`
}

type RuntimeConfigAudit struct {
	ID          string                      `json:"id"`
	WorkspaceID string                      `json:"workspace_id"`
	OwnerScope  contract.RuntimeConfigScope `json:"owner_scope"`
	AppKey      string                      `json:"app_key,omitempty"`
	Path        string                      `json:"path"`
	ObjectKind  string                      `json:"object_kind"`
	Storage     string                      `json:"storage,omitempty"`
	Revision    int64                       `json:"revision"`
	OperationID string                      `json:"operation_id,omitempty"`
	JobID       string                      `json:"job_id,omitempty"`
	Attempt     int                         `json:"attempt,omitempty"`
	Actor       string                      `json:"actor"`
	CreatedAt   time.Time                   `json:"created_at"`
}

func normalizeRuntimeMutation(workspaceID, appKey, path, operationID, fingerprint string, jobID string, attempt int) (string, string, string, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	appKey = strings.TrimSpace(appKey)
	if appKey == "" {
		return "", "", "", &RuntimeConfigError{Code: RuntimeConfigCodeForbidden, Err: fmt.Errorf("app key is required: %w", ErrForbidden)}
	}
	path, err := contract.NormalizeRuntimeConfigPath(path)
	if err != nil {
		return "", "", "", fmt.Errorf("%w: %v", ErrInvalidState, err)
	}
	operationID = strings.TrimSpace(operationID)
	if operationID == "" || len(operationID) > 200 {
		return "", "", "", fmt.Errorf("%w: operationId is required and must not exceed 200 bytes", ErrInvalidState)
	}
	if strings.TrimSpace(fingerprint) == "" {
		return "", "", "", fmt.Errorf("%w: request fingerprint is required", ErrInvalidState)
	}
	if strings.TrimSpace(jobID) == "" || attempt <= 0 {
		return "", "", "", &RuntimeConfigError{Code: RuntimeConfigCodeAttemptInvalid, Err: fmt.Errorf("live job attempt is required: %w", ErrInvalidState)}
	}
	return workspaceID, appKey, path, nil
}

func validateRuntimeVariableValue(request RuntimeVariableMutationRequest) error {
	if request.PlaintextBytes < 0 {
		return runtimeConfigError(RuntimeConfigCodeLimitExceeded, fmt.Errorf("variable plaintext size is invalid: %w", ErrInvalidState))
	}
	logicalBytes := len(request.Value)
	storedLimit := RuntimeConfigMaxValueBytes
	if request.IsSecret {
		storedLimit = RuntimeConfigMaxStoredSecretBytes
		if request.PlaintextBytes > 0 {
			logicalBytes = request.PlaintextBytes
		}
	}
	if logicalBytes > RuntimeConfigMaxValueBytes || len(request.Value) > storedLimit {
		return runtimeConfigError(RuntimeConfigCodeLimitExceeded, fmt.Errorf("variable value exceeds %d bytes: %w", RuntimeConfigMaxValueBytes, ErrInvalidState))
	}
	return nil
}

func runtimeConfigError(code string, err error) error {
	if err == nil {
		err = errors.New(code)
	}
	return &RuntimeConfigError{Code: code, Err: err}
}

func runtimeConfigRevisionError(current int64) error {
	return &RuntimeConfigError{
		Code:            RuntimeConfigCodeRevisionConflict,
		CurrentRevision: current,
		Err:             fmt.Errorf("runtime configuration revision conflict: %w", ErrConflict),
	}
}

func runtimeConfigOperationKey(workspaceID, jobID string, attempt int, operationID string) string {
	return workspaceID + "\x00" + jobID + "\x00" + fmt.Sprint(attempt) + "\x00" + operationID
}

func runtimeConfigObjectKey(scope contract.RuntimeConfigScope, appKey, path string) string {
	return string(scope) + "\x00" + strings.TrimSpace(appKey) + "\x00" + strings.TrimSpace(path)
}
