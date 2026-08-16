package runtimeconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/secretbackend"
	"github.com/imprun/windforce-core/internal/state"
)

const (
	defaultMaxDepth      = 16
	defaultMaxReferences = 256
	defaultMaxBytes      = 1 << 20
)

type Store interface {
	GetVariableScoped(ctx context.Context, workspaceID string, scope contract.RuntimeConfigScope, appKey string, path string) (state.Variable, bool, error)
	GetResourceScoped(ctx context.Context, workspaceID string, scope contract.RuntimeConfigScope, appKey string, path string) (state.Resource, bool, error)
}

type SecretAccessRecorder interface {
	AppendSecretAccessAudit(ctx context.Context, record state.SecretAccessAudit) error
}

type Resolver struct {
	Store         Store
	Secrets       secretbackend.Backend
	Audit         SecretAccessRecorder
	MaxDepth      int
	MaxReferences int
	MaxBytes      int
}

type Resolution struct {
	Value        json.RawMessage
	SecretValues []string
}

func New(store Store, secrets secretbackend.Backend) *Resolver {
	resolver := &Resolver{Store: store, Secrets: secrets}
	if audit, ok := store.(SecretAccessRecorder); ok {
		resolver.Audit = audit
	}
	return resolver
}

// BuildAccess validates all references without resolving secret plaintext. It
// expands Resource references to a closed allowlist so runtime resolution can
// reject paths that were not admitted with the Job.
func (r *Resolver) BuildAccess(
	ctx context.Context,
	workspaceID string,
	appKey string,
	declared contract.RuntimeAccess,
	input json.RawMessage,
) (contract.RuntimeAccess, error) {
	if r == nil || r.Store == nil {
		return contract.RuntimeAccess{}, errors.New("runtime configuration store is required")
	}
	declared, err := contract.NormalizeRuntimeAccess(declared)
	if err != nil {
		return contract.RuntimeAccess{}, err
	}
	legacyVariables := stringSet(declared.Variables)
	legacyResources := stringSet(declared.Resources)
	variableTargets := runtimeTargetSet(declared.VariableTargets)
	resourceTargets := runtimeTargetSet(declared.ResourceTargets)
	referencedLegacyVariables := map[string]struct{}{}
	referencedLegacyResources := map[string]struct{}{}
	referencedVariableTargets := map[string]contract.RuntimeConfigTarget{}
	referencedResourceTargets := map[string]contract.RuntimeConfigTarget{}

	root, err := decode(input)
	if err != nil {
		return contract.RuntimeAccess{}, err
	}
	if err := collectScopedReferences(root, referencedLegacyVariables, referencedLegacyResources, referencedVariableTargets, referencedResourceTargets); err != nil {
		return contract.RuntimeAccess{}, err
	}
	requiredVariables := map[string]bool{}
	requiredResources := map[string]bool{}
	for _, target := range allRuntimeTargets(referencedLegacyVariables, referencedVariableTargets) {
		requiredVariables[runtimeTargetKey(target)] = true
	}
	for _, target := range allRuntimeTargets(referencedLegacyResources, referencedResourceTargets) {
		requiredResources[runtimeTargetKey(target)] = true
	}
	mergeStringSet(legacyVariables, referencedLegacyVariables)
	mergeStringSet(legacyResources, referencedLegacyResources)
	mergeRuntimeTargetSet(variableTargets, referencedVariableTargets)
	mergeRuntimeTargetSet(resourceTargets, referencedResourceTargets)
	if runtimeReferenceCount(legacyVariables, legacyResources, variableTargets, resourceTargets) > r.maxReferences() {
		return contract.RuntimeAccess{}, fmt.Errorf("runtime configuration references exceed limit %d", r.maxReferences())
	}

	visited := map[string]bool{}
	visiting := map[string]bool{}
	var visitResource func(contract.RuntimeConfigTarget, int) error
	visitResource = func(target contract.RuntimeConfigTarget, depth int) error {
		if depth > r.maxDepth() {
			return fmt.Errorf("resource reference depth exceeds limit %d", r.maxDepth())
		}
		key := runtimeTargetKey(target)
		if visiting[key] {
			return fmt.Errorf("resource reference cycle at %q", target.Path)
		}
		if visited[key] {
			return nil
		}
		visiting[key] = true
		resource, found, err := r.Store.GetResourceScoped(ctx, workspaceID, target.Scope, appKey, target.Path)
		if err != nil {
			return fmt.Errorf("read resource %q: %w", target.Path, err)
		}
		if !found {
			delete(visiting, key)
			visited[key] = true
			if !requiredResources[key] {
				return nil
			}
			return fmt.Errorf("resource %q: %w", target.Path, state.ErrNotFound)
		}
		// App-owned Resources are data, not capability bearers. Their nested
		// references must already be present in the pinned read closure and do
		// not expand it here.
		if target.Scope == contract.RuntimeConfigScopeApp {
			delete(visiting, key)
			visited[key] = true
			return nil
		}
		value, err := decode(resource.Value)
		if err != nil {
			return fmt.Errorf("decode resource %q: %w", target.Path, err)
		}
		nestedLegacyVariables := map[string]struct{}{}
		nestedLegacyResources := map[string]struct{}{}
		nestedVariableTargets := map[string]contract.RuntimeConfigTarget{}
		nestedResourceTargets := map[string]contract.RuntimeConfigTarget{}
		if err := collectScopedReferences(value, nestedLegacyVariables, nestedLegacyResources, nestedVariableTargets, nestedResourceTargets); err != nil {
			return fmt.Errorf("resource %q: %w", target.Path, err)
		}
		for _, nested := range allRuntimeTargets(nestedLegacyVariables, nestedVariableTargets) {
			requiredVariables[runtimeTargetKey(nested)] = true
		}
		for _, nested := range allRuntimeTargets(nestedLegacyResources, nestedResourceTargets) {
			requiredResources[runtimeTargetKey(nested)] = true
		}
		for _, nested := range allRuntimeTargets(nestedLegacyResources, nestedResourceTargets) {
			if visiting[runtimeTargetKey(nested)] {
				return fmt.Errorf("resource reference cycle at %q", nested.Path)
			}
		}
		mergeStringSet(legacyVariables, nestedLegacyVariables)
		mergeStringSet(legacyResources, nestedLegacyResources)
		mergeRuntimeTargetSet(variableTargets, nestedVariableTargets)
		mergeRuntimeTargetSet(resourceTargets, nestedResourceTargets)
		if runtimeReferenceCount(legacyVariables, legacyResources, variableTargets, resourceTargets) > r.maxReferences() {
			return fmt.Errorf("runtime configuration references exceed limit %d", r.maxReferences())
		}
		for _, nested := range allRuntimeTargets(legacyResources, resourceTargets) {
			if !visited[runtimeTargetKey(nested)] && !visiting[runtimeTargetKey(nested)] {
				if err := visitResource(nested, depth+1); err != nil {
					return err
				}
			}
		}
		delete(visiting, key)
		visited[key] = true
		return nil
	}
	for _, target := range allRuntimeTargets(legacyResources, resourceTargets) {
		if err := visitResource(target, 1); err != nil {
			return contract.RuntimeAccess{}, err
		}
	}
	for _, target := range allRuntimeTargets(legacyVariables, variableTargets) {
		if _, found, err := r.Store.GetVariableScoped(ctx, workspaceID, target.Scope, appKey, target.Path); err != nil {
			return contract.RuntimeAccess{}, fmt.Errorf("read variable %q: %w", target.Path, err)
		} else if !found && requiredVariables[runtimeTargetKey(target)] {
			return contract.RuntimeAccess{}, fmt.Errorf("variable %q: %w", target.Path, state.ErrNotFound)
		}
	}
	return contract.NormalizeRuntimeAccess(contract.RuntimeAccess{
		Variables:       sortedSet(legacyVariables),
		Resources:       sortedSet(legacyResources),
		VariableTargets: sortedRuntimeTargets(variableTargets),
		ResourceTargets: sortedRuntimeTargets(resourceTargets),
		WriteVariables:  append([]contract.RuntimeVariableWriteTarget(nil), declared.WriteVariables...),
		WriteResources:  append([]contract.RuntimeConfigTarget(nil), declared.WriteResources...),
	})
}

func (r *Resolver) ResolveInput(
	ctx context.Context,
	workspaceID string,
	appKey string,
	access contract.RuntimeAccess,
	input json.RawMessage,
) (Resolution, error) {
	root, err := decode(input)
	if err != nil {
		return Resolution{}, err
	}
	variableTargets, resourceTargets := runtimeAccessTargetSets(access)
	resolutionState := &resolutionState{
		resolver:        r,
		ctx:             ctx,
		workspaceID:     workspaceID,
		appKey:          appKey,
		variableTargets: variableTargets,
		resourceTargets: resourceTargets,
		resourceStack:   map[string]bool{},
		secretValueSet:  map[string]struct{}{},
	}
	resolved, err := resolutionState.resolve(root, 0)
	if err != nil {
		return Resolution{}, err
	}
	value, err := json.Marshal(resolved)
	if err != nil {
		return Resolution{}, fmt.Errorf("encode resolved runtime input: %w", err)
	}
	if len(value) > r.maxBytes() {
		return Resolution{}, fmt.Errorf("resolved runtime input exceeds limit %d bytes", r.maxBytes())
	}
	secretValues := make([]string, 0, len(resolutionState.secretValueSet))
	for secret := range resolutionState.secretValueSet {
		secretValues = append(secretValues, secret)
	}
	sort.Slice(secretValues, func(i, j int) bool {
		if len(secretValues[i]) == len(secretValues[j]) {
			return secretValues[i] < secretValues[j]
		}
		return len(secretValues[i]) > len(secretValues[j])
	})
	return Resolution{Value: value, SecretValues: secretValues}, nil
}

// ResolveRuntimeInput implements the worker execution-time resolver contract.
// The Job contains only Admission-pinned paths; values are fetched now.
func (r *Resolver) ResolveRuntimeInput(
	ctx context.Context,
	job state.Job,
	input json.RawMessage,
) (json.RawMessage, []string, error) {
	if err := r.authorizeJobRuntimeRead(ctx, job); err != nil {
		return nil, nil, err
	}
	workspaceID := contract.NormalizeWorkspace(job.Payload.Workspace)
	resolved, err := r.ResolveInput(
		withSecretAccessAudit(ctx, job, "input"),
		workspaceID,
		job.Payload.App,
		job.Payload.RuntimeAccess,
		input,
	)
	if err != nil {
		return nil, nil, err
	}
	secretSet := map[string]struct{}{}
	for _, value := range resolved.SecretValues {
		if value != "" {
			secretSet[value] = struct{}{}
		}
	}
	preparedContext := withSecretAccessAudit(ctx, job, "redaction")
	for _, path := range job.Payload.RuntimeAccess.Variables {
		value, secret, resolveErr := r.ResolveVariable(
			preparedContext,
			workspaceID,
			job.Payload.App,
			job.Payload.RuntimeAccess,
			path,
		)
		if resolveErr != nil {
			if errors.Is(resolveErr, state.ErrNotFound) {
				continue
			}
			return nil, nil, fmt.Errorf("prepare secret redaction for variable %q: %w", path, resolveErr)
		}
		if secret && value != "" {
			secretSet[value] = struct{}{}
		}
	}
	for _, target := range job.Payload.RuntimeAccess.VariableTargets {
		value, secret, resolveErr := r.ResolveVariableScoped(
			preparedContext,
			workspaceID,
			job.Payload.App,
			job.Payload.RuntimeAccess,
			target.Scope,
			target.Path,
		)
		if resolveErr != nil {
			if errors.Is(resolveErr, state.ErrNotFound) {
				continue
			}
			return nil, nil, fmt.Errorf("prepare secret redaction for %s variable %q: %w", target.Scope, target.Path, resolveErr)
		}
		if secret && value != "" {
			secretSet[value] = struct{}{}
		}
	}
	return resolved.Value, sortedSecrets(secretSet), nil
}

func (r *Resolver) ResolveJobVariable(ctx context.Context, job state.Job, path string) (string, bool, error) {
	ctx = withSecretAccessAudit(ctx, job, "sdk")
	return r.ResolveVariable(
		ctx,
		contract.NormalizeWorkspace(job.Payload.Workspace),
		job.Payload.App,
		job.Payload.RuntimeAccess,
		path,
	)
}

func (r *Resolver) ResolveJobVariableScoped(ctx context.Context, job state.Job, scope contract.RuntimeConfigScope, path string) (string, bool, error) {
	if err := r.authorizeJobRuntimeRead(ctx, job); err != nil {
		return "", false, err
	}
	ctx = withSecretAccessAudit(ctx, job, "sdk")
	return r.ResolveVariableScoped(
		ctx,
		contract.NormalizeWorkspace(job.Payload.Workspace),
		job.Payload.App,
		job.Payload.RuntimeAccess,
		scope,
		path,
	)
}

func (r *Resolver) ResolveJobResource(ctx context.Context, job state.Job, path string) (Resolution, error) {
	ctx = withSecretAccessAudit(ctx, job, "sdk")
	return r.ResolveResource(
		ctx,
		contract.NormalizeWorkspace(job.Payload.Workspace),
		job.Payload.App,
		job.Payload.RuntimeAccess,
		path,
	)
}

func (r *Resolver) ResolveJobResourceScoped(ctx context.Context, job state.Job, scope contract.RuntimeConfigScope, path string) (Resolution, error) {
	if err := r.authorizeJobRuntimeRead(ctx, job); err != nil {
		return Resolution{}, err
	}
	ctx = withSecretAccessAudit(ctx, job, "sdk")
	return r.ResolveResourceScoped(
		ctx,
		contract.NormalizeWorkspace(job.Payload.Workspace),
		job.Payload.App,
		job.Payload.RuntimeAccess,
		scope,
		path,
	)
}

type appRuntimeLifecycleStore interface {
	GetAppRuntimeLifecycle(context.Context, string, string) (state.AppRuntimeLifecycle, error)
}

func (r *Resolver) authorizeJobRuntimeRead(ctx context.Context, job state.Job) error {
	store, ok := r.Store.(appRuntimeLifecycleStore)
	if !ok {
		return nil
	}
	lifecycle, err := store.GetAppRuntimeLifecycle(ctx, job.Payload.Workspace, job.Payload.App)
	if err != nil {
		return err
	}
	if lifecycle.State == state.AppRuntimeRevoked || lifecycle.State == state.AppRuntimeTombstoned && (job.StartedAt == nil || job.StartedAt.After(lifecycle.UpdatedAt)) {
		return fmt.Errorf("App runtime access is not active for this attempt: %w", state.ErrForbidden)
	}
	return nil
}

func (r *Resolver) ResolveVariable(
	ctx context.Context,
	workspaceID string,
	appKey string,
	access contract.RuntimeAccess,
	path string,
) (string, bool, error) {
	return r.ResolveVariableScoped(ctx, workspaceID, appKey, access, contract.RuntimeConfigScopeWorkspace, path)
}

func (r *Resolver) ResolveVariableScoped(
	ctx context.Context,
	workspaceID string,
	appKey string,
	access contract.RuntimeAccess,
	scope contract.RuntimeConfigScope,
	path string,
) (string, bool, error) {
	normalizedPath, err := contract.NormalizeRuntimeConfigPath(path)
	if err != nil {
		return "", false, err
	}
	target := contract.RuntimeConfigTarget{Scope: scope, Path: normalizedPath}
	variableTargets, _ := runtimeAccessTargetSets(access)
	if _, allowed := variableTargets[runtimeTargetKey(target)]; !allowed {
		return "", false, state.ErrForbidden
	}
	variable, found, err := r.Store.GetVariableScoped(ctx, workspaceID, scope, appKey, normalizedPath)
	if err != nil {
		return "", false, err
	}
	if !found {
		return "", false, state.ErrNotFound
	}
	if !variable.IsSecret {
		return variable.Value, false, nil
	}
	if r.Secrets == nil {
		return "", true, errors.New("secret backend is required")
	}
	reference := secretbackend.Reference{
		WorkspaceID: workspaceID,
		Kind:        "variable",
		Path:        variable.Path,
	}
	if scope == contract.RuntimeConfigScopeApp {
		reference.Kind = "variable-app"
		reference.Path = strings.TrimSpace(appKey) + "/" + variable.Path
	}
	reference, stored, err := secretbackend.OpenRuntimeCandidate(reference, variable.Value)
	if err != nil {
		return "", true, err
	}
	plaintext, err := r.Secrets.Resolve(ctx, reference, stored)
	if err == nil {
		err = r.recordSecretAccess(ctx, variable.Path)
	}
	return plaintext, true, err
}

func (r *Resolver) ResolveResource(
	ctx context.Context,
	workspaceID string,
	appKey string,
	access contract.RuntimeAccess,
	path string,
) (Resolution, error) {
	return r.ResolveResourceScoped(ctx, workspaceID, appKey, access, contract.RuntimeConfigScopeWorkspace, path)
}

func (r *Resolver) ResolveResourceScoped(
	ctx context.Context,
	workspaceID string,
	appKey string,
	access contract.RuntimeAccess,
	scope contract.RuntimeConfigScope,
	path string,
) (Resolution, error) {
	normalizedPath, err := contract.NormalizeRuntimeConfigPath(path)
	if err != nil {
		return Resolution{}, err
	}
	target := contract.RuntimeConfigTarget{Scope: scope, Path: normalizedPath}
	variableTargets, resourceTargets := runtimeAccessTargetSets(access)
	if _, allowed := resourceTargets[runtimeTargetKey(target)]; !allowed {
		return Resolution{}, state.ErrForbidden
	}
	resolutionState := &resolutionState{
		resolver:        r,
		ctx:             ctx,
		workspaceID:     workspaceID,
		appKey:          appKey,
		variableTargets: variableTargets,
		resourceTargets: resourceTargets,
		resourceStack:   map[string]bool{},
		secretValueSet:  map[string]struct{}{},
	}
	value, err := resolutionState.resolveResource(target, 1)
	if err != nil {
		return Resolution{}, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return Resolution{}, err
	}
	if len(encoded) > r.maxBytes() {
		return Resolution{}, fmt.Errorf("resolved resource exceeds limit %d bytes", r.maxBytes())
	}
	secrets := make([]string, 0, len(resolutionState.secretValueSet))
	for secret := range resolutionState.secretValueSet {
		secrets = append(secrets, secret)
	}
	return Resolution{Value: encoded, SecretValues: secrets}, nil
}

func runtimeTargetKey(target contract.RuntimeConfigTarget) string {
	return string(target.Scope) + "\x00" + target.Path
}

func runtimeTargetSet(values []contract.RuntimeConfigTarget) map[string]contract.RuntimeConfigTarget {
	result := make(map[string]contract.RuntimeConfigTarget, len(values))
	for _, target := range values {
		result[runtimeTargetKey(target)] = target
	}
	return result
}

func mergeStringSet(target, source map[string]struct{}) {
	for value := range source {
		target[value] = struct{}{}
	}
}

func mergeRuntimeTargetSet(target, source map[string]contract.RuntimeConfigTarget) {
	for key, value := range source {
		target[key] = value
	}
}

func addRuntimeReference(
	reference contract.RuntimeConfigReference,
	legacyVariables map[string]struct{},
	legacyResources map[string]struct{},
	variableTargets map[string]contract.RuntimeConfigTarget,
	resourceTargets map[string]contract.RuntimeConfigTarget,
) {
	legacy := legacyVariables
	targets := variableTargets
	if reference.Kind == "res" {
		legacy = legacyResources
		targets = resourceTargets
	}
	if reference.Scope == contract.RuntimeConfigScopeWorkspace && !reference.Explicit {
		legacy[reference.Path] = struct{}{}
		delete(targets, runtimeTargetKey(contract.RuntimeConfigTarget{Scope: reference.Scope, Path: reference.Path}))
		return
	}
	if reference.Scope == contract.RuntimeConfigScopeWorkspace {
		if _, found := legacy[reference.Path]; found {
			return
		}
	}
	target := contract.RuntimeConfigTarget{Scope: reference.Scope, Path: reference.Path}
	targets[runtimeTargetKey(target)] = target
}

func collectScopedReferences(
	value any,
	legacyVariables map[string]struct{},
	legacyResources map[string]struct{},
	variableTargets map[string]contract.RuntimeConfigTarget,
	resourceTargets map[string]contract.RuntimeConfigTarget,
) error {
	switch typed := value.(type) {
	case string:
		reference, ok, err := contract.ParseRuntimeConfigReference(typed)
		if err != nil {
			return err
		}
		if ok {
			addRuntimeReference(reference, legacyVariables, legacyResources, variableTargets, resourceTargets)
		}
	case []any:
		for _, item := range typed {
			if err := collectScopedReferences(item, legacyVariables, legacyResources, variableTargets, resourceTargets); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, item := range typed {
			if err := collectScopedReferences(item, legacyVariables, legacyResources, variableTargets, resourceTargets); err != nil {
				return err
			}
		}
	}
	return nil
}

func runtimeReferenceCount(
	legacyVariables map[string]struct{},
	legacyResources map[string]struct{},
	variableTargets map[string]contract.RuntimeConfigTarget,
	resourceTargets map[string]contract.RuntimeConfigTarget,
) int {
	return len(legacyVariables) + len(legacyResources) + len(variableTargets) + len(resourceTargets)
}

func allRuntimeTargets(legacy map[string]struct{}, scoped map[string]contract.RuntimeConfigTarget) []contract.RuntimeConfigTarget {
	result := make([]contract.RuntimeConfigTarget, 0, len(legacy)+len(scoped))
	for path := range legacy {
		result = append(result, contract.RuntimeConfigTarget{Scope: contract.RuntimeConfigScopeWorkspace, Path: path})
	}
	for _, target := range scoped {
		result = append(result, target)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Scope == result[j].Scope {
			return result[i].Path < result[j].Path
		}
		return result[i].Scope < result[j].Scope
	})
	return result
}

func sortedRuntimeTargets(values map[string]contract.RuntimeConfigTarget) []contract.RuntimeConfigTarget {
	return allRuntimeTargets(nil, values)
}

func runtimeAccessTargetSets(access contract.RuntimeAccess) (map[string]contract.RuntimeConfigTarget, map[string]contract.RuntimeConfigTarget) {
	variables := runtimeTargetSet(access.VariableTargets)
	resources := runtimeTargetSet(access.ResourceTargets)
	for _, path := range access.Variables {
		target := contract.RuntimeConfigTarget{Scope: contract.RuntimeConfigScopeWorkspace, Path: path}
		variables[runtimeTargetKey(target)] = target
	}
	for _, path := range access.Resources {
		target := contract.RuntimeConfigTarget{Scope: contract.RuntimeConfigScopeWorkspace, Path: path}
		resources[runtimeTargetKey(target)] = target
	}
	return variables, resources
}

func runtimeAccessFromTargetSets(variables, resources map[string]contract.RuntimeConfigTarget) contract.RuntimeAccess {
	return contract.RuntimeAccess{
		VariableTargets: sortedRuntimeTargets(variables),
		ResourceTargets: sortedRuntimeTargets(resources),
	}
}

type resolutionState struct {
	resolver        *Resolver
	ctx             context.Context
	workspaceID     string
	appKey          string
	variableTargets map[string]contract.RuntimeConfigTarget
	resourceTargets map[string]contract.RuntimeConfigTarget
	resourceStack   map[string]bool
	secretValueSet  map[string]struct{}
}

func (s *resolutionState) resolve(value any, depth int) (any, error) {
	if depth > s.resolver.maxDepth() {
		return nil, fmt.Errorf("runtime configuration depth exceeds limit %d", s.resolver.maxDepth())
	}
	switch typed := value.(type) {
	case string:
		reference, ok, err := contract.ParseRuntimeConfigReference(typed)
		if err != nil {
			return nil, err
		}
		if !ok {
			return typed, nil
		}
		target := contract.RuntimeConfigTarget{Scope: reference.Scope, Path: reference.Path}
		switch reference.Kind {
		case "var":
			if _, allowed := s.variableTargets[runtimeTargetKey(target)]; !allowed {
				return nil, fmt.Errorf("variable %q: %w", reference.Path, state.ErrForbidden)
			}
			value, secret, err := s.resolver.ResolveVariableScoped(
				s.ctx,
				s.workspaceID,
				s.appKey,
				runtimeAccessFromTargetSets(s.variableTargets, s.resourceTargets),
				reference.Scope,
				reference.Path,
			)
			if err != nil {
				return nil, fmt.Errorf("resolve variable %q: %w", reference.Path, err)
			}
			if secret && value != "" {
				s.secretValueSet[value] = struct{}{}
			}
			return value, nil
		case "res":
			if _, allowed := s.resourceTargets[runtimeTargetKey(target)]; !allowed {
				return nil, fmt.Errorf("resource %q: %w", reference.Path, state.ErrForbidden)
			}
			return s.resolveResource(target, depth+1)
		}
	case []any:
		for index := range typed {
			resolved, err := s.resolve(typed[index], depth+1)
			if err != nil {
				return nil, err
			}
			typed[index] = resolved
		}
		return typed, nil
	case map[string]any:
		for key, item := range typed {
			resolved, err := s.resolve(item, depth+1)
			if err != nil {
				return nil, err
			}
			typed[key] = resolved
		}
		return typed, nil
	}
	return value, nil
}

func (s *resolutionState) resolveResource(target contract.RuntimeConfigTarget, depth int) (any, error) {
	if depth > s.resolver.maxDepth() {
		return nil, fmt.Errorf("resource reference depth exceeds limit %d", s.resolver.maxDepth())
	}
	key := runtimeTargetKey(target)
	if s.resourceStack[key] {
		return nil, fmt.Errorf("resource reference cycle at %q", target.Path)
	}
	resource, found, err := s.resolver.Store.GetResourceScoped(s.ctx, s.workspaceID, target.Scope, s.appKey, target.Path)
	if err != nil {
		return nil, fmt.Errorf("read resource %q: %w", target.Path, err)
	}
	if !found {
		return nil, fmt.Errorf("resource %q: %w", target.Path, state.ErrNotFound)
	}
	value, err := decode(resource.Value)
	if err != nil {
		return nil, fmt.Errorf("decode resource %q: %w", target.Path, err)
	}
	s.resourceStack[key] = true
	resolved, err := s.resolve(value, depth)
	delete(s.resourceStack, key)
	return resolved, err
}

func decode(raw json.RawMessage) (any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`null`)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode runtime configuration: %w", err)
	}
	return value, nil
}

func collectReferences(value any, variables map[string]struct{}, resources map[string]struct{}) error {
	switch typed := value.(type) {
	case string:
		kind, path, ok, err := parseReference(typed)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if kind == "var" {
			variables[path] = struct{}{}
		} else {
			resources[path] = struct{}{}
		}
	case []any:
		for _, item := range typed {
			if err := collectReferences(item, variables, resources); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, item := range typed {
			if err := collectReferences(item, variables, resources); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseReference(value string) (kind string, path string, ok bool, err error) {
	reference, ok, err := contract.ParseRuntimeConfigReference(value)
	if err != nil || !ok {
		return "", "", ok, err
	}
	return reference.Kind, reference.Path, true, nil
}

func stringSet(values []string) map[string]struct{} {
	result := map[string]struct{}{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}

func sortedSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func sortedSecrets(values map[string]struct{}) []string {
	result := sortedSet(values)
	sort.Slice(result, func(i, j int) bool {
		if len(result[i]) == len(result[j]) {
			return result[i] < result[j]
		}
		return len(result[i]) > len(result[j])
	})
	return result
}

func contains(values []string, candidate string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == candidate {
			return true
		}
	}
	return false
}

func (r *Resolver) maxDepth() int {
	if r != nil && r.MaxDepth > 0 {
		return r.MaxDepth
	}
	return defaultMaxDepth
}

func (r *Resolver) maxReferences() int {
	if r != nil && r.MaxReferences > 0 {
		return r.MaxReferences
	}
	return defaultMaxReferences
}

func (r *Resolver) maxBytes() int {
	if r != nil && r.MaxBytes > 0 {
		return r.MaxBytes
	}
	return defaultMaxBytes
}

type secretAccessAuditContext struct {
	job    state.Job
	source string
}

type secretAccessAuditContextKey struct{}

func withSecretAccessAudit(ctx context.Context, job state.Job, source string) context.Context {
	return context.WithValue(ctx, secretAccessAuditContextKey{}, secretAccessAuditContext{
		job:    job,
		source: source,
	})
}

func (r *Resolver) recordSecretAccess(ctx context.Context, path string) error {
	if r == nil || r.Audit == nil {
		return nil
	}
	auditContext, ok := ctx.Value(secretAccessAuditContextKey{}).(secretAccessAuditContext)
	if !ok {
		return nil
	}
	job := auditContext.job
	return r.Audit.AppendSecretAccessAudit(ctx, state.SecretAccessAudit{
		WorkspaceID: contract.NormalizeWorkspace(job.Payload.Workspace),
		JobID:       job.ID,
		Attempt:     job.Attempt,
		AppKey:      job.Payload.App,
		ActionKey:   job.Payload.Action,
		Path:        path,
		Source:      auditContext.source,
	})
}
