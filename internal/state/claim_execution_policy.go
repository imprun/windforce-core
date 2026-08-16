package state

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/imprun/windforce-core/internal/executionlimit"
)

type executionLimitPolicyLookup map[string]ExecutionLimitPolicy

func executionLimitPolicyLookupFromSnapshot(snapshot *Snapshot) executionLimitPolicyLookup {
	return executionLimitPolicyLookup(snapshot.ExecutionLimitPolicies)
}

func executionLimitPolicyLookupFromSlice(policies []ExecutionLimitPolicy) executionLimitPolicyLookup {
	lookup := make(executionLimitPolicyLookup, len(policies))
	for _, policy := range policies {
		lookup[executionLimitPolicyStorageKey(policy.ExecutionLimitPolicyKey)] = policy
	}
	return lookup
}

func executionPolicyKeyForJob(candidate Job, scope string, policyID string, kind string) ExecutionLimitPolicyKey {
	key := ExecutionLimitPolicyKey{
		WorkspaceID: normalizedJobWorkspace("", candidate),
		AppKey:      jobAppKey(candidate),
		Scope:       scope,
		PolicyID:    policyID,
		Kind:        kind,
	}
	if scope == executionlimit.ScopeAction {
		key.ActionKey = strings.TrimSpace(candidate.Payload.Action)
	}
	return key
}

func matchingPolicyAllowance(policies executionLimitPolicyLookup, key ExecutionLimitPolicyKey, shapeFingerprint string) (int, bool) {
	if !executionlimit.IsFingerprint(shapeFingerprint) {
		return 0, false
	}
	policy, ok := policies[executionLimitPolicyStorageKey(key)]
	if !ok || policy.Deleted || policy.Allowance <= 0 || policy.ShapeFingerprint != shapeFingerprint {
		return 0, false
	}
	return int(policy.Allowance), true
}

func effectiveAppConcurrencyLimit(candidate Job, policies executionLimitPolicyLookup) (int, bool, bool) {
	appKey := jobAppKey(candidate)
	if appKey == "" {
		return 0, false, true
	}
	releaseLimit, releaseLimited := jobMaxConcurrent(candidate)
	fingerprint := ""
	if pin := candidate.Payload.ExecutionLimits.AppConcurrency; pin != nil {
		if pin.PolicyID != executionlimit.ImplicitAppConcurrencyPolicyID || !executionlimit.IsFingerprint(pin.ShapeFingerprint) {
			return 0, false, false
		}
		fingerprint = pin.ShapeFingerprint
	} else {
		// Jobs written before ADR 0042 can safely join the implicit App-wide
		// shape because it has no input-derived key material.
		var err error
		fingerprint, err = executionlimit.AppConcurrencyFingerprint(normalizedJobWorkspace("", candidate), appKey)
		if err != nil {
			return 0, false, false
		}
	}
	key := executionPolicyKeyForJob(candidate, executionlimit.ScopeApp, executionlimit.ImplicitAppConcurrencyPolicyID, executionlimit.KindConcurrency)
	allowance, hasAllowance := matchingPolicyAllowance(policies, key, fingerprint)
	return minimumExecutionLimit(releaseLimit, releaseLimited, allowance, hasAllowance)
}

func effectiveKeyedConcurrencyLimit(candidate Job, pin KeyedConcurrencyLimitPin, policies executionLimitPolicyLookup) (int, bool) {
	limit := int(pin.MaxConcurrent)
	if pin.ShapeFingerprint == "" {
		return limit, true
	}
	if !executionlimit.IsFingerprint(pin.ShapeFingerprint) {
		return 0, false
	}
	key := executionPolicyKeyForJob(candidate, pin.Scope, pin.PolicyID, executionlimit.KindConcurrency)
	if allowance, ok := matchingPolicyAllowance(policies, key, pin.ShapeFingerprint); ok && allowance < limit {
		limit = allowance
	}
	return limit, true
}

func effectiveKeyedRateLimit(candidate Job, pin KeyedRateLimitPin, policies executionLimitPolicyLookup) (int32, bool) {
	limit := pin.MaxAttempts
	if pin.ShapeFingerprint == "" {
		return limit, true
	}
	if !executionlimit.IsFingerprint(pin.ShapeFingerprint) {
		return 0, false
	}
	key := executionPolicyKeyForJob(candidate, pin.Scope, pin.PolicyID, executionlimit.KindRate)
	if allowance, ok := matchingPolicyAllowance(policies, key, pin.ShapeFingerprint); ok && int32(allowance) < limit {
		limit = int32(allowance)
	}
	return limit, true
}

func minimumExecutionLimit(first int, hasFirst bool, second int, hasSecond bool) (int, bool, bool) {
	if !hasFirst && !hasSecond {
		return 0, false, true
	}
	if !hasFirst {
		return second, true, second > 0
	}
	if !hasSecond {
		return first, true, first > 0
	}
	if second < first {
		first = second
	}
	return first, true, first > 0
}

func candidateExecutionPolicyRequirements(candidate Job) []struct {
	Key         ExecutionLimitPolicyKey
	Fingerprint string
} {
	requirements := make([]struct {
		Key         ExecutionLimitPolicyKey
		Fingerprint string
	}, 0, len(candidate.Payload.ExecutionLimits.Concurrency)+len(candidate.Payload.ExecutionLimits.Rate)+1)
	appFingerprint := ""
	if pin := candidate.Payload.ExecutionLimits.AppConcurrency; pin != nil {
		appFingerprint = pin.ShapeFingerprint
	} else if appKey := jobAppKey(candidate); appKey != "" {
		appFingerprint, _ = executionlimit.AppConcurrencyFingerprint(normalizedJobWorkspace("", candidate), appKey)
	}
	if appFingerprint != "" {
		requirements = append(requirements, struct {
			Key         ExecutionLimitPolicyKey
			Fingerprint string
		}{executionPolicyKeyForJob(candidate, executionlimit.ScopeApp, executionlimit.ImplicitAppConcurrencyPolicyID, executionlimit.KindConcurrency), appFingerprint})
	}
	for _, pin := range candidate.Payload.ExecutionLimits.Concurrency {
		if pin.ShapeFingerprint != "" {
			requirements = append(requirements, struct {
				Key         ExecutionLimitPolicyKey
				Fingerprint string
			}{executionPolicyKeyForJob(candidate, pin.Scope, pin.PolicyID, executionlimit.KindConcurrency), pin.ShapeFingerprint})
		}
	}
	for _, pin := range candidate.Payload.ExecutionLimits.Rate {
		if pin.ShapeFingerprint != "" {
			requirements = append(requirements, struct {
				Key         ExecutionLimitPolicyKey
				Fingerprint string
			}{executionPolicyKeyForJob(candidate, pin.Scope, pin.PolicyID, executionlimit.KindRate), pin.ShapeFingerprint})
		}
	}
	return requirements
}

func postgresExecutionPoliciesForCandidate(ctx context.Context, tx pgx.Tx, candidate Job) (executionLimitPolicyLookup, error) {
	requirements := candidateExecutionPolicyRequirements(candidate)
	if len(requirements) == 0 {
		return executionLimitPolicyLookup{}, nil
	}
	actionKeys := make([]string, len(requirements))
	scopes := make([]string, len(requirements))
	policyIDs := make([]string, len(requirements))
	kinds := make([]string, len(requirements))
	for i, requirement := range requirements {
		actionKeys[i] = requirement.Key.ActionKey
		scopes[i] = requirement.Key.Scope
		policyIDs[i] = requirement.Key.PolicyID
		kinds[i] = requirement.Key.Kind
	}
	rows, err := tx.Query(ctx, `
WITH requested(req_action_key, req_scope, req_policy_id, req_kind) AS (
	SELECT * FROM unnest($3::text[], $4::text[], $5::text[], $6::text[])
)
SELECT `+executionLimitPolicyColumns+`
FROM execution_limit_policy p
JOIN requested r
	ON p.action_key=r.req_action_key
	AND p.scope=r.req_scope
	AND p.policy_id=r.req_policy_id
	AND p.kind=r.req_kind
WHERE p.workspace_id=$1 AND p.app_key=$2 AND p.deleted=false
`, normalizedJobWorkspace("", candidate), jobAppKey(candidate), actionKeys, scopes, policyIDs, kinds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	policies := make([]ExecutionLimitPolicy, 0)
	for rows.Next() {
		policy, scanErr := scanExecutionLimitPolicy(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		policies = append(policies, policy)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return executionLimitPolicyLookupFromSlice(policies), nil
}

func postgresAllExecutionPolicies(ctx context.Context, tx pgx.Tx) (executionLimitPolicyLookup, error) {
	rows, err := tx.Query(ctx, `SELECT `+executionLimitPolicyColumns+` FROM execution_limit_policy WHERE deleted=false`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lookup := executionLimitPolicyLookup{}
	for rows.Next() {
		policy, scanErr := scanExecutionLimitPolicy(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		lookup[executionLimitPolicyStorageKey(policy.ExecutionLimitPolicyKey)] = policy
	}
	return lookup, rows.Err()
}
