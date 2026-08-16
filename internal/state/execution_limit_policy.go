package state

import (
	"fmt"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/executionlimit"
)

// ExecutionLimitPolicyKey identifies the operator-controlled allowance for one
// release-owned limit shape. App-wide concurrency uses the reserved implicit
// policy ID even when the Release omits maxConcurrent.
type ExecutionLimitPolicyKey struct {
	WorkspaceID string `json:"workspaceId"`
	AppKey      string `json:"app"`
	ActionKey   string `json:"action,omitempty"`
	Scope       string `json:"scope"`
	PolicyID    string `json:"policyId"`
	Kind        string `json:"kind"`
}

type ExecutionLimitPolicy struct {
	ExecutionLimitPolicyKey
	ShapeFingerprint       string    `json:"shapeFingerprint"`
	Allowance              int32     `json:"allowance,omitempty"`
	WindowSeconds          int32     `json:"windowSeconds,omitempty"`
	Revision               int64     `json:"revision"`
	Deleted                bool      `json:"deleted,omitempty"`
	LastOperationID        string    `json:"lastOperationId,omitempty"`
	LastRequestFingerprint string    `json:"lastRequestFingerprint,omitempty"`
	UpdatedBy              string    `json:"updatedBy,omitempty"`
	UpdatedAt              time.Time `json:"updatedAt"`
}

type MutateExecutionLimitPolicyRequest struct {
	Policy             ExecutionLimitPolicy `json:"policy"`
	ExpectedRevision   int64                `json:"expectedRevision"`
	OperationID        string               `json:"operationId"`
	RequestFingerprint string               `json:"requestFingerprint"`
	Delete             bool                 `json:"delete,omitempty"`
	Actor              string               `json:"actor,omitempty"`
	Reason             string               `json:"reason,omitempty"`
}

type ExecutionLimitPolicyMutationResult struct {
	Policy   ExecutionLimitPolicy `json:"policy"`
	Replayed bool                 `json:"replayed,omitempty"`
}

type ExecutionLimitPolicyAudit struct {
	ID string `json:"id"`
	ExecutionLimitPolicyKey
	EventKind         string    `json:"eventKind"`
	ShapeFingerprint  string    `json:"shapeFingerprint"`
	PreviousAllowance *int32    `json:"previousAllowance,omitempty"`
	Allowance         *int32    `json:"allowance,omitempty"`
	Revision          int64     `json:"revision"`
	OperationID       string    `json:"operationId"`
	Reason            string    `json:"reason,omitempty"`
	Actor             string    `json:"actor,omitempty"`
	CreatedAt         time.Time `json:"createdAt"`
}

// ExecutionLimitResidual is one queued/running cohort that still carries an
// immutable shape pin. PinnedCeiling remains separate because capacity-only
// Release changes intentionally keep the same shape fingerprint.
type ExecutionLimitResidual struct {
	ExecutionLimitPolicyKey
	ShapeFingerprint string `json:"shapeFingerprint"`
	PinnedCeiling    *int32 `json:"pinnedCeiling"`
	WindowSeconds    int32  `json:"windowSeconds,omitempty"`
	Queued           int64  `json:"queued"`
	Running          int64  `json:"running"`
	MaxRunningForKey int64  `json:"-"`
}

func NormalizeExecutionLimitPolicyKey(key ExecutionLimitPolicyKey) (ExecutionLimitPolicyKey, error) {
	key.WorkspaceID = contract.NormalizeWorkspace(key.WorkspaceID)
	key.AppKey = strings.TrimSpace(key.AppKey)
	key.ActionKey = strings.TrimSpace(key.ActionKey)
	key.Scope = strings.TrimSpace(key.Scope)
	key.PolicyID = strings.TrimSpace(key.PolicyID)
	key.Kind = strings.TrimSpace(key.Kind)
	if key.AppKey == "" || CleanID(key.AppKey) != key.AppKey || key.PolicyID == "" || CleanID(key.PolicyID) != key.PolicyID {
		return ExecutionLimitPolicyKey{}, fmt.Errorf("%w: execution limit policy app and policy id are required", ErrInvalidState)
	}
	if key.Scope != executionlimit.ScopeApp && key.Scope != executionlimit.ScopeAction {
		return ExecutionLimitPolicyKey{}, fmt.Errorf("%w: execution limit policy scope %q", ErrInvalidState, key.Scope)
	}
	if key.Scope == executionlimit.ScopeAction {
		if key.ActionKey == "" || CleanID(key.ActionKey) != key.ActionKey {
			return ExecutionLimitPolicyKey{}, fmt.Errorf("%w: action-scoped execution limit policy requires an action", ErrInvalidState)
		}
	} else {
		key.ActionKey = ""
	}
	if key.Kind != executionlimit.KindConcurrency && key.Kind != executionlimit.KindRate {
		return ExecutionLimitPolicyKey{}, fmt.Errorf("%w: execution limit policy kind %q", ErrInvalidState, key.Kind)
	}
	return key, nil
}

func NormalizeExecutionLimitPolicyMutation(request MutateExecutionLimitPolicyRequest) (MutateExecutionLimitPolicyRequest, error) {
	key, err := NormalizeExecutionLimitPolicyKey(request.Policy.ExecutionLimitPolicyKey)
	if err != nil {
		return MutateExecutionLimitPolicyRequest{}, err
	}
	request.Policy.ExecutionLimitPolicyKey = key
	request.Policy.ShapeFingerprint = strings.TrimSpace(request.Policy.ShapeFingerprint)
	request.OperationID = strings.TrimSpace(request.OperationID)
	request.RequestFingerprint = strings.TrimSpace(request.RequestFingerprint)
	request.Actor = strings.TrimSpace(request.Actor)
	request.Reason = strings.TrimSpace(request.Reason)
	if !executionlimit.IsFingerprint(request.Policy.ShapeFingerprint) || request.ExpectedRevision < 0 || request.OperationID == "" ||
		len(request.OperationID) > 128 || CleanID(request.OperationID) != request.OperationID || request.RequestFingerprint == "" {
		return MutateExecutionLimitPolicyRequest{}, ErrInvalidState
	}
	if len(request.RequestFingerprint) > 256 || len(request.Reason) > 1024 {
		return MutateExecutionLimitPolicyRequest{}, ErrInvalidState
	}
	if request.Delete {
		request.Policy.Allowance = 0
	} else if request.Policy.Allowance <= 0 {
		return MutateExecutionLimitPolicyRequest{}, fmt.Errorf("%w: execution limit allowance must be positive", ErrInvalidState)
	}
	if request.Policy.Kind == executionlimit.KindConcurrency && request.Policy.WindowSeconds != 0 {
		return MutateExecutionLimitPolicyRequest{}, ErrInvalidState
	}
	if request.Policy.Kind == executionlimit.KindRate && request.Policy.WindowSeconds <= 0 {
		return MutateExecutionLimitPolicyRequest{}, ErrInvalidState
	}
	return request, nil
}

func normalizeExecutionLimitPolicyBatch(requests []MutateExecutionLimitPolicyRequest) ([]MutateExecutionLimitPolicyRequest, error) {
	normalized := make([]MutateExecutionLimitPolicyRequest, len(requests))
	seen := make(map[string]struct{}, len(requests))
	for index, request := range requests {
		item, err := NormalizeExecutionLimitPolicyMutation(request)
		if err != nil {
			return nil, err
		}
		key := executionLimitPolicyStorageKey(item.Policy.ExecutionLimitPolicyKey)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("%w: execution limit policy batch repeats one identity", ErrInvalidState)
		}
		seen[key] = struct{}{}
		normalized[index] = item
	}
	return normalized, nil
}

func executionLimitPolicyStorageKey(key ExecutionLimitPolicyKey) string {
	return strings.Join([]string{key.WorkspaceID, key.AppKey, key.ActionKey, key.Scope, key.PolicyID, key.Kind}, "\x1f")
}

func executionLimitPolicyLockKey(key ExecutionLimitPolicyKey) string {
	return "execution-limit-policy:" + strings.Join([]string{key.AppKey, key.ActionKey, key.Scope, key.PolicyID, key.Kind}, "\x1f")
}

func executionLimitPolicyAuditKey(workspaceID string, appKey string) string {
	return contract.NormalizeWorkspace(workspaceID) + "\x1f" + strings.TrimSpace(appKey)
}

func cloneExecutionLimitPolicy(policy ExecutionLimitPolicy) ExecutionLimitPolicy {
	return policy
}

func int32Value(value int32) *int32 {
	copy := value
	return &copy
}
