package opaquehttp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/execution"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestContractExamplesMatchSchemasAndRuntimeValidation(t *testing.T) {
	t.Parallel()

	invocationRaw := contractFixture(t, "opaque-http-invocation.example.json")
	invocationSchema := contractFixture(t, "opaque-http-invocation.schema.json")
	appInputRaw := contractFixture(t, "opaque-http-app-input.example.json")
	appInputSchema := contractFixture(t, "opaque-http-app-input.schema.json")
	responseRaw := contractFixture(t, "application-wire-response.example.json")
	responseSchema := contractFixture(t, "application-wire-response.schema.json")

	reader := execution.NewSchemaReader(context.Background(), nil, contract.Deployment{})
	for _, item := range []struct {
		name    string
		schema  json.RawMessage
		fixture json.RawMessage
	}{
		{name: "invocation", schema: invocationSchema, fixture: invocationRaw},
		{name: "App input", schema: appInputSchema, fixture: appInputRaw},
		{name: "application response", schema: responseSchema, fixture: responseRaw},
	} {
		t.Run(item.name, func(t *testing.T) {
			t.Parallel()
			if err := reader.Validate("", item.schema, item.fixture); err != nil {
				t.Fatalf("validate fixture against schema: %v", err)
			}
		})
	}

	invocation, decodedRequest, err := decodeInvocation(bytes.NewReader(invocationRaw), int64(len(invocationRaw)+1), MaxWireBodyBytes)
	if err != nil {
		t.Fatalf("decode invocation: %v", err)
	}
	if want := []byte{0, 1, 2, 127, 128, 255, 65, 66, 67, 10}; !bytes.Equal(decodedRequest, want) {
		t.Fatalf("decoded request bytes = %v, want %v", decodedRequest, want)
	}
	encodedAppInput, err := encodeAppInput(invocation)
	if err != nil {
		t.Fatalf("encode App input: %v", err)
	}
	assertJSONEqual(t, encodedAppInput, appInputRaw)

	_, decodedResponse, err := decodeApplicationWireResponse(responseRaw, MaxWireBodyBytes)
	if err != nil {
		t.Fatalf("decode application wire response: %v", err)
	}
	if want := []byte{255, 0, 16, 32, 123, 125, 10}; !bytes.Equal(decodedResponse, want) {
		t.Fatalf("decoded response bytes = %v, want %v", decodedResponse, want)
	}
}

func TestMaximumApplicationWireResponseFitsWorkerPlaneCompletionBudget(t *testing.T) {
	body := bytes.Repeat([]byte{0xa5}, int(contract.MaxApplicationWireResponseBodyBytes))
	digest := sha256.Sum256(body)
	wireResponse, err := json.Marshal(ApplicationWireResponseV1{
		Kind:    ApplicationWireResponseKindV1,
		Status:  200,
		Headers: []ResponseHeaderV1{{Name: "content-type", Value: "application/octet-stream"}},
		Body: BodyBytesV1{
			Encoding:   RFC4648Base64Encoding,
			Data:       base64.StdEncoding.EncodeToString(body),
			ByteLength: int64(len(body)),
			Digest:     fmt.Sprintf("sha256:%x", digest),
		},
	})
	if err != nil {
		t.Fatalf("marshal maximum wire response: %v", err)
	}
	if _, decoded, err := decodeApplicationWireResponse(wireResponse, contract.MaxApplicationWireResponseBodyBytes); err != nil || !bytes.Equal(decoded, body) {
		t.Fatalf("validate maximum wire response: decoded=%d err=%v", len(decoded), err)
	}
	completion, err := json.Marshal(map[string]any{
		"lease": map[string]any{
			"job_id": "job-synthetic", "worker_id": "worker-synthetic", "attempt": 1,
			"acquired_at": "2026-01-01T00:00:00Z", "expires_at": "2026-01-01T00:01:00Z",
		},
		"outcome": "succeeded",
		"result": contract.JobResult{
			JobID: "job-synthetic", App: testApp, Action: testAction, Output: wireResponse,
		},
	})
	if err != nil {
		t.Fatalf("marshal Worker Plane completion: %v", err)
	}
	if int64(len(completion)) >= contract.WorkerPlaneMaxRequestBytes {
		t.Fatalf("completion bytes = %d, Worker Plane limit = %d", len(completion), contract.WorkerPlaneMaxRequestBytes)
	}
}

func TestExecutionOutcomeSchemaResolvesSiblingWireResponseSchema(t *testing.T) {
	t.Parallel()

	const (
		responseSchemaURL = "https://raw.githubusercontent.com/imprun/windforce-core/main/contracts/opaque-http/v1/application-wire-response.schema.json"
		outcomeSchemaURL  = "https://raw.githubusercontent.com/imprun/windforce-core/main/contracts/opaque-http/v1/execution-outcome.schema.json"
	)
	compiler := jsonschema.NewCompiler()
	for _, resource := range []struct {
		url string
		raw json.RawMessage
	}{
		{url: responseSchemaURL, raw: contractFixture(t, "application-wire-response.schema.json")},
		{url: outcomeSchemaURL, raw: contractFixture(t, "execution-outcome.schema.json")},
	} {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(resource.raw))
		if err != nil {
			t.Fatalf("decode schema %s: %v", resource.url, err)
		}
		if err := compiler.AddResource(resource.url, document); err != nil {
			t.Fatalf("add schema %s: %v", resource.url, err)
		}
	}
	compiled, err := compiler.Compile(outcomeSchemaURL)
	if err != nil {
		t.Fatalf("compile execution outcome schema: %v", err)
	}
	completed := append([]byte(`{"kind":"windforce.execution-outcome/v1","outcome":"completed","response":`), contractFixture(t, "application-wire-response.example.json")...)
	completed = append(completed, '}')
	for _, fixture := range []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "completed", raw: completed},
		{name: "platform failed", raw: contractFixture(t, "platform-failed.example.json")},
	} {
		instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(fixture.raw))
		if err != nil {
			t.Fatalf("decode %s outcome: %v", fixture.name, err)
		}
		if err := compiled.Validate(instance); err != nil {
			t.Fatalf("%s outcome does not match schema: %v", fixture.name, err)
		}
	}
}

func TestPlatformFailedExampleKeepsPostAdmissionRetryDisabled(t *testing.T) {
	t.Parallel()

	var outcome ExecutionOutcomeV1
	if err := json.Unmarshal(contractFixture(t, "platform-failed.example.json"), &outcome); err != nil {
		t.Fatalf("decode platform-failed example: %v", err)
	}
	if outcome.Outcome != ExecutionOutcomePlatformFailed || outcome.Failure == nil {
		t.Fatalf("platform-failed example = %#v", outcome)
	}
	if outcome.Failure.Category != FailureWorkerLost || outcome.Failure.Retryable {
		t.Fatalf("platform-failed failure = %#v, want workerLost and non-retryable", outcome.Failure)
	}
}

func TestDecodeInvocationRejectsNonConformingEnvelope(t *testing.T) {
	t.Parallel()

	valid := contractFixture(t, "opaque-http-invocation.example.json")
	invalidLength := contractFixture(t, "opaque-http-invocation.invalid-length.example.json")
	unknown := bytes.Replace(valid, []byte(`"kind": "windforce.opaque-http-ingress-request/v1",`), []byte(`"kind": "windforce.opaque-http-ingress-request/v1", "unknown": true,`), 1)
	duplicate := bytes.Replace(valid, []byte(`"kind": "windforce.opaque-http-ingress-request/v1",`), []byte(`"kind": "windforce.opaque-http-ingress-request/v1", "kind": "windforce.opaque-http-ingress-request/v1",`), 1)
	badBase64 := bytes.Replace(valid, []byte(`"data": "AAECf4D/QUJDCg=="`), []byte(`"data": "AAECf4D_QUJDCg=="`), 1)
	badDigest := bytes.Replace(valid, []byte(`sha256:1b07ff65446a1e3d40ea19fffed12722cfc3762a0bc8f70ace978c13b1949ad1`), []byte(`sha256:0b07ff65446a1e3d40ea19fffed12722cfc3762a0bc8f70ace978c13b1949ad1`), 1)

	for _, item := range []struct {
		name string
		raw  []byte
	}{
		{name: "unknown member", raw: unknown},
		{name: "duplicate member", raw: duplicate},
		{name: "non-RFC4648 alphabet", raw: badBase64},
		{name: "length mismatch", raw: invalidLength},
		{name: "digest mismatch", raw: badDigest},
	} {
		t.Run(item.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := decodeInvocation(bytes.NewReader(item.raw), int64(len(item.raw)+1), MaxWireBodyBytes); err == nil {
				t.Fatal("decode invocation unexpectedly succeeded")
			}
		})
	}

	if _, _, err := decodeInvocation(bytes.NewReader(valid), int64(len(valid)-1), MaxWireBodyBytes); err == nil {
		t.Fatal("oversized envelope unexpectedly succeeded")
	}
	if _, _, err := decodeInvocation(bytes.NewReader(valid), int64(len(valid)+1), 9); err == nil {
		t.Fatal("oversized decoded body unexpectedly succeeded")
	}
}

func TestDuplicateMemberScannerRejectsExcessiveNesting(t *testing.T) {
	t.Parallel()

	raw := []byte(strings.Repeat("[", maxJSONNestingDepth+2) + "0" + strings.Repeat("]", maxJSONNestingDepth+2))
	if err := rejectDuplicateJSONMembers(raw); err == nil || !strings.Contains(err.Error(), "nesting depth") {
		t.Fatalf("error = %v, want nesting depth rejection", err)
	}
}

func TestDecodeInvocationRejectsNullEmptyBodyMetadata(t *testing.T) {
	t.Parallel()

	var invocation OpaqueHTTPInvocationV1
	if err := json.Unmarshal(contractFixture(t, "opaque-http-invocation.example.json"), &invocation); err != nil {
		t.Fatal(err)
	}
	invocation.Body.Data = ""
	invocation.Body.ByteLength = 0
	invocation.Body.Digest = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	raw, err := json.Marshal(invocation)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]byte{
		bytes.Replace(raw, []byte(`"data":""`), []byte(`"data":null`), 1),
		bytes.Replace(raw, []byte(`"byteLength":0`), []byte(`"byteLength":null`), 1),
	} {
		if _, _, err := decodeInvocation(bytes.NewReader(invalid), int64(len(invalid)+1), MaxWireBodyBytes); err == nil {
			t.Fatal("null empty-body metadata unexpectedly accepted")
		}
	}
}

func TestCanonicalEscapedPathValidation(t *testing.T) {
	t.Parallel()

	for _, valid := range []string{
		"/",
		"/synthetic/bytes",
		"/alpha:beta@example/value+one;two=three",
		"/utf8/%EA%B0%80",
	} {
		if err := validateCanonicalEscapedPath(valid); err != nil {
			t.Errorf("valid path %q rejected: %v", valid, err)
		}
	}
	for _, invalid := range []string{
		"",
		"//double",
		"/a//b",
		`/a\b`,
		"/a?query",
		"/a#fragment",
		"/%2E%2E/admin",
		"/%252Fadmin",
		"/%ZZ",
		"/a/./b",
		"/a/../b",
		"/trailing/",
		"/unnecessary%41escape",
		"/wildcard/*",
	} {
		if err := validateCanonicalEscapedPath(invalid); err == nil {
			t.Errorf("invalid path %q unexpectedly accepted", invalid)
		}
	}
}

func TestDecodeApplicationWireResponseRejectsProtocolViolations(t *testing.T) {
	t.Parallel()

	valid := contractFixture(t, "application-wire-response.example.json")
	for _, item := range []struct {
		name string
		raw  []byte
	}{
		{
			name: "unknown root member",
			raw:  bytes.Replace(valid, []byte(`"status": 200,`), []byte(`"status": 200, "unknown": true,`), 1),
		},
		{
			name: "duplicate root member",
			raw:  bytes.Replace(valid, []byte(`"status": 200,`), []byte(`"status": 200, "status": 201,`), 1),
		},
		{
			name: "null headers",
			raw: bytes.Replace(valid, []byte(`[
    {
      "name": "content-type",
      "value": "application/octet-stream"
    }
  ]`), []byte(`null`), 1),
		},
		{
			name: "non-allowlisted header",
			raw:  bytes.Replace(valid, []byte(`"name": "content-type"`), []byte(`"name": "x-provider"`), 1),
		},
		{
			name: "digest mismatch",
			raw:  bytes.Replace(valid, []byte(`sha256:ecb5152cfab1017c6fe09b823bdf0a7f938957c4b64ca66c0c7ed41e47074e60`), []byte(`sha256:0cb5152cfab1017c6fe09b823bdf0a7f938957c4b64ca66c0c7ed41e47074e60`), 1),
		},
		{
			name: "non-canonical Base64",
			raw:  bytes.Replace(valid, []byte(`"data": "/wAQIHt9Cg=="`), []byte(`"data": "/wAQIHt9Cg"`), 1),
		},
	} {
		t.Run(item.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := decodeApplicationWireResponse(item.raw, MaxWireBodyBytes); err == nil {
				t.Fatal("decode application wire response unexpectedly succeeded")
			}
		})
	}
}

func contractFixture(t *testing.T, name string) json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", "opaque-http", "v1", name))
	if err != nil {
		t.Fatalf("read contract fixture %s: %v", name, err)
	}
	return raw
}

func assertJSONEqual(t *testing.T, got, want []byte) {
	t.Helper()
	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("decode got JSON: %v", err)
	}
	var wantValue any
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("decode want JSON: %v", err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("JSON mismatch\ngot:  %s\nwant: %s", strings.TrimSpace(string(got)), strings.TrimSpace(string(want)))
	}
}
