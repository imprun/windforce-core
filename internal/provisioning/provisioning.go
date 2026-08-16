package provisioning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/executionlimit"
	"github.com/imprun/windforce-core/internal/gitsource"
	"github.com/imprun/windforce-core/internal/state"
	"gopkg.in/yaml.v3"
)

const APIVersion = "windforce-lite.imprun.dev/v1"

type Document struct {
	APIVersion string   `json:"apiVersion" yaml:"apiVersion"`
	Kind       string   `json:"kind" yaml:"kind"`
	Metadata   Metadata `json:"metadata" yaml:"metadata"`
	Spec       Spec     `json:"spec" yaml:"spec"`
	SourcePath string   `json:"-" yaml:"-"`
}

type Metadata struct {
	Name string            `json:"name" yaml:"name"`
	Tags map[string]string `json:"tags,omitempty" yaml:"tags,omitempty"`
}

type Spec struct {
	Name             string                `json:"name,omitempty" yaml:"name,omitempty"`
	AppKey           string                `json:"appKey,omitempty" yaml:"appKey,omitempty"`
	ActionKey        string                `json:"actionKey,omitempty" yaml:"actionKey,omitempty"`
	ClientRef        string                `json:"clientRef,omitempty" yaml:"clientRef,omitempty"`
	ClientKey        ValueSource           `json:"clientKey,omitempty" yaml:"clientKey,omitempty"`
	ExternalKey      ValueSource           `json:"externalKey,omitempty" yaml:"externalKey,omitempty"`
	InvocationPolicy *InvocationPolicySpec `json:"invocationPolicy,omitempty" yaml:"invocationPolicy,omitempty"`
	Scope            string                `json:"scope,omitempty" yaml:"scope,omitempty"`
	PolicyID         string                `json:"policyId,omitempty" yaml:"policyId,omitempty"`
	LimitKind        string                `json:"limitKind,omitempty" yaml:"limitKind,omitempty"`
	ShapeFingerprint string                `json:"shapeFingerprint,omitempty" yaml:"shapeFingerprint,omitempty"`
	Allowance        int32                 `json:"allowance,omitempty" yaml:"allowance,omitempty"`
	WindowSeconds    int32                 `json:"windowSeconds,omitempty" yaml:"windowSeconds,omitempty"`

	Method     string      `json:"method,omitempty" yaml:"method,omitempty"`
	StorageRef string      `json:"storageRef,omitempty" yaml:"storageRef,omitempty"`
	Username   ValueSource `json:"username,omitempty" yaml:"username,omitempty"`
	Password   ValueSource `json:"password,omitempty" yaml:"password,omitempty"`
	Token      ValueSource `json:"token,omitempty" yaml:"token,omitempty"`

	Path          string                `json:"path,omitempty" yaml:"path,omitempty"`
	AppScope      string                `json:"appScope,omitempty" yaml:"appScope,omitempty"`
	Value         ValueSource           `json:"value,omitempty" yaml:"value,omitempty"`
	Secret        bool                  `json:"secret,omitempty" yaml:"secret,omitempty"`
	Description   string                `json:"description,omitempty" yaml:"description,omitempty"`
	ResourceType  string                `json:"resourceType,omitempty" yaml:"resourceType,omitempty"`
	ResourceValue any                   `json:"resourceValue,omitempty" yaml:"resourceValue,omitempty"`
	Revision      int64                 `json:"revision,omitempty" yaml:"revision,omitempty"`
	RuntimeState  state.AppRuntimeState `json:"runtimeState,omitempty" yaml:"runtimeState,omitempty"`
	Reason        string                `json:"reason,omitempty" yaml:"reason,omitempty"`

	Repository Repository     `json:"repository,omitempty" yaml:"repository,omitempty"`
	Config     map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
	LockedKeys []string       `json:"lockedKeys,omitempty" yaml:"lockedKeys,omitempty"`
}

type InvocationPolicySpec struct {
	Mode           string   `json:"mode" yaml:"mode"`
	AllowedTargets []string `json:"allowedTargets" yaml:"allowedTargets"`
}

type Repository struct {
	URL           string `json:"url,omitempty" yaml:"url,omitempty"`
	Branch        string `json:"branch,omitempty" yaml:"branch,omitempty"`
	Subpath       string `json:"subpath,omitempty" yaml:"subpath,omitempty"`
	AuthRef       string `json:"authRef,omitempty" yaml:"authRef,omitempty"`
	CredentialRef string `json:"credentialRef,omitempty" yaml:"credentialRef,omitempty"`
}

type ValueSource struct {
	Value     any        `json:"value,omitempty" yaml:"value,omitempty"`
	ValueFrom *ValueFrom `json:"valueFrom,omitempty" yaml:"valueFrom,omitempty"`
	Redacted  bool       `json:"redacted,omitempty" yaml:"redacted,omitempty"`
}

type ValueFrom struct {
	Env  string `json:"env,omitempty" yaml:"env,omitempty"`
	File string `json:"file,omitempty" yaml:"file,omitempty"`
}

type Options struct {
	Workspace string
	Actor     string
	DryRun    bool
}

type Result struct {
	Applied []AppliedResource `json:"applied"`
}

type AppliedResource struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Action string `json:"action"`
	Detail string `json:"detail,omitempty"`
}

type GitSourceRegistry interface {
	Create(context.Context, gitsource.Source) (gitsource.Source, error)
	Get(context.Context, string, string) (gitsource.Source, error)
	Patch(context.Context, string, string, gitsource.Patch) (gitsource.Source, error)
	Load(context.Context) (gitsource.Snapshot, error)
}

type Service struct {
	Store      state.Store
	GitSources GitSourceRegistry
	Encrypt    func(context.Context, string, string) (string, error)
	AppKeys    []string
}

func LoadDir(dir string) ([]Document, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	paths := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext == ".yaml" || ext == ".yml" || ext == ".json" {
			paths = append(paths, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(paths)
	docs := []Document{}
	for _, path := range paths {
		loaded, err := LoadFile(path)
		if err != nil {
			return nil, err
		}
		docs = append(docs, loaded...)
	}
	return docs, nil
}

func LoadFile(path string) ([]Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	docs, err := Decode(data, filepath.Ext(path))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for i := range docs {
		docs[i].SourcePath = path
	}
	return docs, nil
}

func Decode(data []byte, ext string) ([]Document, error) {
	ext = strings.ToLower(ext)
	var docs []Document
	var err error
	if ext == ".json" {
		docs, err = decodeJSON(data)
	} else {
		docs, err = decodeYAML(data)
	}
	if err != nil {
		return nil, err
	}
	for i := range docs {
		if err := normalizeDocument(&docs[i]); err != nil {
			return nil, err
		}
	}
	return docs, nil
}

func (s Service) Apply(ctx context.Context, docs []Document, options Options) (Result, error) {
	if s.Store == nil {
		return Result{}, errors.New("state store is required")
	}
	options.Workspace = contract.NormalizeWorkspace(options.Workspace)
	if options.Actor == "" {
		options.Actor = "provisioning"
	}
	result := Result{}
	credentials := map[string]string{}
	clients := map[string]state.Client{}
	policyResults, err := s.applyExecutionLimitPolicyDocuments(ctx, docs, options)
	if err != nil {
		return result, err
	}
	result.Applied = append(result.Applied, policyResults...)
	runtimeBatch, runtimeResults, err := s.runtimeConfigProvisioningBatch(ctx, docs, options)
	if err != nil {
		return result, err
	}
	if len(runtimeBatch.Variables) != 0 || len(runtimeBatch.Resources) != 0 || len(runtimeBatch.Lifecycles) != 0 {
		if err := s.Store.ApplyRuntimeConfigProvisioningBatch(ctx, runtimeBatch); err != nil {
			return result, err
		}
	}
	result.Applied = append(result.Applied, runtimeResults...)

	for _, doc := range docs {
		if doc.Kind != "GitCredential" {
			continue
		}
		if gitCredentialHasRedactedValue(doc) {
			ref := gitCredentialRef(doc)
			if err := s.requireExistingVariable(ctx, options, "", ref, "redacted credential"); err != nil {
				return result, resourceError(doc, err)
			}
			credentials[doc.Metadata.Name] = ref
			result.Applied = append(result.Applied, AppliedResource{Kind: doc.Kind, Name: doc.Metadata.Name, Action: dryRunAction(options, "unchanged"), Detail: ref})
			continue
		}
		ref, credentialJSON, err := gitCredential(doc)
		if err != nil {
			return result, resourceError(doc, err)
		}
		credentials[doc.Metadata.Name] = ref
		if credentialJSON == "" || options.DryRun {
			result.Applied = append(result.Applied, AppliedResource{Kind: doc.Kind, Name: doc.Metadata.Name, Action: dryRunAction(options, "validated"), Detail: ref})
			continue
		}
		value := credentialJSON
		if s.Encrypt != nil {
			encrypted, err := s.Encrypt(ctx, options.Workspace, credentialJSON)
			if err != nil {
				return result, resourceError(doc, err)
			}
			value = encrypted
		}
		if err := s.Store.SetVariable(ctx, options.Workspace, "", ref, value, true, "Git credential managed by provisioning"); err != nil {
			return result, resourceError(doc, err)
		}
		result.Applied = append(result.Applied, AppliedResource{Kind: doc.Kind, Name: doc.Metadata.Name, Action: "stored", Detail: ref})
	}

	for _, doc := range docs {
		switch doc.Kind {
		case "GitCredential":
			continue
		case "ExecutionLimitPolicy":
			continue
		case "Client":
			client, action, err := s.applyClient(ctx, doc, options)
			if err != nil {
				return result, resourceError(doc, err)
			}
			clients[doc.Metadata.Name] = client
			result.Applied = append(result.Applied, AppliedResource{Kind: doc.Kind, Name: doc.Metadata.Name, Action: action, Detail: client.ID})
		case "Variable":
			continue
		case "Resource":
			continue
		case "AppRuntimeLifecycle":
			continue
		case "AppSource":
			action, detail, err := s.applyAppSource(ctx, doc, options, credentials)
			if err != nil {
				return result, resourceError(doc, err)
			}
			result.Applied = append(result.Applied, AppliedResource{Kind: doc.Kind, Name: doc.Metadata.Name, Action: action, Detail: detail})
		case "InputSettings":
			action, detail, err := s.applyInputSettings(ctx, doc, options, clients)
			if err != nil {
				return result, resourceError(doc, err)
			}
			result.Applied = append(result.Applied, AppliedResource{Kind: doc.Kind, Name: doc.Metadata.Name, Action: action, Detail: detail})
		default:
			return result, resourceError(doc, fmt.Errorf("unsupported kind %q", doc.Kind))
		}
	}
	return result, nil
}

func (s Service) Export(ctx context.Context, workspace string, includeValues bool) ([]Document, error) {
	workspace = contract.NormalizeWorkspace(workspace)
	docs := []Document{}
	if s.GitSources != nil {
		snapshot, err := s.GitSources.Load(ctx)
		if err != nil {
			return nil, err
		}
		sources := make([]gitsource.Source, 0, len(snapshot.Sources))
		for _, source := range snapshot.Sources {
			if contract.NormalizeWorkspace(source.Workspace) == workspace {
				sources = append(sources, source)
			}
		}
		sort.Slice(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })
		for _, source := range sources {
			doc := Document{
				APIVersion: APIVersion,
				Kind:       "AppSource",
				Metadata:   Metadata{Name: source.Name},
				Spec: Spec{
					Name: source.Name,
					Repository: Repository{
						URL:           source.RepoURL,
						Branch:        source.Branch,
						Subpath:       source.Subpath,
						CredentialRef: source.TokenEnv,
					},
				},
			}
			docs = append(docs, doc)
		}
	}
	clients, err := s.Store.ListClients(ctx, workspace)
	if err != nil {
		return nil, err
	}
	for _, client := range clients {
		policy := client.EffectiveInvocationPolicy()
		docs = append(docs, Document{
			APIVersion: APIVersion,
			Kind:       "Client",
			Metadata:   Metadata{Name: client.Name},
			Spec: Spec{
				Name: client.Name,
				InvocationPolicy: &InvocationPolicySpec{
					Mode: policy.Mode, AllowedTargets: append([]string{}, policy.AllowedTargets...),
				},
			},
		})
	}
	policies, err := s.Store.ListExecutionLimitPolicies(ctx, workspace, "")
	if err != nil {
		return nil, err
	}
	for _, policy := range policies {
		docs = append(docs, Document{
			APIVersion: APIVersion,
			Kind:       "ExecutionLimitPolicy",
			Metadata:   Metadata{Name: resourceName(policy.AppKey, policy.Kind+"-"+policy.PolicyID)},
			Spec: Spec{
				AppKey: policy.AppKey, ActionKey: policy.ActionKey, Scope: policy.Scope,
				PolicyID: policy.PolicyID, LimitKind: policy.Kind, ShapeFingerprint: policy.ShapeFingerprint,
				Allowance: policy.Allowance, WindowSeconds: policy.WindowSeconds,
			},
		})
	}
	variables, err := s.Store.ListVariables(ctx, workspace)
	if err != nil {
		return nil, err
	}
	sort.Slice(variables, func(i, j int) bool {
		if variables[i].AppKey != variables[j].AppKey {
			return variables[i].AppKey < variables[j].AppKey
		}
		return variables[i].Path < variables[j].Path
	})
	for _, variable := range variables {
		value := ValueSource{Redacted: true}
		if includeValues && !variable.IsSecret {
			value = ValueSource{Value: variable.Value}
		}
		docs = append(docs, Document{
			APIVersion: APIVersion,
			Kind:       "Variable",
			Metadata:   Metadata{Name: resourceName(variable.AppKey, variable.Path)},
			Spec: Spec{
				Path:        variable.Path,
				AppScope:    variable.AppKey,
				Value:       value,
				Secret:      variable.IsSecret,
				Description: variable.Description,
				Revision:    variable.Revision,
			},
		})
	}
	resources, err := s.Store.ListResources(ctx, workspace)
	if err != nil {
		return nil, err
	}
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].AppKey != resources[j].AppKey {
			return resources[i].AppKey < resources[j].AppKey
		}
		return resources[i].Path < resources[j].Path
	})
	for _, resource := range resources {
		var value any
		if len(resource.Value) != 0 && json.Unmarshal(resource.Value, &value) != nil {
			return nil, fmt.Errorf("Resource %s has invalid JSON", resource.Path)
		}
		docs = append(docs, Document{
			APIVersion: APIVersion,
			Kind:       "Resource",
			Metadata:   Metadata{Name: resourceName(resource.AppKey, resource.Path)},
			Spec: Spec{Path: resource.Path, AppScope: resource.AppKey, ResourceType: resource.ResourceType,
				ResourceValue: value, Description: resource.Description, Revision: resource.Revision},
		})
	}
	lifecycles, err := s.Store.ListAppRuntimeLifecycles(ctx, workspace)
	if err != nil {
		return nil, err
	}
	for _, lifecycle := range lifecycles {
		docs = append(docs, Document{APIVersion: APIVersion, Kind: "AppRuntimeLifecycle", Metadata: Metadata{Name: lifecycle.AppKey},
			Spec: Spec{AppKey: lifecycle.AppKey, RuntimeState: lifecycle.State, Reason: lifecycle.Reason, Revision: lifecycle.Revision}})
	}
	inputDocs, err := s.exportInputSettings(ctx, workspace, includeValues)
	if err != nil {
		return nil, err
	}
	docs = append(docs, inputDocs...)
	return docs, nil
}

func (s Service) exportInputSettings(ctx context.Context, workspace string, includeValues bool) ([]Document, error) {
	seen := map[string]state.InputConfig{}
	for _, appKey := range s.AppKeys {
		configs, err := s.Store.ListInputConfigsForApp(ctx, workspace, appKey)
		if err != nil {
			return nil, err
		}
		for _, config := range configs {
			seen[inputConfigKeyForExport(config)] = config
		}
	}
	clients, err := s.Store.ListClients(ctx, workspace)
	if err != nil {
		return nil, err
	}
	for _, client := range clients {
		configs, err := s.Store.ListInputConfigsForClient(ctx, workspace, client.ID)
		if err != nil {
			return nil, err
		}
		for _, config := range configs {
			seen[inputConfigKeyForExport(config)] = config
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	docs := []Document{}
	for _, key := range keys {
		config := seen[key]
		values := map[string]any{}
		if includeValues {
			var decoded map[string]any
			_ = json.Unmarshal(config.Config, &decoded)
			values = decoded
		} else {
			var decoded map[string]json.RawMessage
			_ = json.Unmarshal(config.Config, &decoded)
			for configKey := range decoded {
				values[configKey] = ValueSource{Redacted: true}
			}
		}
		docs = append(docs, Document{
			APIVersion: APIVersion,
			Kind:       "InputSettings",
			Metadata:   Metadata{Name: resourceName(config.AppKey, inputDetail(config.AppKey, config.ActionKey, config.ClientID))},
			Spec: Spec{
				AppKey:     config.AppKey,
				ActionKey:  config.ActionKey,
				ClientRef:  config.ClientID,
				Config:     values,
				LockedKeys: append([]string(nil), config.LockedKeys...),
			},
		})
	}
	return docs, nil
}

func (s Service) applyExecutionLimitPolicyDocuments(ctx context.Context, docs []Document, options Options) ([]AppliedResource, error) {
	existing, err := s.Store.ListExecutionLimitPolicies(ctx, options.Workspace, "")
	if err != nil {
		return nil, err
	}
	existingByKey := make(map[string]state.ExecutionLimitPolicy, len(existing))
	for _, policy := range existing {
		existingByKey[provisioningExecutionLimitPolicyKey(policy.ExecutionLimitPolicyKey)] = policy
	}
	type pendingPolicy struct {
		doc     Document
		request state.MutateExecutionLimitPolicyRequest
	}
	pending := make([]pendingPolicy, 0)
	results := make([]AppliedResource, 0)
	for _, doc := range docs {
		if doc.Kind != "ExecutionLimitPolicy" {
			continue
		}
		key, keyErr := state.NormalizeExecutionLimitPolicyKey(state.ExecutionLimitPolicyKey{
			WorkspaceID: options.Workspace, AppKey: doc.Spec.AppKey, ActionKey: doc.Spec.ActionKey,
			Scope: doc.Spec.Scope, PolicyID: doc.Spec.PolicyID, Kind: doc.Spec.LimitKind,
		})
		if keyErr != nil {
			return nil, resourceError(doc, keyErr)
		}
		if key.PolicyID == executionlimit.ImplicitAppConcurrencyPolicyID && key.Scope == executionlimit.ScopeApp && key.Kind == executionlimit.KindConcurrency {
			expected, fingerprintErr := executionlimit.AppConcurrencyFingerprint(key.WorkspaceID, key.AppKey)
			if fingerprintErr != nil || expected != strings.TrimSpace(doc.Spec.ShapeFingerprint) {
				return nil, resourceError(doc, errors.New("implicit App concurrency shapeFingerprint does not match appKey"))
			}
		}
		current, found := existingByKey[provisioningExecutionLimitPolicyKey(key)]
		if found && current.ShapeFingerprint != strings.TrimSpace(doc.Spec.ShapeFingerprint) {
			return nil, resourceError(doc, errors.New("existing policy has a different shapeFingerprint; delete it explicitly before importing a replacement"))
		}
		if found && current.Allowance == doc.Spec.Allowance && current.WindowSeconds == doc.Spec.WindowSeconds {
			results = append(results, AppliedResource{Kind: doc.Kind, Name: doc.Metadata.Name, Action: "unchanged", Detail: key.AppKey + "/" + key.PolicyID})
			continue
		}
		expectedRevision := int64(0)
		if found {
			expectedRevision = current.Revision
		}
		policy := state.ExecutionLimitPolicy{
			ExecutionLimitPolicyKey: key, ShapeFingerprint: strings.TrimSpace(doc.Spec.ShapeFingerprint),
			Allowance: doc.Spec.Allowance, WindowSeconds: doc.Spec.WindowSeconds,
		}
		seed, _ := json.Marshal(struct {
			Policy           state.ExecutionLimitPolicy `json:"policy"`
			ExpectedRevision int64                      `json:"expectedRevision"`
		}{policy, expectedRevision})
		request := state.MutateExecutionLimitPolicyRequest{
			Policy: policy, ExpectedRevision: expectedRevision,
			OperationID: stableID("provision_limit", string(seed)), RequestFingerprint: stableID("request", string(seed)),
			Actor: options.Actor, Reason: "declarative provisioning import",
		}
		if _, normalizeErr := state.NormalizeExecutionLimitPolicyMutation(request); normalizeErr != nil {
			return nil, resourceError(doc, normalizeErr)
		}
		pending = append(pending, pendingPolicy{doc: doc, request: request})
	}
	if options.DryRun {
		for _, item := range pending {
			results = append(results, AppliedResource{Kind: item.doc.Kind, Name: item.doc.Metadata.Name, Action: "validated", Detail: item.request.Policy.AppKey + "/" + item.request.Policy.PolicyID})
		}
		return results, nil
	}
	requests := make([]state.MutateExecutionLimitPolicyRequest, len(pending))
	for index, item := range pending {
		requests[index] = item.request
	}
	mutations, err := s.Store.MutateExecutionLimitPolicies(ctx, requests)
	if err != nil {
		if len(pending) > 0 {
			return nil, resourceError(pending[0].doc, err)
		}
		return nil, err
	}
	for index, mutation := range mutations {
		action := "stored"
		if mutation.Replayed {
			action = "unchanged"
		}
		item := pending[index]
		results = append(results, AppliedResource{Kind: item.doc.Kind, Name: item.doc.Metadata.Name, Action: action, Detail: mutation.Policy.AppKey + "/" + mutation.Policy.PolicyID})
	}
	return results, nil
}

func provisioningExecutionLimitPolicyKey(key state.ExecutionLimitPolicyKey) string {
	return strings.Join([]string{key.WorkspaceID, key.AppKey, key.ActionKey, key.Scope, key.PolicyID, key.Kind}, "\x1f")
}

func EncodeYAML(docs []Document) ([]byte, error) {
	data, err := yaml.Marshal(docs)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func (s Service) applyClient(ctx context.Context, doc Document, options Options) (state.Client, string, error) {
	name := firstNonEmpty(doc.Spec.Name, doc.Metadata.Name)
	if name == "" {
		return state.Client{}, "", errors.New("client name is required")
	}
	if valueSourceHasCredential(doc.Spec.ExternalKey) || valueSourceHasCredential(doc.Spec.ClientKey) {
		return state.Client{}, "", errors.New("client credentials are issued and rotated through the control plane API")
	}
	desiredPolicy, hasDesiredPolicy, err := provisioningTargetPolicy(doc.Spec.InvocationPolicy)
	if err != nil {
		return state.Client{}, "", err
	}
	client, err := s.clientByName(ctx, options.Workspace, name)
	if err == nil {
		updated, changed, err := s.applyClientInvocationPolicy(ctx, client, desiredPolicy, hasDesiredPolicy, options)
		if err != nil {
			return state.Client{}, "", err
		}
		if changed {
			return updated, dryRunAction(options, "updated"), nil
		}
		return updated, "unchanged", nil
	}
	if !errors.Is(err, state.ErrNotFound) {
		return state.Client{}, "", err
	}
	if options.DryRun {
		client = state.Client{ID: stableID("client", name), WorkspaceID: options.Workspace, Name: name}
		if hasDesiredPolicy {
			client.InvocationPolicy = desiredPolicy
		}
		return client, "validated", nil
	}
	var initialPolicy *state.TargetPolicy
	if hasDesiredPolicy {
		initialPolicy = &desiredPolicy
	}
	created, err := s.Store.CreateClientWithInvocationPolicy(ctx, state.CreateClientRequest{
		WorkspaceID: options.Workspace, Name: name, InvocationPolicy: initialPolicy, Actor: options.Actor,
	})
	return created, "created", err
}

func provisioningTargetPolicy(spec *InvocationPolicySpec) (state.TargetPolicy, bool, error) {
	if spec == nil {
		return state.TargetPolicy{}, false, nil
	}
	policy, err := state.NormalizeTargetPolicy(state.TargetPolicy{
		Mode: spec.Mode, AllowedTargets: spec.AllowedTargets,
	})
	if err != nil {
		return state.TargetPolicy{}, false, fmt.Errorf("invalid invocationPolicy: %w", err)
	}
	return policy, true, nil
}

func (s Service) applyClientInvocationPolicy(ctx context.Context, client state.Client, desired state.TargetPolicy, present bool, options Options) (state.Client, bool, error) {
	if !present || sameTargetPolicy(client.EffectiveInvocationPolicy(), desired) {
		return client, false, nil
	}
	if options.DryRun {
		client.InvocationPolicy = desired
		client.InvocationPolicyRevision++
		return client, true, nil
	}
	requestSeed, _ := json.Marshal(struct {
		ClientID         string             `json:"client_id"`
		ExpectedRevision int64              `json:"expected_revision"`
		Policy           state.TargetPolicy `json:"policy"`
	}{client.ID, client.InvocationPolicyRevision, desired})
	operationID := stableID("provision_policy", string(requestSeed))
	updated, _, err := s.Store.UpdateClientInvocationPolicy(ctx, state.UpdateClientInvocationPolicyRequest{
		WorkspaceID: options.Workspace, ClientID: client.ID, Policy: desired,
		OperationID: operationID, ExpectedRevision: client.InvocationPolicyRevision,
		RequestFingerprint: stableID("request", string(requestSeed)), Actor: options.Actor,
	})
	return updated, true, err
}

func sameTargetPolicy(left state.TargetPolicy, right state.TargetPolicy) bool {
	left = state.EffectiveTargetPolicy(left)
	right = state.EffectiveTargetPolicy(right)
	if left.Mode != right.Mode || len(left.AllowedTargets) != len(right.AllowedTargets) {
		return false
	}
	for index := range left.AllowedTargets {
		if left.AllowedTargets[index] != right.AllowedTargets[index] {
			return false
		}
	}
	return true
}

func (s Service) applyVariable(ctx context.Context, doc Document, options Options) (string, string, error) {
	path := strings.TrimSpace(doc.Spec.Path)
	if path == "" {
		path = strings.TrimSpace(doc.Metadata.Name)
	}
	if path == "" {
		return "", "", errors.New("variable path is required")
	}
	if valueSourceIsRedacted(doc.Spec.Value) {
		existing, err := s.existingVariable(ctx, options.Workspace, doc.Spec.AppScope, path)
		if err != nil {
			return "", "", fmt.Errorf("redacted variable requires existing value for %q: %w", path, err)
		}
		if options.DryRun {
			return "validated", path, nil
		}
		isSecret := existing.IsSecret
		if doc.Spec.Secret {
			isSecret = true
		}
		if err := s.Store.SetVariable(ctx, options.Workspace, doc.Spec.AppScope, path, existing.Value, isSecret, doc.Spec.Description); err != nil {
			return "", "", err
		}
		return "unchanged", path, nil
	}
	value, err := resolveString(doc.Spec.Value)
	if err != nil {
		return "", "", err
	}
	if options.DryRun {
		return "validated", path, nil
	}
	if doc.Spec.Secret && s.Encrypt != nil {
		value, err = s.Encrypt(ctx, options.Workspace, value)
		if err != nil {
			return "", "", err
		}
	}
	if err := s.Store.SetVariable(ctx, options.Workspace, doc.Spec.AppScope, path, value, doc.Spec.Secret, doc.Spec.Description); err != nil {
		return "", "", err
	}
	return "stored", path, nil
}

func (s Service) runtimeConfigProvisioningBatch(ctx context.Context, docs []Document, options Options) (state.RuntimeConfigProvisioningBatch, []AppliedResource, error) {
	batch := state.RuntimeConfigProvisioningBatch{WorkspaceID: options.Workspace, Actor: options.Actor, DryRun: options.DryRun}
	results := []AppliedResource{}
	seen := map[string]struct{}{}
	for _, doc := range docs {
		switch doc.Kind {
		case "Variable":
			path := strings.TrimSpace(firstNonEmpty(doc.Spec.Path, doc.Metadata.Name))
			if path == "" {
				return batch, results, resourceError(doc, errors.New("variable path is required"))
			}
			key := "variable\x00" + doc.Spec.AppScope + "\x00" + path
			if _, duplicate := seen[key]; duplicate {
				return batch, results, resourceError(doc, errors.New("duplicate Variable"))
			}
			seen[key] = struct{}{}
			value, isSecret := "", doc.Spec.Secret
			if valueSourceIsRedacted(doc.Spec.Value) {
				existing, err := s.existingVariable(ctx, options.Workspace, doc.Spec.AppScope, path)
				if err != nil {
					return batch, results, resourceError(doc, fmt.Errorf("redacted variable requires existing value for %q: %w", path, err))
				}
				value, isSecret = existing.Value, existing.IsSecret || doc.Spec.Secret
			} else {
				var err error
				value, err = resolveString(doc.Spec.Value)
				if err != nil {
					return batch, results, resourceError(doc, err)
				}
				if isSecret && s.Encrypt != nil {
					value, err = s.Encrypt(ctx, options.Workspace, value)
					if err != nil {
						return batch, results, resourceError(doc, err)
					}
				}
			}
			batch.Variables = append(batch.Variables, state.ProvisionedRuntimeVariable{AppKey: doc.Spec.AppScope, Path: path,
				Value: value, IsSecret: isSecret, Description: doc.Spec.Description, Revision: doc.Spec.Revision})
			results = append(results, AppliedResource{Kind: doc.Kind, Name: doc.Metadata.Name, Action: dryRunAction(options, "stored"), Detail: path})
		case "Resource":
			path := strings.TrimSpace(firstNonEmpty(doc.Spec.Path, doc.Metadata.Name))
			if path == "" {
				return batch, results, resourceError(doc, errors.New("resource path is required"))
			}
			key := "resource\x00" + doc.Spec.AppScope + "\x00" + path
			if _, duplicate := seen[key]; duplicate {
				return batch, results, resourceError(doc, errors.New("duplicate Resource"))
			}
			seen[key] = struct{}{}
			value := doc.Spec.ResourceValue
			if value == nil {
				value = map[string]any{}
			}
			raw, err := json.Marshal(value)
			if err != nil {
				return batch, results, resourceError(doc, fmt.Errorf("resource value: %w", err))
			}
			batch.Resources = append(batch.Resources, state.ProvisionedRuntimeResource{AppKey: doc.Spec.AppScope, Path: path,
				Value: raw, ResourceType: strings.TrimSpace(doc.Spec.ResourceType), Description: doc.Spec.Description, Revision: doc.Spec.Revision})
			results = append(results, AppliedResource{Kind: doc.Kind, Name: doc.Metadata.Name, Action: dryRunAction(options, "stored"), Detail: path})
		case "AppRuntimeLifecycle":
			appKey := strings.TrimSpace(firstNonEmpty(doc.Spec.AppKey, doc.Metadata.Name))
			if appKey == "" {
				return batch, results, resourceError(doc, errors.New("App key is required"))
			}
			key := "lifecycle\x00" + appKey
			if _, duplicate := seen[key]; duplicate {
				return batch, results, resourceError(doc, errors.New("duplicate AppRuntimeLifecycle"))
			}
			seen[key] = struct{}{}
			batch.Lifecycles = append(batch.Lifecycles, state.ProvisionedAppRuntimeLifecycle{AppKey: appKey,
				State: doc.Spec.RuntimeState, Reason: doc.Spec.Reason, Revision: doc.Spec.Revision})
			results = append(results, AppliedResource{Kind: doc.Kind, Name: doc.Metadata.Name, Action: dryRunAction(options, "stored"), Detail: appKey})
		}
	}
	return batch, results, nil
}

func (s Service) applyResource(ctx context.Context, doc Document, options Options) (string, string, error) {
	path := strings.TrimSpace(doc.Spec.Path)
	if path == "" {
		path = strings.TrimSpace(doc.Metadata.Name)
	}
	if path == "" {
		return "", "", errors.New("resource path is required")
	}
	value := doc.Spec.ResourceValue
	if value == nil {
		value = map[string]any{}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", "", fmt.Errorf("resource value: %w", err)
	}
	if options.DryRun {
		return "validated", path, nil
	}
	if err := s.Store.SetResourceScoped(ctx, options.Workspace, doc.Spec.AppScope, path, raw, strings.TrimSpace(doc.Spec.ResourceType), doc.Spec.Description); err != nil {
		return "", "", err
	}
	return "stored", path, nil
}

func (s Service) applyAppSource(ctx context.Context, doc Document, options Options, credentials map[string]string) (string, string, error) {
	if s.GitSources == nil {
		return "", "", errors.New("git source registry is required")
	}
	name := firstNonEmpty(doc.Spec.Name, doc.Metadata.Name)
	repo := doc.Spec.Repository
	branch := firstNonEmpty(repo.Branch, "main")
	credsRef := strings.TrimSpace(repo.CredentialRef)
	if credsRef == "" && repo.AuthRef != "" {
		credsRef = credentials[repo.AuthRef]
	}
	if name == "" || strings.TrimSpace(repo.URL) == "" {
		return "", "", errors.New("source name and repository.url are required")
	}
	source := gitsource.Source{
		Workspace: options.Workspace,
		Name:      name,
		RepoURL:   strings.TrimSpace(repo.URL),
		Branch:    branch,
		Subpath:   strings.TrimSpace(repo.Subpath),
		TokenEnv:  credsRef,
		Kind:      "external",
	}
	if options.DryRun {
		return "validated", name, nil
	}
	existing, err := s.GitSources.Get(ctx, options.Workspace, name)
	if err == nil {
		updated, err := s.GitSources.Patch(ctx, options.Workspace, existing.ID, gitsource.Patch{
			Name:     stringPtr(name),
			RepoURL:  stringPtr(source.RepoURL),
			Branch:   stringPtr(source.Branch),
			Subpath:  stringPtr(source.Subpath),
			TokenEnv: stringPtr(source.TokenEnv),
		})
		if err != nil {
			return "", "", err
		}
		return "updated", updated.ID, nil
	}
	if !errors.Is(err, gitsource.ErrGitSourceNotFound) {
		return "", "", err
	}
	created, err := s.GitSources.Create(ctx, source)
	if err != nil {
		return "", "", err
	}
	return "created", created.ID, nil
}

func (s Service) applyInputSettings(ctx context.Context, doc Document, options Options, clients map[string]state.Client) (string, string, error) {
	appKey := strings.TrimSpace(doc.Spec.AppKey)
	if appKey == "" {
		return "", "", errors.New("appKey is required")
	}
	clientID := ""
	if ref := strings.TrimSpace(doc.Spec.ClientRef); ref != "" {
		if client, ok := clients[ref]; ok {
			clientID = client.ID
		} else {
			client, err := s.Store.GetClient(ctx, options.Workspace, ref)
			if err != nil {
				return "", "", fmt.Errorf("clientRef %q was not found", ref)
			}
			clientID = client.ID
		}
	}
	existing, err := s.findInputConfig(ctx, options.Workspace, appKey, strings.TrimSpace(doc.Spec.ActionKey), clientID)
	if err != nil {
		return "", "", err
	}
	values, err := resolveConfigWithExisting(doc.Spec.Config, existing)
	if err != nil {
		return "", "", err
	}
	configJSON, err := json.Marshal(values)
	if err != nil {
		return "", "", err
	}
	if options.DryRun {
		return "validated", inputDetail(appKey, doc.Spec.ActionKey, clientID), nil
	}
	_, err = s.Store.SetInputConfig(ctx, state.InputConfig{
		WorkspaceID: options.Workspace,
		AppKey:      appKey,
		ActionKey:   strings.TrimSpace(doc.Spec.ActionKey),
		ClientID:    clientID,
		Config:      configJSON,
		LockedKeys:  doc.Spec.LockedKeys,
	}, options.Actor)
	return "stored", inputDetail(appKey, doc.Spec.ActionKey, clientID), err
}

func decodeJSON(data []byte) ([]Document, error) {
	var list []Document
	if err := json.Unmarshal(data, &list); err == nil {
		return list, nil
	}
	var envelope struct {
		Resources []Document `json:"resources"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil && envelope.Resources != nil {
		return envelope.Resources, nil
	}
	var single Document
	if err := json.Unmarshal(data, &single); err != nil {
		return nil, err
	}
	return []Document{single}, nil
}

func decodeYAML(data []byte) ([]Document, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	if len(node.Content) == 0 {
		return nil, nil
	}
	root := node.Content[0]
	var docs []Document
	if root.Kind == yaml.SequenceNode {
		if err := root.Decode(&docs); err != nil {
			return nil, err
		}
		return docs, nil
	}
	var envelope struct {
		Resources []Document `yaml:"resources"`
	}
	if err := root.Decode(&envelope); err == nil && envelope.Resources != nil {
		return envelope.Resources, nil
	}
	var single Document
	if err := root.Decode(&single); err != nil {
		return nil, err
	}
	return []Document{single}, nil
}

func normalizeDocument(doc *Document) error {
	doc.APIVersion = strings.TrimSpace(doc.APIVersion)
	doc.Kind = strings.TrimSpace(doc.Kind)
	doc.Metadata.Name = strings.TrimSpace(doc.Metadata.Name)
	if doc.APIVersion == "" {
		doc.APIVersion = APIVersion
	}
	if doc.APIVersion != APIVersion {
		return fmt.Errorf("unsupported apiVersion %q", doc.APIVersion)
	}
	if doc.Kind == "" {
		return errors.New("kind is required")
	}
	if doc.Metadata.Name == "" {
		return errors.New("metadata.name is required")
	}
	return nil
}

func gitCredential(doc Document) (string, string, error) {
	ref := gitCredentialRef(doc)
	method := strings.ToLower(strings.TrimSpace(doc.Spec.Method))
	if method == "" {
		method = "pat"
	}
	switch method {
	case "none", "public":
		return ref, "", nil
	case "pat", "token", "access_token":
		token, err := resolveString(doc.Spec.Token, doc.Spec.Value)
		if err != nil {
			return "", "", err
		}
		if token == "" {
			return "", "", errors.New("token is required")
		}
		value, err := marshalStringMap(map[string]string{"type": "pat", "token": token})
		return ref, value, err
	case "basic", "password":
		username, err := resolveString(doc.Spec.Username)
		if err != nil {
			return "", "", err
		}
		password, err := resolveString(doc.Spec.Password, doc.Spec.Token)
		if err != nil {
			return "", "", err
		}
		if username == "" || password == "" {
			return "", "", errors.New("username and password are required")
		}
		value, err := marshalStringMap(map[string]string{"type": "basic", "username": username, "password": password})
		return ref, value, err
	default:
		return "", "", fmt.Errorf("unsupported credential method %q", doc.Spec.Method)
	}
}

func gitCredentialRef(doc Document) string {
	ref := strings.TrimSpace(doc.Spec.StorageRef)
	if ref == "" {
		ref = "git/" + doc.Metadata.Name + "/credential"
	}
	return ref
}

func gitCredentialHasRedactedValue(doc Document) bool {
	return valueSourceIsRedacted(doc.Spec.Value) ||
		valueSourceIsRedacted(doc.Spec.Token) ||
		valueSourceIsRedacted(doc.Spec.Username) ||
		valueSourceIsRedacted(doc.Spec.Password)
}

func resolveConfig(values map[string]any) (map[string]json.RawMessage, error) {
	return resolveConfigWithExisting(values, nil)
}

func resolveConfigWithExisting(values map[string]any, existing map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	resolved := map[string]json.RawMessage{}
	for key, raw := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, errors.New("input setting key must not be empty")
		}
		if redactedAny(raw) {
			value, ok := existing[key]
			if !ok {
				return nil, fmt.Errorf("%s: redacted input setting requires an existing value", key)
			}
			resolved[key] = append(json.RawMessage(nil), value...)
			continue
		}
		value, err := resolveAny(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		resolved[key] = data
	}
	return resolved, nil
}

func (s Service) requireExistingVariable(ctx context.Context, options Options, appKey string, path string, label string) error {
	_, err := s.existingVariable(ctx, options.Workspace, appKey, path)
	if err != nil {
		return fmt.Errorf("%s requires existing variable %q: %w", label, path, err)
	}
	return nil
}

func (s Service) existingVariable(ctx context.Context, workspace string, appKey string, path string) (state.Variable, error) {
	variable, found, err := s.Store.GetVariableExact(ctx, workspace, appKey, path)
	if err != nil {
		return state.Variable{}, err
	}
	if !found {
		return state.Variable{}, state.ErrNotFound
	}
	return variable, nil
}

func (s Service) clientByName(ctx context.Context, workspace string, name string) (state.Client, error) {
	clients, err := s.Store.ListClients(ctx, workspace)
	if err != nil {
		return state.Client{}, err
	}
	for _, client := range clients {
		if client.Name == name {
			return client, nil
		}
	}
	return state.Client{}, state.ErrNotFound
}

func (s Service) findInputConfig(ctx context.Context, workspace string, appKey string, actionKey string, clientID string) (map[string]json.RawMessage, error) {
	configs, err := s.Store.ListInputConfigsForApp(ctx, workspace, appKey)
	if err != nil {
		return nil, err
	}
	for _, config := range configs {
		if config.ActionKey == actionKey && config.ClientID == clientID {
			var values map[string]json.RawMessage
			if err := json.Unmarshal(config.Config, &values); err != nil {
				return nil, err
			}
			return values, nil
		}
	}
	return nil, nil
}

func valueSourceIsRedacted(source ValueSource) bool {
	return source.Redacted && source.Value == nil && source.ValueFrom == nil
}

func valueSourceHasCredential(source ValueSource) bool {
	return source.Value != nil || source.ValueFrom != nil
}

func redactedAny(raw any) bool {
	if source, ok := raw.(ValueSource); ok {
		return valueSourceIsRedacted(source)
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	_, hasValue := m["value"]
	return m["redacted"] == true && !hasValue && m["valueFrom"] == nil
}

func resolveString(sources ...ValueSource) (string, error) {
	for _, source := range sources {
		value, err := source.Resolve()
		if err != nil {
			return "", err
		}
		if value != "" {
			return value, nil
		}
	}
	return "", nil
}

func (source ValueSource) Resolve() (string, error) {
	if source.Redacted {
		return "", errors.New("redacted value cannot be applied; provide valueFrom.env or valueFrom.file")
	}
	if source.ValueFrom != nil {
		if source.ValueFrom.Env != "" {
			value, ok := os.LookupEnv(source.ValueFrom.Env)
			if !ok {
				return "", fmt.Errorf("environment variable %s is not set", source.ValueFrom.Env)
			}
			return value, nil
		}
		if source.ValueFrom.File != "" {
			data, err := os.ReadFile(source.ValueFrom.File)
			if err != nil {
				return "", err
			}
			return strings.TrimRight(string(data), "\r\n"), nil
		}
	}
	if source.Value == nil {
		return "", nil
	}
	switch value := source.Value.(type) {
	case string:
		return value, nil
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
}

func resolveAny(raw any) (any, error) {
	if m, ok := raw.(map[string]any); ok {
		if _, hasValue := m["value"]; hasValue || m["valueFrom"] != nil || m["redacted"] != nil {
			source := ValueSource{}
			data, _ := json.Marshal(m)
			if err := json.Unmarshal(data, &source); err != nil {
				return nil, err
			}
			value, err := source.Resolve()
			if err != nil {
				return nil, err
			}
			if source.Value != nil {
				return source.Value, nil
			}
			return value, nil
		}
	}
	return raw, nil
}

func marshalStringMap(value map[string]string) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func dryRunAction(options Options, action string) string {
	if options.DryRun {
		return "validated"
	}
	return action
}

func resourceError(doc Document, err error) error {
	location := doc.Metadata.Name
	if doc.SourcePath != "" {
		location = doc.SourcePath + ":" + location
	}
	return fmt.Errorf("%s %s: %w", doc.Kind, location, err)
}

func inputDetail(appKey string, actionKey string, clientID string) string {
	parts := []string{appKey}
	if actionKey != "" {
		parts = append(parts, actionKey)
	}
	if clientID != "" {
		parts = append(parts, "client="+clientID)
	}
	return strings.Join(parts, "/")
}

func inputConfigKeyForExport(config state.InputConfig) string {
	return config.AppKey + "\x00" + config.ActionKey + "\x00" + config.ClientID
}

func resourceName(appKey string, path string) string {
	value := strings.Trim(strings.ReplaceAll(appKey+"-"+path, "/", "-"), "-")
	if value == "" {
		value = "variable"
	}
	return value
}

func stableID(prefix string, value string) string {
	sum := sha256.Sum256([]byte(value))
	return prefix + "_" + hex.EncodeToString(sum[:6])
}

func stringPtr(value string) *string {
	return &value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
