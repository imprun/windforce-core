package opaquehttp

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/attestation"
	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/execution"
	"github.com/imprun/windforce-core/internal/state"
)

const testAttestationAudience = "synthetic-capability-service"

// admitOpaqueInvocation drives one trusted delivery through the handler against
// a real AdmissionService and returns the admitted Job. No worker completes the
// Run, so the delivery ends in the bounded deadline response; the evidence is
// the durable Job payload.
func admitOpaqueInvocation(t *testing.T, options ...execution.AdmissionOption) state.Job {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	deploymentID := testDeploymentID
	if _, err := store.PublishRelease(ctx, contract.Deployment{
		Workspace:    testWorkspace,
		GitSourceID:  "synthetic-source",
		APIVersion:   contract.AppManifestV2,
		App:          testApp,
		Commit:       testCommit,
		DeploymentID: &deploymentID,
		BundleDigest: testBundleDigest,
		ObjectURI:    "bundle://synthetic/source/commit",
		Actions: map[string]contract.Action{
			testAction: {
				Action:           testAction,
				InputSchemaBody:  contractFixture(t, "opaque-http-app-input.schema.json"),
				OutputSchemaBody: contractFixture(t, "application-wire-response.schema.json"),
			},
		},
	}, time.Now().UTC()); err != nil {
		t.Fatalf("publish release: %v", err)
	}

	admission := execution.NewAdmissionService(store, store, nil, options...)
	handler := mustHandler(t, resolverFunc(func(context.Context, ResolutionRequest) (ResolvedAdmission, error) {
		return validResolvedAdmission(), nil
	}), admission, 2*time.Second)
	envelope := invocationEnvelope(t, func(invocation *OpaqueHTTPInvocationV1) {
		invocation.ReceivedAt = time.Now().UTC()
		invocation.DeadlineAt = invocation.ReceivedAt.Add(300 * time.Millisecond)
	})
	request := httptest.NewRequest(http.MethodPost, "http://internal.invalid/conformance", bytes.NewReader(envelope))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(httptest.NewRecorder(), request)

	snapshot, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if len(snapshot.Jobs) != 1 {
		t.Fatalf("admitted Jobs = %d, want one", len(snapshot.Jobs))
	}
	for _, job := range snapshot.Jobs {
		return job
	}
	return state.Job{}
}

func testAttestationIssuer(t *testing.T) *attestation.Issuer {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate issuer key: %v", err)
	}
	issuer, err := attestation.NewIssuer("synthetic-issuer-key-1", testAttestationAudience, time.Minute, private)
	if err != nil {
		t.Fatalf("new issuer: %v", err)
	}
	return issuer
}

func TestOpaqueAdmissionBindsTheAttestationToTheResolvedReferences(t *testing.T) {
	issuer := testAttestationIssuer(t)
	job := admitOpaqueInvocation(t, execution.WithExecutionAttestationIssuer(issuer))

	minted := job.Payload.ExecutionAttestation
	if minted == nil {
		t.Fatal("an admitted opaque-ingress Run carries no execution attestation")
	}
	resolved := validResolvedAdmission()
	expected := contract.ExecutionAttestationBinding{
		RunRef:          job.RunID,
		Workspace:       testWorkspace,
		App:             testApp,
		Action:          testAction,
		PublicationRef:  resolved.InvocationPins.PublicationRef,
		RouteGeneration: resolved.InvocationPins.RouteGeneration,
		OperationRef:    resolved.InvocationPins.OperationRef,
		CredentialRef:   resolved.InvocationPins.CredentialRef,
		Release: contract.ExecutionReleasePin{
			DeploymentID: testDeploymentID,
			Commit:       testCommit,
			BundleDigest: testBundleDigest,
		},
		References: resolved.InvocationPins.References,
	}
	if err := attestation.Verify(*minted, issuer.PublicKey(), time.Now().UTC(), attestation.Expectation{
		Audience: testAttestationAudience,
		Binding:  expected,
	}); err != nil {
		t.Fatalf("the attestation does not bind the resolved references: %v", err)
	}

	// A verifier that expects one different resolved reference must reject it.
	drifted := expected
	drifted.RouteGeneration = resolved.InvocationPins.RouteGeneration + 1
	if err := attestation.Verify(*minted, issuer.PublicKey(), time.Now().UTC(), attestation.Expectation{
		Audience: testAttestationAudience,
		Binding:  drifted,
	}); err == nil {
		t.Fatal("a drifted route generation was accepted")
	}
}

func TestAdmissionMintsNoAttestationWithoutAnIssuer(t *testing.T) {
	job := admitOpaqueInvocation(t)
	if job.Payload.ExecutionAttestation != nil {
		t.Fatalf("an unconfigured deployment minted an attestation: %+v", job.Payload.ExecutionAttestation)
	}
	encoded, err := json.Marshal(job.Payload)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	if bytes.Contains(encoded, []byte("executionAttestation")) {
		t.Fatalf("an absent attestation still appears in the payload: %s", encoded)
	}
}
