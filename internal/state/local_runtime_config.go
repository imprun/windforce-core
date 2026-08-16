package state

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/resourceconfig"
)

func (s *LocalStore) GetVariableScoped(ctx context.Context, workspaceID string, scope contract.RuntimeConfigScope, appKey string, path string) (Variable, bool, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return Variable{}, false, err
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	key, err := normalizedRuntimeObjectKey(scope, appKey, path)
	if err != nil {
		return Variable{}, false, err
	}
	variable, found := snapshot.Variables[workspaceID][key]
	return variable, found, nil
}

func (s *LocalStore) GetResourceScoped(ctx context.Context, workspaceID string, scope contract.RuntimeConfigScope, appKey string, path string) (Resource, bool, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return Resource{}, false, err
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	key, err := normalizedRuntimeObjectKey(scope, appKey, path)
	if err != nil {
		return Resource{}, false, err
	}
	resource, found := snapshot.Resources[workspaceID][key]
	resource.Value = cloneRaw(resource.Value)
	return resource, found, nil
}

func (s *LocalStore) MutateRuntimeVariable(ctx context.Context, request RuntimeVariableMutationRequest) (RuntimeConfigMutationResult, error) {
	workspaceID, appKey, path, err := normalizeRuntimeMutation(
		request.WorkspaceID, request.AppKey, request.Path, request.OperationID,
		request.RequestFingerprint, request.JobID, request.Attempt,
	)
	if err != nil {
		return RuntimeConfigMutationResult{}, err
	}
	if err := validateRuntimeVariableValue(request); err != nil {
		return RuntimeConfigMutationResult{}, err
	}
	request.WorkspaceID, request.AppKey, request.Path = workspaceID, appKey, path
	var result RuntimeConfigMutationResult
	err = s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		job, err := authorizeRuntimeMutation(snapshot, now, workspaceID, appKey, request.JobID, request.Attempt)
		if err != nil {
			return err
		}
		storage := contract.RuntimeVariableStoragePlain
		if request.IsSecret {
			storage = contract.RuntimeVariableStorageSecret
		}
		if !allowsVariableWrite(job.Payload.RuntimeAccess, path, storage) {
			return runtimeConfigError(RuntimeConfigCodeStorageClassMismatch, fmt.Errorf("variable write target or storage class is not pinned: %w", ErrForbidden))
		}
		operationKey := runtimeConfigOperationKey(workspaceID, request.JobID, request.Attempt, request.OperationID)
		if replay, ok := snapshot.RuntimeConfigOperations[operationKey]; ok {
			if replay.RequestFingerprint != request.RequestFingerprint {
				return runtimeConfigError(RuntimeConfigCodeOperationConflict, fmt.Errorf("operationId was already used with another payload: %w", ErrConflict))
			}
			result = RuntimeConfigMutationResult{Path: replay.Path, Revision: replay.Revision, Replayed: true}
			return nil
		}
		if runtimeConfigAttemptWriteCount(snapshot, workspaceID, request.JobID, request.Attempt) >= RuntimeConfigMaxWritesPerAttempt {
			return runtimeConfigError(RuntimeConfigCodeLimitExceeded, fmt.Errorf("runtime writes exceed per-attempt limit %d: %w", RuntimeConfigMaxWritesPerAttempt, ErrInvalidState))
		}
		if snapshot.Variables[workspaceID] == nil {
			snapshot.Variables[workspaceID] = map[string]Variable{}
		}
		key := runtimeConfigObjectKey(contract.RuntimeConfigScopeApp, appKey, path)
		current := snapshot.Variables[workspaceID][key]
		if request.ExpectedRevision != nil && current.Revision != *request.ExpectedRevision {
			return runtimeConfigRevisionError(current.Revision)
		}
		revision := current.Revision + 1
		if revision <= 0 {
			revision = 1
		}
		variable := Variable{
			OwnerScope: contract.RuntimeConfigScopeApp,
			AppKey:     appKey, Path: path, Value: request.Value,
			IsSecret: request.IsSecret, Description: request.Description,
			Revision: revision, UpdatedAt: now.UTC(),
		}
		snapshot.Variables[workspaceID][key] = variable
		recordRuntimeConfigSuccess(snapshot, now, RuntimeConfigOperation{
			WorkspaceID: workspaceID, JobID: request.JobID, Attempt: request.Attempt,
			OperationID: request.OperationID, RequestFingerprint: request.RequestFingerprint,
			ObjectKind: "variable", AppKey: appKey, Path: path, Revision: revision,
		}, RuntimeConfigAudit{
			WorkspaceID: workspaceID, OwnerScope: contract.RuntimeConfigScopeApp,
			AppKey: appKey, Path: path, ObjectKind: "variable", Storage: string(storage),
			Revision: revision, OperationID: request.OperationID, JobID: request.JobID,
			Attempt: request.Attempt, Actor: runtimeMutationActor(request.Actor, request.JobID),
		})
		result = RuntimeConfigMutationResult{Path: path, Revision: revision}
		return nil
	})
	return result, err
}

func (s *LocalStore) MutateRuntimeResource(ctx context.Context, request RuntimeResourceMutationRequest) (RuntimeConfigMutationResult, error) {
	workspaceID, appKey, path, err := normalizeRuntimeMutation(
		request.WorkspaceID, request.AppKey, request.Path, request.OperationID,
		request.RequestFingerprint, request.JobID, request.Attempt,
	)
	if err != nil {
		return RuntimeConfigMutationResult{}, err
	}
	if len(request.Value) == 0 || len(request.Value) > RuntimeConfigMaxValueBytes || !json.Valid(request.Value) {
		return RuntimeConfigMutationResult{}, runtimeConfigError(RuntimeConfigCodeLimitExceeded, fmt.Errorf("resource value must be valid JSON within %d bytes: %w", RuntimeConfigMaxValueBytes, ErrInvalidState))
	}
	request.WorkspaceID, request.AppKey, request.Path = workspaceID, appKey, path
	var result RuntimeConfigMutationResult
	err = s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		job, err := authorizeRuntimeMutation(snapshot, now, workspaceID, appKey, request.JobID, request.Attempt)
		if err != nil {
			return err
		}
		if !allowsResourceWrite(job.Payload.RuntimeAccess, path) {
			return runtimeConfigError(RuntimeConfigCodeForbidden, fmt.Errorf("resource write target is not pinned: %w", ErrForbidden))
		}
		operationKey := runtimeConfigOperationKey(workspaceID, request.JobID, request.Attempt, request.OperationID)
		if replay, ok := snapshot.RuntimeConfigOperations[operationKey]; ok {
			if replay.RequestFingerprint != request.RequestFingerprint {
				return runtimeConfigError(RuntimeConfigCodeOperationConflict, fmt.Errorf("operationId was already used with another payload: %w", ErrConflict))
			}
			result = RuntimeConfigMutationResult{Path: replay.Path, Revision: replay.Revision, Replayed: true}
			return nil
		}
		if runtimeConfigAttemptWriteCount(snapshot, workspaceID, request.JobID, request.Attempt) >= RuntimeConfigMaxWritesPerAttempt {
			return runtimeConfigError(RuntimeConfigCodeLimitExceeded, fmt.Errorf("runtime writes exceed per-attempt limit %d: %w", RuntimeConfigMaxWritesPerAttempt, ErrInvalidState))
		}
		name, version, err := resourceconfig.ParseTypeReference(request.ResourceType)
		if err != nil {
			return fmt.Errorf("%w: App-owned Resource requires a versioned resource type: %v", ErrInvalidState, err)
		}
		registered, found := snapshot.ResourceTypes[workspaceID][resourceTypeKey(name, version)]
		if !found {
			return ErrNotFound
		}
		if err := resourceconfig.ValidateValue(registered.Schema, request.Value); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidState, err)
		}
		if err := authorizeResourceReferences(request.Value, job.Payload.RuntimeAccess); err != nil {
			return err
		}
		if snapshot.Resources[workspaceID] == nil {
			snapshot.Resources[workspaceID] = map[string]Resource{}
		}
		key := runtimeConfigObjectKey(contract.RuntimeConfigScopeApp, appKey, path)
		current := snapshot.Resources[workspaceID][key]
		if request.ExpectedRevision != nil && current.Revision != *request.ExpectedRevision {
			return runtimeConfigRevisionError(current.Revision)
		}
		revision := current.Revision + 1
		if revision <= 0 {
			revision = 1
		}
		snapshot.Resources[workspaceID][key] = Resource{
			OwnerScope: contract.RuntimeConfigScopeApp, AppKey: appKey, Path: path,
			Value: cloneRaw(request.Value), ResourceType: name + "@" + version,
			Description: request.Description, Revision: revision, UpdatedAt: now.UTC(),
		}
		recordRuntimeConfigSuccess(snapshot, now, RuntimeConfigOperation{
			WorkspaceID: workspaceID, JobID: request.JobID, Attempt: request.Attempt,
			OperationID: request.OperationID, RequestFingerprint: request.RequestFingerprint,
			ObjectKind: "resource", AppKey: appKey, Path: path, Revision: revision,
		}, RuntimeConfigAudit{
			WorkspaceID: workspaceID, OwnerScope: contract.RuntimeConfigScopeApp,
			AppKey: appKey, Path: path, ObjectKind: "resource", Revision: revision,
			OperationID: request.OperationID, JobID: request.JobID, Attempt: request.Attempt,
			Actor: runtimeMutationActor(request.Actor, request.JobID),
		})
		result = RuntimeConfigMutationResult{Path: path, Revision: revision}
		return nil
	})
	return result, err
}

func (s *LocalStore) ListRuntimeConfigAudit(ctx context.Context, workspaceID string, appKey string) ([]RuntimeConfigAudit, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	items := append([]RuntimeConfigAudit(nil), snapshot.RuntimeConfigAudits[workspaceID]...)
	if strings.TrimSpace(appKey) != "" {
		filtered := items[:0]
		for _, item := range items {
			if item.AppKey == strings.TrimSpace(appKey) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func authorizeRuntimeMutation(snapshot *Snapshot, now time.Time, workspaceID, appKey, jobID string, attempt int) (Job, error) {
	job, found := snapshot.Jobs[jobID]
	if !found || contract.NormalizeWorkspace(job.Payload.Workspace) != workspaceID || job.Payload.App != appKey ||
		job.Attempt != attempt || job.State != JobRunning || job.LeaseOwner == "" ||
		job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.After(now) {
		return Job{}, runtimeConfigError(RuntimeConfigCodeAttemptInvalid, fmt.Errorf("job attempt is not live: %w", ErrInvalidState))
	}
	if !appRuntimeAllowsRunningAttempt(snapshot, job) {
		return Job{}, runtimeConfigError(RuntimeConfigCodeForbidden, fmt.Errorf("App runtime access is not active for this attempt: %w", ErrForbidden))
	}
	return job, nil
}

func allowsVariableWrite(access contract.RuntimeAccess, path string, storage contract.RuntimeVariableStorage) bool {
	for _, target := range access.WriteVariables {
		if target.Scope == contract.RuntimeConfigScopeApp && target.Path == path && target.Storage == storage {
			return true
		}
	}
	return false
}

func allowsResourceWrite(access contract.RuntimeAccess, path string) bool {
	for _, target := range access.WriteResources {
		if target.Scope == contract.RuntimeConfigScopeApp && target.Path == path {
			return true
		}
	}
	return false
}

func authorizeResourceReferences(value json.RawMessage, access contract.RuntimeAccess) error {
	var decoded any
	if err := json.Unmarshal(value, &decoded); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidState, err)
	}
	count := 0
	var visit func(any) error
	visit = func(item any) error {
		switch typed := item.(type) {
		case string:
			reference, ok, err := contract.ParseRuntimeConfigReference(typed)
			if err != nil {
				return fmt.Errorf("%w: %v", ErrInvalidState, err)
			}
			if !ok {
				return nil
			}
			count++
			if count > 256 {
				return runtimeConfigError(RuntimeConfigCodeLimitExceeded, fmt.Errorf("resource references exceed 256: %w", ErrInvalidState))
			}
			if !runtimeAccessAllowsReference(access, reference) {
				return runtimeConfigError(RuntimeConfigCodeReferenceForbidden, fmt.Errorf("resource reference is outside the pinned read closure: %w", ErrForbidden))
			}
		case []any:
			for _, child := range typed {
				if err := visit(child); err != nil {
					return err
				}
			}
		case map[string]any:
			for _, child := range typed {
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return visit(decoded)
}

func runtimeAccessAllowsReference(access contract.RuntimeAccess, reference contract.RuntimeConfigReference) bool {
	legacy := access.Variables
	targets := access.VariableTargets
	if reference.Kind == "res" {
		legacy = access.Resources
		targets = access.ResourceTargets
	}
	if reference.Scope == contract.RuntimeConfigScopeWorkspace {
		for _, path := range legacy {
			if path == reference.Path {
				return true
			}
		}
	}
	for _, target := range targets {
		if target.Scope == reference.Scope && target.Path == reference.Path {
			return true
		}
	}
	return false
}

func normalizedRuntimeObjectKey(scope contract.RuntimeConfigScope, appKey, path string) (string, error) {
	if scope != contract.RuntimeConfigScopeWorkspace && scope != contract.RuntimeConfigScopeApp {
		return "", fmt.Errorf("%w: invalid runtime configuration scope", ErrInvalidState)
	}
	if scope == contract.RuntimeConfigScopeWorkspace {
		appKey = ""
	} else if strings.TrimSpace(appKey) == "" {
		return "", fmt.Errorf("%w: app key is required", ErrInvalidState)
	}
	path, err := contract.NormalizeRuntimeConfigPath(path)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidState, err)
	}
	return runtimeConfigObjectKey(scope, appKey, path), nil
}

func runtimeConfigAttemptWriteCount(snapshot *Snapshot, workspaceID, jobID string, attempt int) int {
	count := 0
	for _, operation := range snapshot.RuntimeConfigOperations {
		if operation.WorkspaceID == workspaceID && operation.JobID == jobID && operation.Attempt == attempt {
			count++
		}
	}
	return count
}

func recordRuntimeConfigSuccess(snapshot *Snapshot, now time.Time, operation RuntimeConfigOperation, audit RuntimeConfigAudit) {
	operation.CreatedAt = now.UTC()
	snapshot.RuntimeConfigOperations[runtimeConfigOperationKey(operation.WorkspaceID, operation.JobID, operation.Attempt, operation.OperationID)] = operation
	audit.ID = "runtime-config:" + operation.JobID + ":" + fmt.Sprint(operation.Attempt) + ":" + operation.OperationID
	audit.CreatedAt = now.UTC()
	snapshot.RuntimeConfigAudits[operation.WorkspaceID] = append(snapshot.RuntimeConfigAudits[operation.WorkspaceID], audit)
}

func runtimeMutationActor(actor, jobID string) string {
	if actor = strings.TrimSpace(actor); actor != "" {
		return actor
	}
	return "job:" + jobID
}

func normalizeLocalRuntimeConfiguration(snapshot *Snapshot) {
	for workspaceID, variables := range snapshot.Variables {
		normalized := make(map[string]Variable, len(variables))
		for _, variable := range variables {
			if variable.OwnerScope != contract.RuntimeConfigScopeApp {
				variable.OwnerScope = contract.RuntimeConfigScopeWorkspace
				if strings.TrimSpace(variable.AppKey) != "" {
					variable.OwnerScope = contract.RuntimeConfigScopeApp
				}
			}
			if variable.OwnerScope == contract.RuntimeConfigScopeWorkspace {
				variable.AppKey = ""
			}
			if variable.Revision <= 0 {
				variable.Revision = 1
			}
			normalized[runtimeConfigObjectKey(variable.OwnerScope, variable.AppKey, variable.Path)] = variable
		}
		snapshot.Variables[workspaceID] = normalized
	}
	for workspaceID, resources := range snapshot.Resources {
		normalized := make(map[string]Resource, len(resources))
		for _, resource := range resources {
			if resource.OwnerScope != contract.RuntimeConfigScopeApp {
				resource.OwnerScope = contract.RuntimeConfigScopeWorkspace
				resource.AppKey = ""
			}
			if resource.Revision <= 0 {
				resource.Revision = 1
			}
			normalized[runtimeConfigObjectKey(resource.OwnerScope, resource.AppKey, resource.Path)] = resource
		}
		snapshot.Resources[workspaceID] = normalized
	}
}
