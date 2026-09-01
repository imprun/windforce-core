package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpaqueHTTPProjectionOpenAPIIsStrictAndSanitized(t *testing.T) {
	t.Parallel()

	document := buildControlPlaneOpenAPI("http://core.invalid", "workspace-a")
	paths := mustOpenAPIMap(t, document, "paths")
	expected := map[string]string{
		"/api/w/{workspace}/opaque-http-projections/credential-snapshots":   "post",
		"/api/w/{workspace}/opaque-http-projections/credential-revocations": "post",
		"/api/w/{workspace}/opaque-http-projections/publication-revisions":  "post",
		"/api/w/{workspace}/opaque-http-projections/activations":            "post",
		"/api/w/{workspace}/opaque-http-projections/audit":                  "get",
		"/api/w/{workspace}/opaque-http-projections/retention/prune":        "post",
	}
	for path, method := range expected {
		item := mustOpenAPIMap(t, paths, path)
		operation := mustOpenAPIMap(t, item, method)
		if strings.TrimSpace(operation["operationId"].(string)) == "" {
			t.Fatalf("%s %s has no operationId", method, path)
		}
		responses := mustOpenAPIMap(t, operation, "responses")
		if _, ok := responses["200"]; !ok {
			t.Fatalf("%s %s has no 200 response", method, path)
		}
		if method == "post" {
			if _, ok := responses["201"]; !ok {
				t.Fatalf("%s %s has no 201 response", method, path)
			}
			for _, status := range []string{"400", "401", "403", "404", "409", "415", "422", "500"} {
				response, ok := responses[status]
				if !ok {
					t.Fatalf("%s %s has no %s response", method, path, status)
				}
				raw, _ := json.Marshal(response)
				if strings.Contains(string(raw), `"$ref":"#/components/responses/"`) {
					t.Fatalf("%s %s has an empty response component ref for %s", method, path, status)
				}
			}
		}
	}

	components := mustOpenAPIMap(t, document, "components")
	schemas := mustOpenAPIMap(t, components, "schemas")
	names := []string{
		"OpaqueIngressCredentialSnapshotRequest",
		"OpaqueIngressCredentialRevocationRequest",
		"OpaqueIngressPublicationRevisionRequest",
		"OpaqueIngressActivationRequest",
		"OpaqueIngressRetentionRequest",
		"OpaqueIngressCredentialSnapshot",
		"OpaqueIngressCredentialRevocation",
		"OpaqueIngressPublicationRevision",
		"OpaqueIngressActivation",
		"OpaqueIngressAudit",
		"OpaqueIngressRetentionResult",
	}
	for _, name := range names {
		schema := mustOpenAPIMap(t, schemas, name)
		if additional, ok := schema["additionalProperties"].(bool); !ok || additional {
			t.Fatalf("schema %s must set additionalProperties=false", name)
		}
	}

	for _, name := range []string{
		"OpaqueIngressCredentialSnapshotRequest",
		"OpaqueIngressCredentialRevocationRequest",
		"OpaqueIngressPublicationRevisionRequest",
		"OpaqueIngressActivationRequest",
		"OpaqueIngressRetentionRequest",
	} {
		raw, err := json.Marshal(schemas[name])
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"actor", "requestFingerprint", "request_fingerprint", "reason", "authorizedTarget", "token", "secret", "privateKey", "aes"} {
			if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(forbidden)) {
				t.Fatalf("request schema %s exposes forbidden field %q: %s", name, forbidden, raw)
			}
		}
	}

	for _, name := range []string{
		"OpaqueIngressCredentialSnapshot",
		"OpaqueIngressCredentialRevocation",
		"OpaqueIngressPublicationRevision",
		"OpaqueIngressActivation",
		"OpaqueIngressAudit",
		"OpaqueIngressRetentionResult",
	} {
		raw, err := json.Marshal(schemas[name])
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"requestFingerprint", "reason", "authorizedTarget", "detail", "token", "secret", "privateKey", "aes"} {
			if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(forbidden)) {
				t.Fatalf("response schema %s exposes forbidden field %q: %s", name, forbidden, raw)
			}
		}
	}

	for _, name := range []string{"OpaqueIngressCredentialSnapshotRequest", "OpaqueIngressPublicationRevisionRequest"} {
		schema := mustOpenAPIMap(t, schemas, name)
		digestContract := mustOpenAPIMap(t, schema, "x-windforce-content-digest")
		if digestContract["canonicalization"] != "projection-ascii-subset/v1" || digestContract["workspaceSource"] != "path" {
			t.Fatalf("schema %s has an incomplete portable digest contract: %#v", name, digestContract)
		}
	}
	publicationRequest := mustOpenAPIMap(t, schemas, "OpaqueIngressPublicationRevisionRequest")
	publicationProperties := mustOpenAPIMap(t, publicationRequest, "properties")
	httpSchema := mustOpenAPIMap(t, publicationProperties, "http")
	httpProperties := mustOpenAPIMap(t, httpSchema, "properties")
	methodSchema := mustOpenAPIMap(t, httpProperties, "method")
	if methodSchema["maxLength"] != 32 {
		t.Fatalf("HTTP method maxLength = %#v, want 32", methodSchema["maxLength"])
	}
	responsePolicy := mustOpenAPIMap(t, httpProperties, "responsePolicy")
	responseProperties := mustOpenAPIMap(t, responsePolicy, "properties")
	maxBodyBytes := mustOpenAPIMap(t, responseProperties, "maxBodyBytes")
	if maxBodyBytes["maximum"] != 7<<20 {
		t.Fatalf("response maxBodyBytes maximum = %#v, want %d", maxBodyBytes["maximum"], 7<<20)
	}
}

func mustOpenAPIMap(t *testing.T, value map[string]any, key string) map[string]any {
	t.Helper()
	result, ok := value[key].(map[string]any)
	if !ok {
		t.Fatalf("OpenAPI key %q = %#v, want object", key, value[key])
	}
	return result
}
