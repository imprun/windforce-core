package runtimeconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
)

// ValidateSecretReferences enforces release-owned secret annotations without
// resolving plaintext. A schema node marked writeOnly or x-windforce-secret
// must contain an exact $var reference, and that Variable must itself be
// secret. The returned paths are normalized and sorted.
func (r *Resolver) ValidateSecretReferences(
	ctx context.Context,
	workspaceID string,
	appKey string,
	schema json.RawMessage,
	input json.RawMessage,
) ([]string, error) {
	references, err := secretReferences(schema, input)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(references))
	for _, reference := range references {
		variable, found, err := r.Store.GetVariableScoped(ctx, workspaceID, reference.Scope, appKey, reference.Path)
		if err != nil {
			return nil, fmt.Errorf("read secret variable %q: %w", reference.Path, err)
		}
		if !found {
			return nil, fmt.Errorf("secret variable %q: %w", reference.Path, errors.New("not found"))
		}
		if !variable.IsSecret {
			return nil, fmt.Errorf("variable %q is not secret", reference.Path)
		}
		paths = append(paths, reference.Path)
	}
	return paths, nil
}

func secretReferences(schema json.RawMessage, input json.RawMessage) ([]contract.RuntimeConfigReference, error) {
	if len(strings.TrimSpace(string(schema))) == 0 {
		return nil, nil
	}
	var root any
	if err := json.Unmarshal(schema, &root); err != nil {
		return nil, fmt.Errorf("decode operator settings schema: %w", err)
	}
	var value any
	if err := json.Unmarshal(input, &value); err != nil {
		return nil, fmt.Errorf("decode input for secret reference validation: %w", err)
	}
	paths := map[string]contract.RuntimeConfigReference{}
	if err := walkSecretSchema(root, root, value, "$", paths, map[string]bool{}, 0); err != nil {
		return nil, err
	}
	result := make([]contract.RuntimeConfigReference, 0, len(paths))
	for _, reference := range paths {
		result = append(result, reference)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Scope == result[j].Scope {
			return result[i].Path < result[j].Path
		}
		return result[i].Scope < result[j].Scope
	})
	return result, nil
}

func walkSecretSchema(
	root any,
	schema any,
	value any,
	instancePath string,
	paths map[string]contract.RuntimeConfigReference,
	refStack map[string]bool,
	depth int,
) error {
	if depth > 64 {
		return errors.New("operator settings schema nesting exceeds limit 64")
	}
	node, ok := schema.(map[string]any)
	if !ok {
		return nil
	}
	if ref, ok := node["$ref"].(string); ok && strings.HasPrefix(ref, "#/") {
		if refStack[ref] {
			return fmt.Errorf("operator settings schema reference cycle at %q", ref)
		}
		resolved, err := resolveLocalSchemaReference(root, ref)
		if err != nil {
			return err
		}
		refStack[ref] = true
		if err := walkSecretSchema(root, resolved, value, instancePath, paths, refStack, depth+1); err != nil {
			return err
		}
		delete(refStack, ref)
	}

	if schemaSecret(node) {
		reference, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s is secret and must be an exact $var reference", instancePath)
		}
		parsed, isReference, err := contract.ParseRuntimeConfigReference(reference)
		if err != nil {
			return fmt.Errorf("%s: %w", instancePath, err)
		}
		if !isReference || parsed.Kind != "var" {
			return fmt.Errorf("%s is secret and must be an exact $var reference", instancePath)
		}
		paths[string(parsed.Scope)+"\x00"+parsed.Path] = parsed
		return nil
	}

	for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
		branches, _ := node[keyword].([]any)
		for _, branch := range branches {
			if err := walkSecretSchema(root, branch, value, instancePath, paths, refStack, depth+1); err != nil {
				return err
			}
		}
	}

	switch typed := value.(type) {
	case map[string]any:
		properties, _ := node["properties"].(map[string]any)
		patternProperties, _ := node["patternProperties"].(map[string]any)
		for key, child := range typed {
			childSchema, found := properties[key]
			if found {
				if err := walkSecretSchema(root, childSchema, child, joinInstancePath(instancePath, key), paths, refStack, depth+1); err != nil {
					return err
				}
			}
			for pattern, candidate := range patternProperties {
				matches, err := regexp.MatchString(pattern, key)
				if err != nil {
					return fmt.Errorf("invalid operator settings pattern %q: %w", pattern, err)
				}
				if matches {
					if err := walkSecretSchema(root, candidate, child, joinInstancePath(instancePath, key), paths, refStack, depth+1); err != nil {
						return err
					}
				}
			}
			if !found && len(patternProperties) == 0 {
				if additional, ok := node["additionalProperties"].(map[string]any); ok {
					if err := walkSecretSchema(root, additional, child, joinInstancePath(instancePath, key), paths, refStack, depth+1); err != nil {
						return err
					}
				}
			}
		}
	case []any:
		if items, ok := node["items"].(map[string]any); ok {
			for index, child := range typed {
				if err := walkSecretSchema(root, items, child, fmt.Sprintf("%s[%d]", instancePath, index), paths, refStack, depth+1); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func schemaSecret(node map[string]any) bool {
	writeOnly, _ := node["writeOnly"].(bool)
	explicit, _ := node["x-windforce-secret"].(bool)
	return writeOnly || explicit
}

func resolveLocalSchemaReference(root any, reference string) (any, error) {
	current := root
	for _, encoded := range strings.Split(strings.TrimPrefix(reference, "#/"), "/") {
		segment := strings.ReplaceAll(strings.ReplaceAll(encoded, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("operator settings schema reference %q is not an object path", reference)
		}
		current, ok = object[segment]
		if !ok {
			return nil, fmt.Errorf("operator settings schema reference %q was not found", reference)
		}
	}
	return current, nil
}

func joinInstancePath(parent string, key string) string {
	if regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(key) {
		return parent + "." + key
	}
	encoded, _ := json.Marshal(key)
	return parent + "[" + string(encoded) + "]"
}
