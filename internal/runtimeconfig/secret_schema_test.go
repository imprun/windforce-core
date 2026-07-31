package runtimeconfig

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/imprun/windforce-core/internal/state"
)

func TestValidateSecretReferencesRequiresSecretVariable(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(t.TempDir() + "/state.json")
	if err := store.SetVariable(ctx, "ws-a", "shop", "secrets/token", "ciphertext", true, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SetVariable(ctx, "ws-a", "shop", "plain/token", "visible", false, ""); err != nil {
		t.Fatal(err)
	}
	resolver := New(store, nil)
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{"credentials":{"$ref":"#/$defs/credentials"}},
		"$defs":{"credentials":{"type":"object","properties":{"token":{"type":"string","writeOnly":true}}}}
	}`)

	paths, err := resolver.ValidateSecretReferences(ctx, "ws-a", "shop", schema, json.RawMessage(`{"credentials":{"token":"$var:secrets/token"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "secrets/token" {
		t.Fatalf("paths = %#v", paths)
	}

	for _, input := range []string{
		`{"credentials":{"token":"plaintext"}}`,
		`{"credentials":{"token":"$res:credentials/api"}}`,
		`{"credentials":{"token":"$var:plain/token"}}`,
		`{"credentials":{"token":"$var:missing/token"}}`,
	} {
		if _, err := resolver.ValidateSecretReferences(ctx, "ws-a", "shop", schema, json.RawMessage(input)); err == nil {
			t.Fatalf("ValidateSecretReferences(%s) succeeded", input)
		}
	}
}

func TestValidateSecretReferencesSupportsExplicitAnnotationAndArrays(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(t.TempDir() + "/state.json")
	if err := store.SetVariable(ctx, "ws-a", "", "shared/password", "ciphertext", true, ""); err != nil {
		t.Fatal(err)
	}
	resolver := New(store, nil)
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{"passwords":{"type":"array","items":{"type":"string","x-windforce-secret":true}}}
	}`)
	paths, err := resolver.ValidateSecretReferences(ctx, "ws-a", "shop", schema, json.RawMessage(`{"passwords":["$var:shared/password"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(paths, ",") != "shared/password" {
		t.Fatalf("paths = %#v", paths)
	}
}
