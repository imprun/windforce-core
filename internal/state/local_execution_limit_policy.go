package state

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

func (s *LocalStore) ListExecutionLimitPolicies(ctx context.Context, workspaceID string, appKey string) ([]ExecutionLimitPolicy, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	appKey = strings.TrimSpace(appKey)
	policies := make([]ExecutionLimitPolicy, 0)
	for _, policy := range snapshot.ExecutionLimitPolicies {
		if policy.WorkspaceID == workspaceID && (appKey == "" || policy.AppKey == appKey) && !policy.Deleted {
			policies = append(policies, cloneExecutionLimitPolicy(policy))
		}
	}
	sortExecutionLimitPolicies(policies)
	return policies, nil
}

func (s *LocalStore) GetExecutionLimitPolicy(ctx context.Context, key ExecutionLimitPolicyKey) (ExecutionLimitPolicy, error) {
	key, err := NormalizeExecutionLimitPolicyKey(key)
	if err != nil {
		return ExecutionLimitPolicy{}, err
	}
	snapshot, err := s.Load(ctx)
	if err != nil {
		return ExecutionLimitPolicy{}, err
	}
	policy, ok := snapshot.ExecutionLimitPolicies[executionLimitPolicyStorageKey(key)]
	if !ok || policy.Deleted {
		return ExecutionLimitPolicy{}, ErrNotFound
	}
	return cloneExecutionLimitPolicy(policy), nil
}

func (s *LocalStore) MutateExecutionLimitPolicy(ctx context.Context, request MutateExecutionLimitPolicyRequest) (ExecutionLimitPolicy, bool, error) {
	request, err := NormalizeExecutionLimitPolicyMutation(request)
	if err != nil {
		return ExecutionLimitPolicy{}, false, err
	}
	var result ExecutionLimitPolicy
	replayed := false
	err = s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		var applyErr error
		result, replayed, applyErr = applyLocalExecutionLimitPolicyMutation(snapshot, request, now)
		return applyErr
	})
	return cloneExecutionLimitPolicy(result), replayed, err
}

func (s *LocalStore) MutateExecutionLimitPolicies(ctx context.Context, requests []MutateExecutionLimitPolicyRequest) ([]ExecutionLimitPolicyMutationResult, error) {
	normalized, err := normalizeExecutionLimitPolicyBatch(requests)
	if err != nil {
		return nil, err
	}
	results := make([]ExecutionLimitPolicyMutationResult, len(normalized))
	err = s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		for index, request := range normalized {
			policy, replayed, applyErr := applyLocalExecutionLimitPolicyMutation(snapshot, request, now)
			if applyErr != nil {
				return applyErr
			}
			results[index] = ExecutionLimitPolicyMutationResult{Policy: policy, Replayed: replayed}
		}
		return nil
	})
	return results, err
}

func applyLocalExecutionLimitPolicyMutation(snapshot *Snapshot, request MutateExecutionLimitPolicyRequest, now time.Time) (ExecutionLimitPolicy, bool, error) {
	storageKey := executionLimitPolicyStorageKey(request.Policy.ExecutionLimitPolicyKey)
	current, exists := snapshot.ExecutionLimitPolicies[storageKey]
	if exists && current.LastOperationID == request.OperationID {
		if current.LastRequestFingerprint != request.RequestFingerprint {
			return ExecutionLimitPolicy{}, false, ErrConflict
		}
		return cloneExecutionLimitPolicy(current), true, nil
	}
	if (!exists && request.ExpectedRevision != 0) || (exists && current.Revision != request.ExpectedRevision) {
		return ExecutionLimitPolicy{}, false, ErrConflict
	}
	if exists && !current.Deleted && current.ShapeFingerprint != request.Policy.ShapeFingerprint {
		return ExecutionLimitPolicy{}, false, ErrConflict
	}
	previousAllowance := (*int32)(nil)
	if exists && !current.Deleted {
		previousAllowance = int32Value(current.Allowance)
	}
	result := request.Policy
	result.Revision = request.ExpectedRevision + 1
	result.Deleted = request.Delete
	result.LastOperationID = request.OperationID
	result.LastRequestFingerprint = request.RequestFingerprint
	result.UpdatedBy = request.Actor
	result.UpdatedAt = now
	snapshot.ExecutionLimitPolicies[storageKey] = result
	eventKind := "updated"
	allowance := int32Value(result.Allowance)
	if request.Delete {
		eventKind = "deleted"
		allowance = nil
	} else if !exists || current.Deleted {
		eventKind = "created"
	}
	audit := ExecutionLimitPolicyAudit{
		ID: NewID("audit"), ExecutionLimitPolicyKey: result.ExecutionLimitPolicyKey,
		EventKind: eventKind, ShapeFingerprint: result.ShapeFingerprint,
		PreviousAllowance: previousAllowance, Allowance: allowance, Revision: result.Revision,
		OperationID: request.OperationID, Reason: request.Reason, Actor: request.Actor, CreatedAt: now,
	}
	auditKey := executionLimitPolicyAuditKey(result.WorkspaceID, result.AppKey)
	snapshot.ExecutionLimitAudits[auditKey] = append(snapshot.ExecutionLimitAudits[auditKey], audit)
	return result, false, nil
}

func (s *LocalStore) ListExecutionLimitPolicyAudit(ctx context.Context, workspaceID string, appKey string) ([]ExecutionLimitPolicyAudit, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	appKey = strings.TrimSpace(appKey)
	audits := make([]ExecutionLimitPolicyAudit, 0)
	if appKey != "" {
		audits = append(audits, snapshot.ExecutionLimitAudits[executionLimitPolicyAuditKey(workspaceID, appKey)]...)
	} else {
		for _, records := range snapshot.ExecutionLimitAudits {
			for _, record := range records {
				if record.WorkspaceID == workspaceID {
					audits = append(audits, record)
				}
			}
		}
	}
	sort.Slice(audits, func(i, j int) bool {
		if !audits[i].CreatedAt.Equal(audits[j].CreatedAt) {
			return audits[i].CreatedAt.Before(audits[j].CreatedAt)
		}
		return audits[i].ID < audits[j].ID
	})
	return audits, nil
}

func sortExecutionLimitPolicies(policies []ExecutionLimitPolicy) {
	sort.Slice(policies, func(i, j int) bool {
		if policies[i].Scope != policies[j].Scope {
			return policies[i].Scope < policies[j].Scope
		}
		if policies[i].ActionKey != policies[j].ActionKey {
			return policies[i].ActionKey < policies[j].ActionKey
		}
		if policies[i].Kind != policies[j].Kind {
			return policies[i].Kind < policies[j].Kind
		}
		return policies[i].PolicyID < policies[j].PolicyID
	})
}
