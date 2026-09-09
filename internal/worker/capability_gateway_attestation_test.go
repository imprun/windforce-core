package worker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

func testExecutionAttestation() *contract.ExecutionAttestation {
	return &contract.ExecutionAttestation{
		Kind: contract.ExecutionAttestationKindV1,
		Binding: contract.ExecutionAttestationBinding{
			Kind:            contract.ExecutionAttestationBindingKindV1,
			Audience:        "capability-service",
			IssuerKeyID:     "key-1",
			ExpiresAt:       "2030-01-01T00:00:00Z",
			RunRef:          "run-456",
			Workspace:       "workspace-1",
			App:             "app_1",
			Action:          "verify",
			PublicationRef:  "publication-1",
			RouteGeneration: 7,
			OperationRef:    "operation/verify",
			CredentialRef:   contract.ImmutableReference{ID: "credential/a", Version: "sha256:aa"},
			Release:         contract.ExecutionReleasePin{DeploymentID: "d1", Commit: "c1", BundleDigest: "sha256:bb"},
			References: []contract.NamedImmutableReferencePin{
				{Name: "epoch", Reference: contract.ImmutableReference{ID: "epoch", Version: "3"}},
			},
		},
		BindingDigest: "sha256:cc",
		Algorithm:     contract.ExecutionAttestationAlgorithm,
		Signature:     "signature-value",
	}
}

// capabilityRunBodyRecorder serves one gateway run and keeps the request body.
func capabilityRunBodyRecorder(t *testing.T, body *[]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/runs":
			read, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read run request: %v", err)
			}
			*body = read
			writeCapabilityTestJSON(t, w, http.StatusCreated, map[string]any{
				"runRef": "run-123", "runToken": "run-secret", "expiresInSeconds": 300,
			})
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
}

// TestCapabilityGatewayRunCarriesTheMintedAttestation proves the attestation
// reaches the gateway byte-identical to what Admission minted, and that it does
// not follow the same path into App input.
func TestCapabilityGatewayRunCarriesTheMintedAttestation(t *testing.T) {
	var body []byte
	server := capabilityRunBodyRecorder(t, &body)
	defer server.Close()

	attestation := testExecutionAttestation()
	binding := CapabilityGatewayBinding{
		ServiceURL:   server.URL,
		WorkerToken:  "worker-secret",
		Timeout:      time.Second,
		Labels:       []string{"document.pdf.v1"},
		Capabilities: []string{"document.pdf/v1"},
		client:       newCapabilityGatewayHTTPClient(time.Second),
	}
	bindings := RuntimeBindings{CapabilityGateway: binding}
	result, err := bindings.Bind(
		context.Background(),
		json.RawMessage(`{"region":"kr"}`),
		RuntimeBindingContext{RunID: "run-456", JobID: "job-789", Attempt: 1, Attestation: attestation},
		[]string{"document.pdf.v1"},
		5*time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}

	var request struct {
		TTLSeconds           uint64          `json:"ttlSeconds"`
		ExecutionAttestation json.RawMessage `json:"executionAttestation"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("decode run request: %v", err)
	}
	minted, err := json.Marshal(attestation)
	if err != nil {
		t.Fatal(err)
	}
	if string(request.ExecutionAttestation) != string(minted) {
		t.Fatalf("attestation on the wire is not byte-identical\n got: %s\nwant: %s", request.ExecutionAttestation, minted)
	}
	if request.TTLSeconds != 360 {
		t.Fatalf("ttlSeconds = %d", request.TTLSeconds)
	}

	// The App must never receive it: a capability service that trusted an
	// App-supplied attestation would be trusting the code it protects.
	for _, secret := range []string{attestation.Signature, attestation.BindingDigest, attestation.Binding.IssuerKeyID} {
		if strings.Contains(string(result.Input), secret) {
			t.Fatalf("attestation value %q reached App input: %s", secret, result.Input)
		}
	}
	if err := result.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestCapabilityGatewayRunWithoutAttestationIsUnchanged proves a deployment
// that mints none opens a run indistinguishable from one opened before this
// field existed — the check a gateway that rejects unknown fields depends on.
func TestCapabilityGatewayRunWithoutAttestationIsUnchanged(t *testing.T) {
	var body []byte
	server := capabilityRunBodyRecorder(t, &body)
	defer server.Close()

	binding := CapabilityGatewayBinding{
		ServiceURL:  server.URL,
		WorkerToken: "worker-secret",
		Timeout:     time.Second,
		client:      newCapabilityGatewayHTTPClient(time.Second),
	}
	if _, err := binding.open(
		context.Background(),
		RuntimeBindingContext{RunID: "run-456", JobID: "job-789", Attempt: 1},
		5*time.Minute,
	); err != nil {
		t.Fatal(err)
	}
	if got, want := string(body), `{"ttlSeconds":360}`; got != want {
		t.Fatalf("run request = %s, want %s", got, want)
	}
}

// TestCapabilityGatewaySessionKeepsNoAttestation proves the attestation is not
// retained past the open call, so closing the run is all it takes to be rid of
// it on this side.
func TestCapabilityGatewaySessionKeepsNoAttestation(t *testing.T) {
	var body []byte
	server := capabilityRunBodyRecorder(t, &body)
	defer server.Close()

	binding := CapabilityGatewayBinding{
		ServiceURL:  server.URL,
		WorkerToken: "worker-secret",
		Timeout:     time.Second,
		client:      newCapabilityGatewayHTTPClient(time.Second),
	}
	attestation := testExecutionAttestation()
	session, err := binding.open(
		context.Background(),
		RuntimeBindingContext{RunID: "run-456", JobID: "job-789", Attempt: 1, Attestation: attestation},
		5*time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), attestation.Signature) {
		t.Fatalf("session retained the attestation: %s", encoded)
	}
}
