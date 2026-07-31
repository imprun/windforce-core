package resourceconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func ParseTypeReference(reference string) (string, string, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return "", "", nil
	}
	name, version, found := strings.Cut(reference, "@")
	name = strings.TrimSpace(name)
	version = strings.TrimSpace(version)
	if name == "" || (found && version == "") {
		return "", "", fmt.Errorf("invalid resource type reference %q", reference)
	}
	if !found {
		version = "1"
	}
	return name, version, nil
}

func ValidateSchema(schema json.RawMessage) error {
	_, err := compile(schema)
	return err
}

func ValidateValue(schema json.RawMessage, value json.RawMessage) error {
	compiled, err := compile(schema)
	if err != nil {
		return err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(value))
	if err != nil {
		return fmt.Errorf("decode resource value: %w", err)
	}
	if err := compiled.Validate(instance); err != nil {
		return fmt.Errorf("resource value does not match its type: %w", err)
	}
	return nil
}

func compile(schema json.RawMessage) (*jsonschema.Schema, error) {
	if !json.Valid(schema) {
		return nil, fmt.Errorf("resource type schema is not valid JSON")
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema))
	if err != nil {
		return nil, fmt.Errorf("decode resource type schema: %w", err)
	}
	const resourceURL = "urn:windforce:resource-type"
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(resourceURL, document); err != nil {
		return nil, fmt.Errorf("register resource type schema: %w", err)
	}
	compiled, err := compiler.Compile(resourceURL)
	if err != nil {
		return nil, fmt.Errorf("compile resource type schema: %w", err)
	}
	return compiled, nil
}
