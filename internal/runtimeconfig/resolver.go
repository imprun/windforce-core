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
	GetVariable(ctx context.Context, workspaceID string, appKey string, path string) (state.Variable, bool, error)
	GetResource(ctx context.Context, workspaceID string, path string) (state.Resource, bool, error)
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
	variablePaths := stringSet(declared.Variables)
	resourcePaths := stringSet(declared.Resources)
	root, err := decode(input)
	if err != nil {
		return contract.RuntimeAccess{}, err
	}
	if err := collectReferences(root, variablePaths, resourcePaths); err != nil {
		return contract.RuntimeAccess{}, err
	}
	if len(variablePaths)+len(resourcePaths) > r.maxReferences() {
		return contract.RuntimeAccess{}, fmt.Errorf("runtime configuration references exceed limit %d", r.maxReferences())
	}

	visited := map[string]bool{}
	visiting := map[string]bool{}
	var visitResource func(string, int) error
	visitResource = func(path string, depth int) error {
		if depth > r.maxDepth() {
			return fmt.Errorf("resource reference depth exceeds limit %d", r.maxDepth())
		}
		if visiting[path] {
			return fmt.Errorf("resource reference cycle at %q", path)
		}
		if visited[path] {
			return nil
		}
		visiting[path] = true
		resource, found, err := r.Store.GetResource(ctx, workspaceID, path)
		if err != nil {
			return fmt.Errorf("read resource %q: %w", path, err)
		}
		if !found {
			return fmt.Errorf("resource %q: %w", path, state.ErrNotFound)
		}
		value, err := decode(resource.Value)
		if err != nil {
			return fmt.Errorf("decode resource %q: %w", path, err)
		}
		nestedVariables := map[string]struct{}{}
		nestedResources := map[string]struct{}{}
		if err := collectReferences(value, nestedVariables, nestedResources); err != nil {
			return fmt.Errorf("resource %q: %w", path, err)
		}
		for nested := range nestedVariables {
			variablePaths[nested] = struct{}{}
		}
		for nested := range nestedResources {
			resourcePaths[nested] = struct{}{}
		}
		if len(variablePaths)+len(resourcePaths) > r.maxReferences() {
			return fmt.Errorf("runtime configuration references exceed limit %d", r.maxReferences())
		}
		for nested := range nestedResources {
			if err := visitResource(nested, depth+1); err != nil {
				return err
			}
		}
		delete(visiting, path)
		visited[path] = true
		return nil
	}
	for path := range resourcePaths {
		if err := visitResource(path, 1); err != nil {
			return contract.RuntimeAccess{}, err
		}
	}
	for path := range variablePaths {
		if _, found, err := r.Store.GetVariable(ctx, workspaceID, appKey, path); err != nil {
			return contract.RuntimeAccess{}, fmt.Errorf("read variable %q: %w", path, err)
		} else if !found {
			return contract.RuntimeAccess{}, fmt.Errorf("variable %q: %w", path, state.ErrNotFound)
		}
	}
	return contract.NormalizeRuntimeAccess(contract.RuntimeAccess{
		Variables: sortedSet(variablePaths),
		Resources: sortedSet(resourcePaths),
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
	resolutionState := &resolutionState{
		resolver:       r,
		ctx:            ctx,
		workspaceID:    workspaceID,
		appKey:         appKey,
		variablePaths:  stringSet(access.Variables),
		resourcePaths:  stringSet(access.Resources),
		resourceStack:  map[string]bool{},
		secretValueSet: map[string]struct{}{},
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
			return nil, nil, fmt.Errorf("prepare secret redaction for variable %q: %w", path, resolveErr)
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

func (r *Resolver) ResolveVariable(
	ctx context.Context,
	workspaceID string,
	appKey string,
	access contract.RuntimeAccess,
	path string,
) (string, bool, error) {
	if !contains(access.Variables, path) {
		return "", false, state.ErrForbidden
	}
	variable, found, err := r.Store.GetVariable(ctx, workspaceID, appKey, path)
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
	plaintext, err := r.Secrets.Resolve(ctx, secretbackend.Reference{
		WorkspaceID: workspaceID,
		Kind:        "variable",
		Path:        variable.Path,
	}, variable.Value)
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
	if !contains(access.Resources, path) {
		return Resolution{}, state.ErrForbidden
	}
	state := &resolutionState{
		resolver:       r,
		ctx:            ctx,
		workspaceID:    workspaceID,
		appKey:         appKey,
		variablePaths:  stringSet(access.Variables),
		resourcePaths:  stringSet(access.Resources),
		resourceStack:  map[string]bool{},
		secretValueSet: map[string]struct{}{},
	}
	value, err := state.resolveResource(path, 1)
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
	secrets := make([]string, 0, len(state.secretValueSet))
	for secret := range state.secretValueSet {
		secrets = append(secrets, secret)
	}
	return Resolution{Value: encoded, SecretValues: secrets}, nil
}

type resolutionState struct {
	resolver       *Resolver
	ctx            context.Context
	workspaceID    string
	appKey         string
	variablePaths  map[string]struct{}
	resourcePaths  map[string]struct{}
	resourceStack  map[string]bool
	secretValueSet map[string]struct{}
}

func (s *resolutionState) resolve(value any, depth int) (any, error) {
	if depth > s.resolver.maxDepth() {
		return nil, fmt.Errorf("runtime configuration depth exceeds limit %d", s.resolver.maxDepth())
	}
	switch typed := value.(type) {
	case string:
		kind, path, reference, err := parseReference(typed)
		if err != nil {
			return nil, err
		}
		if !reference {
			return typed, nil
		}
		switch kind {
		case "var":
			if _, ok := s.variablePaths[path]; !ok {
				return nil, fmt.Errorf("variable %q: %w", path, state.ErrForbidden)
			}
			value, secret, err := s.resolver.ResolveVariable(
				s.ctx,
				s.workspaceID,
				s.appKey,
				contract.RuntimeAccess{Variables: sortedSet(s.variablePaths)},
				path,
			)
			if err != nil {
				return nil, fmt.Errorf("resolve variable %q: %w", path, err)
			}
			if secret && value != "" {
				s.secretValueSet[value] = struct{}{}
			}
			return value, nil
		case "res":
			if _, ok := s.resourcePaths[path]; !ok {
				return nil, fmt.Errorf("resource %q: %w", path, state.ErrForbidden)
			}
			return s.resolveResource(path, depth+1)
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

func (s *resolutionState) resolveResource(path string, depth int) (any, error) {
	if depth > s.resolver.maxDepth() {
		return nil, fmt.Errorf("resource reference depth exceeds limit %d", s.resolver.maxDepth())
	}
	if s.resourceStack[path] {
		return nil, fmt.Errorf("resource reference cycle at %q", path)
	}
	resource, found, err := s.resolver.Store.GetResource(s.ctx, s.workspaceID, path)
	if err != nil {
		return nil, fmt.Errorf("read resource %q: %w", path, err)
	}
	if !found {
		return nil, fmt.Errorf("resource %q: %w", path, state.ErrNotFound)
	}
	value, err := decode(resource.Value)
	if err != nil {
		return nil, fmt.Errorf("decode resource %q: %w", path, err)
	}
	s.resourceStack[path] = true
	resolved, err := s.resolve(value, depth)
	delete(s.resourceStack, path)
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
	for _, prefix := range []string{"$var:", "$res:"} {
		if strings.HasPrefix(value, prefix) {
			path = strings.TrimSpace(strings.TrimPrefix(value, prefix))
			if path == "" {
				return "", "", true, errors.New("runtime configuration reference path is required")
			}
			normalized, normalizeErr := contract.NormalizeRuntimeConfigPath(path)
			if normalizeErr != nil {
				return "", "", true, normalizeErr
			}
			return strings.TrimSuffix(strings.TrimPrefix(prefix, "$"), ":"), normalized, true, nil
		}
	}
	return "", "", false, nil
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
