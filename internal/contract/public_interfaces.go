package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	// AppManifestV2 identifies the Core-owned canonical v2 App manifest.
	AppManifestV2 = "windforce.app-manifest/v2"

	// Public interface bounds protect manifest publication and stored snapshots
	// without assigning any meaning to declaration fields.
	MaxPublicInterfacesPerAction       = 16
	MaxPublicInterfaceDeclarationBytes = 16 * 1024
	MaxPublicInterfacesBytesPerAction  = 64 * 1024
	MaxPublicInterfaceJSONDepth        = 16
	maxPublicInterfaceSourceBytes      = 64 * 1024
	maxPublicInterfacesSourceBytes     = 256 * 1024
)

// NormalizePublicInterfaces validates and canonicalizes opaque declarations.
// Declaration identity is the resulting canonical JSON byte sequence.
func NormalizePublicInterfaces(declarations []json.RawMessage) ([]json.RawMessage, error) {
	if len(declarations) > MaxPublicInterfacesPerAction {
		return nil, fmt.Errorf("publicInterfaces has %d declarations, maximum is %d", len(declarations), MaxPublicInterfacesPerAction)
	}
	if declarations == nil {
		return nil, nil
	}
	normalized := make([]json.RawMessage, 0, len(declarations))
	seen := make(map[string]struct{}, len(declarations))
	totalSourceBytes := 0
	totalCanonicalBytes := 0
	for index, declaration := range declarations {
		if len(declaration) == 0 {
			return nil, fmt.Errorf("publicInterfaces[%d] is empty", index)
		}
		if len(declaration) > maxPublicInterfaceSourceBytes {
			return nil, fmt.Errorf("publicInterfaces[%d] source encoding is %d bytes, maximum is %d", index, len(declaration), maxPublicInterfaceSourceBytes)
		}
		totalSourceBytes += len(declaration)
		if totalSourceBytes > maxPublicInterfacesSourceBytes {
			return nil, fmt.Errorf("publicInterfaces source encoding is %d bytes, maximum is %d", totalSourceBytes, maxPublicInterfacesSourceBytes)
		}
		canonical, err := canonicalPublicInterface(declaration)
		if err != nil {
			return nil, fmt.Errorf("publicInterfaces[%d]: %w", index, err)
		}
		if len(canonical) > MaxPublicInterfaceDeclarationBytes {
			return nil, fmt.Errorf("publicInterfaces[%d] canonical form is %d bytes, maximum is %d", index, len(canonical), MaxPublicInterfaceDeclarationBytes)
		}
		totalCanonicalBytes += len(canonical)
		if totalCanonicalBytes > MaxPublicInterfacesBytesPerAction {
			return nil, fmt.Errorf("publicInterfaces canonical form is %d bytes, maximum is %d", totalCanonicalBytes, MaxPublicInterfacesBytesPerAction)
		}
		identity := string(canonical)
		if _, duplicate := seen[identity]; duplicate {
			return nil, fmt.Errorf("publicInterfaces[%d] duplicates an earlier declaration", index)
		}
		seen[identity] = struct{}{}
		normalized = append(normalized, canonical)
	}
	return normalized, nil
}

// NormalizeDeploymentPublicInterfaces applies the canonical manifest-version
// contract to a Deployment and returns a detached snapshot.
func NormalizeDeploymentPublicInterfaces(deployment Deployment) (Deployment, error) {
	deployment = CloneDeployment(deployment)
	switch deployment.APIVersion {
	case "":
		for actionKey, action := range deployment.Actions {
			if len(action.PublicInterfaces) > 0 {
				return Deployment{}, fmt.Errorf("action %s publicInterfaces requires apiVersion %q", actionKey, AppManifestV2)
			}
		}
		return deployment, nil
	case AppManifestV2:
	default:
		return Deployment{}, fmt.Errorf("unsupported app manifest apiVersion %q", deployment.APIVersion)
	}
	for actionKey, action := range deployment.Actions {
		normalized, err := NormalizePublicInterfaces(action.PublicInterfaces)
		if err != nil {
			return Deployment{}, fmt.Errorf("action %s: %w", actionKey, err)
		}
		action.PublicInterfaces = normalized
		deployment.Actions[actionKey] = action
	}
	return deployment, nil
}

// ClonePublicInterfaces returns a detached copy of opaque JSON declarations.
func ClonePublicInterfaces(declarations []json.RawMessage) []json.RawMessage {
	if declarations == nil {
		return nil
	}
	cloned := make([]json.RawMessage, len(declarations))
	for index, declaration := range declarations {
		cloned[index] = append(json.RawMessage(nil), declaration...)
	}
	return cloned
}

// CloneAction returns a detached Action snapshot.
func CloneAction(action Action) Action {
	cloned := action
	cloned.Tag = cloneContractStringPointer(action.Tag)
	cloned.TagOverride = cloneContractStringPointer(action.TagOverride)
	cloned.RequiredLabelsOverride = cloneContractStringSlicePointer(action.RequiredLabelsOverride)
	cloned.Command = cloneContractStringSlice(action.Command)
	cloned.Adapter = cloneActionAdapter(action.Adapter)
	cloned.InputSchemaBody = append(json.RawMessage(nil), action.InputSchemaBody...)
	cloned.OutputSchemaBody = append(json.RawMessage(nil), action.OutputSchemaBody...)
	cloned.OperatorSettingsSchemaBody = append(json.RawMessage(nil), action.OperatorSettingsSchemaBody...)
	cloned.PublicInterfaces = ClonePublicInterfaces(action.PublicInterfaces)
	cloned.TimeoutS = cloneContractInt32Pointer(action.TimeoutS)
	cloned.Capabilities = cloneContractStringSlicePointer(action.Capabilities)
	cloned.RunsOn = cloneContractStringSlicePointer(action.RunsOn)
	cloned.RuntimeAccess = CloneRuntimeAccess(action.RuntimeAccess)
	cloned.ExecutionLimits = cloneExecutionLimits(action.ExecutionLimits)
	if action.UpdatedAt != nil {
		updatedAt := *action.UpdatedAt
		cloned.UpdatedAt = &updatedAt
	}
	return cloned
}

// CloneDeployment returns a detached Deployment snapshot.
func CloneDeployment(deployment Deployment) Deployment {
	cloned := deployment
	cloned.TagOverride = cloneContractStringPointer(deployment.TagOverride)
	cloned.RequiredLabelsOverride = cloneContractStringSlicePointer(deployment.RequiredLabelsOverride)
	cloned.MaxConcurrent = cloneContractInt32Pointer(deployment.MaxConcurrent)
	cloned.ExecutionLimits = cloneExecutionLimits(deployment.ExecutionLimits)
	cloned.RequiredCapabilities = cloneContractStringSlice(deployment.RequiredCapabilities)
	cloned.RequiredLabels = cloneContractStringSlice(deployment.RequiredLabels)
	cloned.Message = cloneContractStringPointer(deployment.Message)
	cloned.DeploymentID = cloneContractStringPointer(deployment.DeploymentID)
	cloned.CreatedBy = cloneContractStringPointer(deployment.CreatedBy)
	cloned.Actions = make(map[string]Action, len(deployment.Actions))
	for actionKey, action := range deployment.Actions {
		cloned.Actions[actionKey] = CloneAction(action)
	}
	if deployment.Actions == nil {
		cloned.Actions = nil
	}
	if deployment.UpdatedAt != nil {
		updatedAt := *deployment.UpdatedAt
		cloned.UpdatedAt = &updatedAt
	}
	return cloned
}

func canonicalPublicInterface(declaration json.RawMessage) (json.RawMessage, error) {
	if err := validatePublicInterfaceStructure(declaration); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(declaration))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(canonical), nil
}

func validatePublicInterfaceStructure(declaration json.RawMessage) error {
	decoder := json.NewDecoder(bytes.NewReader(declaration))
	decoder.UseNumber()
	first, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	delimiter, ok := first.(json.Delim)
	if !ok || delimiter != '{' {
		return errors.New("declaration must be a JSON object")
	}
	if err := validateJSONObject(decoder, 1); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("declaration has trailing JSON values")
		}
		return fmt.Errorf("invalid trailing JSON: %w", err)
	}
	return nil
}

func validateJSONObject(decoder *json.Decoder, depth int) error {
	if depth > MaxPublicInterfaceJSONDepth {
		return fmt.Errorf("JSON nesting exceeds maximum depth %d", MaxPublicInterfaceJSONDepth)
	}
	keys := map[string]struct{}{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("invalid object key: %w", err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return errors.New("object key is not a string")
		}
		if _, duplicate := keys[key]; duplicate {
			return fmt.Errorf("duplicate object key %q", key)
		}
		keys[key] = struct{}{}
		if err := validateJSONValue(decoder, depth); err != nil {
			return err
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("invalid object: %w", err)
	}
	if closing != json.Delim('}') {
		return errors.New("invalid object closing delimiter")
	}
	return nil
}

func validateJSONArray(decoder *json.Decoder, depth int) error {
	if depth > MaxPublicInterfaceJSONDepth {
		return fmt.Errorf("JSON nesting exceeds maximum depth %d", MaxPublicInterfaceJSONDepth)
	}
	for decoder.More() {
		if err := validateJSONValue(decoder, depth); err != nil {
			return err
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("invalid array: %w", err)
	}
	if closing != json.Delim(']') {
		return errors.New("invalid array closing delimiter")
	}
	return nil
}

func validateJSONValue(decoder *json.Decoder, parentDepth int) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("invalid JSON value: %w", err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		return validateJSONObject(decoder, parentDepth+1)
	case '[':
		return validateJSONArray(decoder, parentDepth+1)
	default:
		return errors.New("unexpected JSON closing delimiter")
	}
}

func cloneContractStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneContractInt32Pointer(value *int32) *int32 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneContractStringSlicePointer(value *[]string) *[]string {
	if value == nil {
		return nil
	}
	cloned := cloneContractStringSlice(*value)
	return &cloned
}

func cloneContractStringSlice(value []string) []string {
	if value == nil {
		return nil
	}
	return append([]string{}, value...)
}

func cloneActionAdapter(adapter *ActionAdapter) *ActionAdapter {
	if adapter == nil {
		return nil
	}
	cloned := *adapter
	cloned.Command = cloneContractStringSlice(adapter.Command)
	cloned.Env = cloneContractStringSlice(adapter.Env)
	if adapter.Options != nil {
		cloned.Options = make(map[string]json.RawMessage, len(adapter.Options))
		for key, value := range adapter.Options {
			cloned.Options[key] = append(json.RawMessage(nil), value...)
		}
	}
	return &cloned
}
