package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/executionlimit"
	"github.com/imprun/windforce-core/internal/state"
)

type canonicalExecutionLimitShape struct {
	WorkspaceID      string `json:"workspace_id"`
	AppKey           string `json:"app_key"`
	ActionKey        string `json:"action_key,omitempty"`
	Scope            string `json:"scope"`
	PolicyID         string `json:"policy_id"`
	Kind             string `json:"kind"`
	ShapeFingerprint string `json:"shape_fingerprint"`
	ReleaseCeiling   *int32 `json:"release_ceiling"`
	WindowSeconds    int32  `json:"window_seconds,omitempty"`
}

type canonicalExecutionLimitPolicy struct {
	WorkspaceID         string    `json:"workspace_id"`
	AppKey              string    `json:"app_key"`
	ActionKey           string    `json:"action_key,omitempty"`
	Scope               string    `json:"scope"`
	PolicyID            string    `json:"policy_id"`
	Kind                string    `json:"kind"`
	ShapeFingerprint    string    `json:"shape_fingerprint"`
	WindowSeconds       int32     `json:"window_seconds,omitempty"`
	OperatorAllowance   *int32    `json:"operator_allowance"`
	Revision            int64     `json:"revision"`
	OperationID         string    `json:"operation_id"`
	Status              string    `json:"status"`
	ObservedFingerprint string    `json:"observed_shape_fingerprint,omitempty"`
	UpdatedBy           string    `json:"updated_by,omitempty"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type canonicalEnforcedExecutionLimit struct {
	canonicalExecutionLimitShape
	OperatorAllowance  *int32 `json:"operator_allowance"`
	EffectiveLimit     *int32 `json:"effective_limit"`
	PolicyRevision     *int64 `json:"policy_revision,omitempty"`
	Status             string `json:"status"`
	OverAllowanceDrain bool   `json:"over_allowance_drain"`
}

type canonicalExecutionLimitResidual struct {
	canonicalExecutionLimitShape
	OperatorAllowance  *int32 `json:"operator_allowance"`
	EffectiveLimit     *int32 `json:"effective_limit"`
	PolicyRevision     *int64 `json:"policy_revision,omitempty"`
	Queued             int64  `json:"queued"`
	Running            int64  `json:"running"`
	OverAllowanceDrain bool   `json:"over_allowance_drain"`
}

type canonicalExecutionLimitPolicyReadback struct {
	WorkspaceID string `json:"workspace_id"`
	AppKey      string `json:"app_key"`
	Desired     struct {
		Items []canonicalExecutionLimitPolicy `json:"items"`
	} `json:"desired"`
	Observed struct {
		CommitSHA string                         `json:"commit_sha"`
		Items     []canonicalExecutionLimitShape `json:"items"`
	} `json:"observed"`
	Enforced struct {
		ActiveRelease   []canonicalEnforcedExecutionLimit `json:"active_release"`
		ResidualCohorts []canonicalExecutionLimitResidual `json:"residual_cohorts"`
	} `json:"enforced"`
}

type canonicalExecutionLimitPolicyMutation struct {
	Scope            string `json:"scope"`
	ActionKey        string `json:"action_key"`
	PolicyID         string `json:"policy_id"`
	Kind             string `json:"kind"`
	ShapeFingerprint string `json:"shape_fingerprint"`
	Allowance        *int32 `json:"allowance"`
	WindowSeconds    int32  `json:"window_seconds"`
	ExpectedRevision *int64 `json:"expected_revision"`
	OperationID      string `json:"operation_id"`
	Reason           string `json:"reason"`
}

type executionLimitPreflightError struct {
	Key                 state.ExecutionLimitPolicyKey
	PolicyFingerprint   string
	ObservedFingerprint string
	PolicyRevision      int64
	OperatorAllowance   int32
	CandidateCeiling    *int32
}

func (e *executionLimitPreflightError) Error() string {
	return fmt.Sprintf("execution limit policy %s/%s is incompatible with the candidate Release", e.Key.Kind, e.Key.PolicyID)
}

func (h *Handler) handleCanonicalExecutionLimitPolicies(w http.ResponseWriter, r *http.Request, workspaceID string, appKey string) {
	if !validAppKey(appKey) {
		writeError(w, http.StatusBadRequest, "invalid app key")
		return
	}
	deployment, ok := h.getCanonicalDeployment(w, r, workspaceID, appKey, "app not found")
	if !ok {
		return
	}
	readback, err := h.executionLimitPolicyReadback(r.Context(), deployment)
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, readback)
}

func (h *Handler) handleCanonicalMutateExecutionLimitPolicy(w http.ResponseWriter, r *http.Request, workspaceID string, appKey string, deleting bool) {
	if !validAppKey(appKey) {
		writeError(w, http.StatusBadRequest, "invalid app key")
		return
	}
	var request canonicalExecutionLimitPolicyMutation
	if err := readRequiredJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if request.ExpectedRevision == nil || *request.ExpectedRevision < 0 || !validOperationID(request.OperationID) || !executionlimit.IsFingerprint(strings.TrimSpace(request.ShapeFingerprint)) {
		writeError(w, http.StatusBadRequest, "valid operation_id, expected_revision, and shape_fingerprint are required")
		return
	}
	key, err := state.NormalizeExecutionLimitPolicyKey(state.ExecutionLimitPolicyKey{
		WorkspaceID: workspaceID, AppKey: appKey, ActionKey: request.ActionKey,
		Scope: request.Scope, PolicyID: request.PolicyID, Kind: request.Kind,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !deleting && (request.Allowance == nil || *request.Allowance <= 0) {
		writeError(w, http.StatusBadRequest, "allowance must be positive")
		return
	}
	shapeFingerprint := strings.TrimSpace(request.ShapeFingerprint)
	windowSeconds := request.WindowSeconds
	var observedShape *canonicalExecutionLimitShape
	if deleting {
		current, getErr := h.store.GetExecutionLimitPolicy(r.Context(), key)
		if getErr != nil {
			writeStateError(w, getErr)
			return
		}
		if current.ShapeFingerprint != shapeFingerprint {
			writeExecutionLimitPolicyConflict(w, current, shapeFingerprint, nil, "shape fingerprint does not match the stored policy")
			return
		}
		windowSeconds = current.WindowSeconds
	} else {
		deployment, ok := h.getCanonicalDeployment(w, r, workspaceID, appKey, "app not found")
		if !ok {
			return
		}
		shapes, shapeErr := observedExecutionLimitShapes(deployment)
		if shapeErr != nil {
			writeError(w, http.StatusUnprocessableEntity, shapeErr.Error())
			return
		}
		observed, found := findExecutionLimitShape(shapes, key)
		if !found || observed.ShapeFingerprint != shapeFingerprint {
			payload := map[string]any{
				"error":             "execution limit shape is not present in the active Release",
				"code":              "execution_limit_shape_mismatch",
				"shape_fingerprint": shapeFingerprint,
			}
			if found {
				payload["observed_shape_fingerprint"] = observed.ShapeFingerprint
			}
			writeJSON(w, http.StatusUnprocessableEntity, payload)
			return
		}
		windowSeconds = observed.WindowSeconds
		observedCopy := observed
		observedShape = &observedCopy
	}
	allowance := int32(0)
	if request.Allowance != nil && !deleting {
		allowance = *request.Allowance
	}
	policy := state.ExecutionLimitPolicy{
		ExecutionLimitPolicyKey: key, ShapeFingerprint: shapeFingerprint,
		Allowance: allowance, WindowSeconds: windowSeconds,
	}
	fingerprint := requestFingerprint(struct {
		Policy           state.ExecutionLimitPolicy `json:"policy"`
		ExpectedRevision int64                      `json:"expected_revision"`
		OperationID      string                     `json:"operation_id"`
		Delete           bool                       `json:"delete"`
	}{policy, *request.ExpectedRevision, strings.TrimSpace(request.OperationID), deleting})
	result, replayed, err := h.store.MutateExecutionLimitPolicy(r.Context(), state.MutateExecutionLimitPolicyRequest{
		Policy: policy, ExpectedRevision: *request.ExpectedRevision,
		OperationID: strings.TrimSpace(request.OperationID), RequestFingerprint: fingerprint,
		Delete: deleting, Actor: requestActorSubject(r), Reason: request.Reason,
	})
	if errors.Is(err, state.ErrConflict) {
		current, _ := h.store.GetExecutionLimitPolicy(r.Context(), key)
		writeExecutionLimitPolicyConflict(w, current, shapeFingerprint, observedShape, err.Error())
		return
	}
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"policy":   executionLimitPolicyStateView(result),
		"replayed": replayed,
	})
}

func (h *Handler) handleCanonicalExecutionLimitPolicyAudit(w http.ResponseWriter, r *http.Request, workspaceID string, appKey string) {
	records, err := h.store.ListExecutionLimitPolicyAudit(r.Context(), workspaceID, appKey)
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": records})
}

func (h *Handler) executionLimitPolicyReadback(ctx context.Context, deployment contract.Deployment) (canonicalExecutionLimitPolicyReadback, error) {
	workspaceID := contract.NormalizeWorkspace(deployment.SourceWorkspace())
	policies, err := h.store.ListExecutionLimitPolicies(ctx, workspaceID, deployment.App)
	if err != nil {
		return canonicalExecutionLimitPolicyReadback{}, err
	}
	residuals, err := h.store.ListExecutionLimitResiduals(ctx, workspaceID, deployment.App)
	if err != nil {
		return canonicalExecutionLimitPolicyReadback{}, err
	}
	shapes, err := observedExecutionLimitShapes(deployment)
	if err != nil {
		return canonicalExecutionLimitPolicyReadback{}, err
	}
	shapeByKey := make(map[string]canonicalExecutionLimitShape, len(shapes))
	policyByKey := make(map[string]state.ExecutionLimitPolicy, len(policies))
	for _, shape := range shapes {
		shapeByKey[canonicalExecutionLimitKey(shape.WorkspaceID, shape.AppKey, shape.ActionKey, shape.Scope, shape.PolicyID, shape.Kind)] = shape
	}
	for _, policy := range policies {
		policyByKey[canonicalExecutionLimitPolicyKey(policy.ExecutionLimitPolicyKey)] = policy
	}
	result := canonicalExecutionLimitPolicyReadback{WorkspaceID: workspaceID, AppKey: deployment.App}
	result.Desired.Items = make([]canonicalExecutionLimitPolicy, 0, len(policies))
	result.Observed.CommitSHA = deployment.Commit
	result.Observed.Items = shapes
	result.Enforced.ActiveRelease = make([]canonicalEnforcedExecutionLimit, 0, len(shapes))
	result.Enforced.ResidualCohorts = make([]canonicalExecutionLimitResidual, 0, len(residuals))
	for _, policy := range policies {
		observed, found := shapeByKey[canonicalExecutionLimitPolicyKey(policy.ExecutionLimitPolicyKey)]
		status := "dormant"
		observedFingerprint := ""
		if found {
			observedFingerprint = observed.ShapeFingerprint
			if observed.ShapeFingerprint == policy.ShapeFingerprint {
				status = "applied"
			}
		}
		view := executionLimitPolicyStateView(policy)
		view.Status = status
		view.ObservedFingerprint = observedFingerprint
		result.Desired.Items = append(result.Desired.Items, view)
	}
	for _, shape := range shapes {
		policy, found := policyByKey[canonicalExecutionLimitKey(shape.WorkspaceID, shape.AppKey, shape.ActionKey, shape.Scope, shape.PolicyID, shape.Kind)]
		allowance := (*int32)(nil)
		revision := (*int64)(nil)
		status := "release_ceiling"
		if found && policy.ShapeFingerprint == shape.ShapeFingerprint {
			allowance = int32Pointer(policy.Allowance)
			revision = int64Pointer(policy.Revision)
			status = "operator_allowance"
		}
		effectiveLimit := minimumLimitPointer(shape.ReleaseCeiling, allowance)
		overAllowanceDrain := false
		for _, residual := range residuals {
			if canonicalExecutionLimitPolicyKey(residual.ExecutionLimitPolicyKey) == canonicalExecutionLimitKey(shape.WorkspaceID, shape.AppKey, shape.ActionKey, shape.Scope, shape.PolicyID, shape.Kind) &&
				residual.ShapeFingerprint == shape.ShapeFingerprint && sameInt32Pointer(residual.PinnedCeiling, shape.ReleaseCeiling) &&
				executionLimitResidualOverAllowance(residual, effectiveLimit) {
				overAllowanceDrain = true
				break
			}
		}
		result.Enforced.ActiveRelease = append(result.Enforced.ActiveRelease, canonicalEnforcedExecutionLimit{
			canonicalExecutionLimitShape: shape, OperatorAllowance: allowance,
			EffectiveLimit: effectiveLimit, PolicyRevision: revision, Status: status, OverAllowanceDrain: overAllowanceDrain,
		})
	}
	for _, residual := range residuals {
		policy, found := policyByKey[canonicalExecutionLimitPolicyKey(residual.ExecutionLimitPolicyKey)]
		allowance := (*int32)(nil)
		revision := (*int64)(nil)
		if found && policy.ShapeFingerprint == residual.ShapeFingerprint {
			allowance = int32Pointer(policy.Allowance)
			revision = int64Pointer(policy.Revision)
		}
		effectiveLimit := minimumLimitPointer(residual.PinnedCeiling, allowance)
		result.Enforced.ResidualCohorts = append(result.Enforced.ResidualCohorts, canonicalExecutionLimitResidual{
			canonicalExecutionLimitShape: canonicalExecutionLimitShape{
				WorkspaceID: residual.WorkspaceID, AppKey: residual.AppKey, ActionKey: residual.ActionKey,
				Scope: residual.Scope, PolicyID: residual.PolicyID, Kind: residual.Kind,
				ShapeFingerprint: residual.ShapeFingerprint, ReleaseCeiling: cloneInt32Ptr(residual.PinnedCeiling), WindowSeconds: residual.WindowSeconds,
			},
			OperatorAllowance: allowance, EffectiveLimit: effectiveLimit,
			PolicyRevision: revision, Queued: residual.Queued, Running: residual.Running,
			OverAllowanceDrain: executionLimitResidualOverAllowance(residual, effectiveLimit),
		})
	}
	return result, nil
}

func observedExecutionLimitShapes(deployment contract.Deployment) ([]canonicalExecutionLimitShape, error) {
	workspaceID := contract.NormalizeWorkspace(deployment.SourceWorkspace())
	shapes := make([]canonicalExecutionLimitShape, 0, 1+contract.MaxKeyedConcurrencyLimits+contract.MaxKeyedRateLimits)
	appFingerprint, err := executionlimit.AppConcurrencyFingerprint(workspaceID, deployment.App)
	if err != nil {
		return nil, err
	}
	shapes = append(shapes, canonicalExecutionLimitShape{
		WorkspaceID: workspaceID, AppKey: deployment.App, Scope: executionlimit.ScopeApp,
		PolicyID: executionlimit.ImplicitAppConcurrencyPolicyID, Kind: executionlimit.KindConcurrency,
		ShapeFingerprint: appFingerprint, ReleaseCeiling: cloneInt32Ptr(deployment.MaxConcurrent),
	})
	appendLimits := func(actionKey string, scope string, limits contract.ExecutionLimits) error {
		normalized, normalizeErr := contract.NormalizeExecutionLimits(limits)
		if normalizeErr != nil {
			return normalizeErr
		}
		for _, limit := range normalized.Concurrency {
			fingerprint, fingerprintErr := executionlimit.Fingerprint(executionlimit.Shape{
				WorkspaceID: workspaceID, AppKey: deployment.App, ActionKey: actionKey,
				Scope: scope, PolicyID: limit.ID, Kind: executionlimit.KindConcurrency, InputPointers: limit.InputPointers,
			})
			if fingerprintErr != nil {
				return fingerprintErr
			}
			ceiling := limit.MaxConcurrent
			shapes = append(shapes, canonicalExecutionLimitShape{WorkspaceID: workspaceID, AppKey: deployment.App, ActionKey: actionKey, Scope: scope, PolicyID: limit.ID, Kind: executionlimit.KindConcurrency, ShapeFingerprint: fingerprint, ReleaseCeiling: &ceiling})
		}
		for _, limit := range normalized.Rate {
			fingerprint, fingerprintErr := executionlimit.Fingerprint(executionlimit.Shape{
				WorkspaceID: workspaceID, AppKey: deployment.App, ActionKey: actionKey,
				Scope: scope, PolicyID: limit.ID, Kind: executionlimit.KindRate, InputPointers: limit.InputPointers, WindowSeconds: limit.WindowSeconds,
			})
			if fingerprintErr != nil {
				return fingerprintErr
			}
			ceiling := limit.MaxAttempts
			shapes = append(shapes, canonicalExecutionLimitShape{WorkspaceID: workspaceID, AppKey: deployment.App, ActionKey: actionKey, Scope: scope, PolicyID: limit.ID, Kind: executionlimit.KindRate, ShapeFingerprint: fingerprint, ReleaseCeiling: &ceiling, WindowSeconds: limit.WindowSeconds})
		}
		return nil
	}
	if err := appendLimits("", executionlimit.ScopeApp, deployment.ExecutionLimits); err != nil {
		return nil, err
	}
	actionKeys := make([]string, 0, len(deployment.Actions))
	for actionKey := range deployment.Actions {
		actionKeys = append(actionKeys, actionKey)
	}
	sort.Strings(actionKeys)
	for _, actionKey := range actionKeys {
		if err := appendLimits(actionKey, executionlimit.ScopeAction, deployment.Actions[actionKey].ExecutionLimits); err != nil {
			return nil, err
		}
	}
	return shapes, nil
}

func (h *Handler) validateExecutionLimitPolicyCompatibility(ctx context.Context, deployment contract.Deployment) error {
	if h.store == nil {
		return nil
	}
	policies, err := h.store.ListExecutionLimitPolicies(ctx, deployment.SourceWorkspace(), deployment.App)
	if err != nil {
		return err
	}
	shapes, err := observedExecutionLimitShapes(deployment)
	if err != nil {
		return err
	}
	shapeByKey := make(map[string]canonicalExecutionLimitShape, len(shapes))
	for _, shape := range shapes {
		shapeByKey[canonicalExecutionLimitKey(shape.WorkspaceID, shape.AppKey, shape.ActionKey, shape.Scope, shape.PolicyID, shape.Kind)] = shape
	}
	for _, policy := range policies {
		observed, found := shapeByKey[canonicalExecutionLimitPolicyKey(policy.ExecutionLimitPolicyKey)]
		if !found || observed.ShapeFingerprint != policy.ShapeFingerprint {
			observedFingerprint := ""
			if found {
				observedFingerprint = observed.ShapeFingerprint
			}
			return &executionLimitPreflightError{
				Key: policy.ExecutionLimitPolicyKey, PolicyFingerprint: policy.ShapeFingerprint,
				ObservedFingerprint: observedFingerprint, PolicyRevision: policy.Revision,
				OperatorAllowance: policy.Allowance, CandidateCeiling: cloneInt32Ptr(observed.ReleaseCeiling),
			}
		}
	}
	return nil
}

func findExecutionLimitShape(shapes []canonicalExecutionLimitShape, key state.ExecutionLimitPolicyKey) (canonicalExecutionLimitShape, bool) {
	wanted := canonicalExecutionLimitPolicyKey(key)
	for _, shape := range shapes {
		if canonicalExecutionLimitKey(shape.WorkspaceID, shape.AppKey, shape.ActionKey, shape.Scope, shape.PolicyID, shape.Kind) == wanted {
			return shape, true
		}
	}
	return canonicalExecutionLimitShape{}, false
}

func executionLimitPolicyStateView(policy state.ExecutionLimitPolicy) canonicalExecutionLimitPolicy {
	view := canonicalExecutionLimitPolicy{
		WorkspaceID: policy.WorkspaceID, AppKey: policy.AppKey, ActionKey: policy.ActionKey,
		Scope: policy.Scope, PolicyID: policy.PolicyID, Kind: policy.Kind,
		ShapeFingerprint: policy.ShapeFingerprint, WindowSeconds: policy.WindowSeconds,
		OperatorAllowance: int32Pointer(policy.Allowance), Revision: policy.Revision,
		OperationID: policy.LastOperationID, UpdatedBy: policy.UpdatedBy, UpdatedAt: policy.UpdatedAt,
	}
	if policy.Deleted {
		view.OperatorAllowance = nil
		view.Status = "deleted"
	}
	return view
}

func canonicalExecutionLimitPolicyKey(key state.ExecutionLimitPolicyKey) string {
	return canonicalExecutionLimitKey(key.WorkspaceID, key.AppKey, key.ActionKey, key.Scope, key.PolicyID, key.Kind)
}

func canonicalExecutionLimitKey(workspaceID string, appKey string, actionKey string, scope string, policyID string, kind string) string {
	return strings.Join([]string{contract.NormalizeWorkspace(workspaceID), appKey, actionKey, scope, policyID, kind}, "\x1f")
}

func minimumLimitPointer(ceiling *int32, allowance *int32) *int32 {
	if ceiling == nil {
		return cloneInt32Ptr(allowance)
	}
	if allowance == nil || *ceiling <= *allowance {
		return cloneInt32Ptr(ceiling)
	}
	return cloneInt32Ptr(allowance)
}

func executionLimitResidualOverAllowance(residual state.ExecutionLimitResidual, effectiveLimit *int32) bool {
	return residual.Kind == executionlimit.KindConcurrency && effectiveLimit != nil && residual.MaxRunningForKey > int64(*effectiveLimit)
}

func sameInt32Pointer(left *int32, right *int32) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func int32Pointer(value int32) *int32 {
	copy := value
	return &copy
}

func int64Pointer(value int64) *int64 {
	copy := value
	return &copy
}

func writeExecutionLimitPolicyConflict(w http.ResponseWriter, current state.ExecutionLimitPolicy, requestedFingerprint string, observed *canonicalExecutionLimitShape, message string) {
	payload := map[string]any{"error": message, "code": "execution_limit_policy_conflict"}
	if current.Revision > 0 {
		payload["current_revision"] = current.Revision
		payload["current_shape_fingerprint"] = current.ShapeFingerprint
		payload["current_operation_id"] = current.LastOperationID
		payload["current_operator_allowance"] = current.Allowance
		payload["compatibility"] = "unknown"
		if observed != nil {
			if current.ShapeFingerprint == requestedFingerprint && current.ShapeFingerprint == observed.ShapeFingerprint {
				payload["compatibility"] = "applied"
				payload["current_effective_limit"] = minimumLimitPointer(observed.ReleaseCeiling, int32Pointer(current.Allowance))
			} else {
				payload["compatibility"] = "incompatible"
			}
		}
	}
	writeJSON(w, http.StatusConflict, payload)
}

func writeExecutionLimitPreflightError(w http.ResponseWriter, err error) bool {
	var preflight *executionLimitPreflightError
	if !errors.As(err, &preflight) {
		return false
	}
	writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
		"error": err.Error(), "code": "execution_limit_policy_incompatible",
		"scope": preflight.Key.Scope, "action_key": preflight.Key.ActionKey,
		"policy_id": preflight.Key.PolicyID, "kind": preflight.Key.Kind,
		"policy_shape_fingerprint":   preflight.PolicyFingerprint,
		"observed_shape_fingerprint": preflight.ObservedFingerprint,
		"current_revision":           preflight.PolicyRevision,
		"current_operator_allowance": preflight.OperatorAllowance,
		"candidate_release_ceiling":  preflight.CandidateCeiling,
		"compatibility":              "incompatible",
	})
	return true
}
