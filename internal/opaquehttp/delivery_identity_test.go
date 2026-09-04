package opaquehttp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/execution"
	"github.com/imprun/windforce-core/internal/state"
)

const syntheticDeliveryID = "synthetic-delivery-0001"

// recordingAdmission delegates to a real AdmissionService while recording every
// outcome, so a test can prove which delivery created the Run and which
// replayed it.
type recordingAdmission struct {
	inner *execution.AdmissionService

	mu       sync.Mutex
	requests []execution.CreateRunRequest
	results  []execution.Admission
}

func (a *recordingAdmission) CreateRun(ctx context.Context, request execution.CreateRunRequest) (execution.Admission, error) {
	admitted, err := a.inner.CreateRun(ctx, request)
	a.mu.Lock()
	a.requests = append(a.requests, request)
	if err == nil {
		a.results = append(a.results, admitted)
	}
	a.mu.Unlock()
	return admitted, err
}

func (a *recordingAdmission) GetRunForPrincipal(ctx context.Context, principal execution.Principal, workspace string, runID string) (state.Run, error) {
	return a.inner.GetRunForPrincipal(ctx, principal, workspace, runID)
}

func (a *recordingAdmission) snapshot() ([]execution.CreateRunRequest, []execution.Admission) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]execution.CreateRunRequest(nil), a.requests...), append([]execution.Admission(nil), a.results...)
}

func TestEnvelopeRequiresTrustedDeliveryIdentity(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		envelope func(*testing.T) []byte
	}{
		{
			name: "absent",
			envelope: func(t *testing.T) []byte {
				return envelopeWithoutDeliveryIdentity(t)
			},
		},
		{
			name: "empty",
			envelope: func(t *testing.T) []byte {
				return invocationEnvelope(t, func(invocation *OpaqueHTTPInvocationV1) {
					invocation.TrustedIngress.DeliveryID = ""
				})
			},
		},
		{
			name: "surrounding whitespace",
			envelope: func(t *testing.T) []byte {
				return invocationEnvelope(t, func(invocation *OpaqueHTTPInvocationV1) {
					invocation.TrustedIngress.DeliveryID = " " + syntheticDeliveryID + " "
				})
			},
		},
		{
			name: "control character",
			envelope: func(t *testing.T) []byte {
				return invocationEnvelope(t, func(invocation *OpaqueHTTPInvocationV1) {
					invocation.TrustedIngress.DeliveryID = "synthetic\ndelivery"
				})
			},
		},
		{
			name: "above the length bound",
			envelope: func(t *testing.T) []byte {
				return invocationEnvelope(t, func(invocation *OpaqueHTTPInvocationV1) {
					invocation.TrustedIngress.DeliveryID = strings.Repeat("d", 201)
				})
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			admission := &admissionFake{}
			handler := mustHandler(t, resolverFunc(func(context.Context, ResolutionRequest) (ResolvedAdmission, error) {
				t.Error("an envelope without a usable delivery identity reached the Resolver")
				return validResolvedAdmission(), nil
			}), admission, 2*time.Second)

			request := httptest.NewRequest(http.MethodPost, "http://internal.invalid/conformance", bytes.NewReader(test.envelope(t)))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
			}
			assertPlatformFailureCategory(t, response.Body.Bytes(), FailureApplicationProtocolViolation)
			if createCalls, _ := admission.counts(); createCalls != 0 {
				t.Fatalf("CreateRun calls = %d, want zero", createCalls)
			}
		})
	}
}

func TestDeliveryIdentityBindsTheAdmissionIdempotencyKey(t *testing.T) {
	t.Parallel()

	keyFor := func(t *testing.T, mutate func(*OpaqueHTTPInvocationV1)) string {
		t.Helper()
		var captured execution.CreateRunRequest
		admission := &admissionFake{create: func(_ context.Context, request execution.CreateRunRequest) (execution.Admission, error) {
			captured = request
			return execution.Admission{}, &execution.Fault{Kind: execution.FaultUnavailable, Message: "synthetic stop"}
		}}
		handler := mustHandler(t, resolverFunc(func(_ context.Context, request ResolutionRequest) (ResolvedAdmission, error) {
			// The Resolver pins whatever route the trusted boundary presented, so
			// the key derivation is what varies between cases.
			resolved := validResolvedAdmission()
			resolved.InvocationPins.PublicationRef = request.TrustedIngress.PublicationRef
			resolved.InvocationPins.RouteGeneration = request.TrustedIngress.RouteGeneration
			resolved.InvocationPins.CredentialRef = contract.ImmutableReference{
				ID:      request.TrustedIngress.CredentialRef.ID,
				Version: request.TrustedIngress.CredentialRef.Revision,
			}
			return resolved, nil
		}), admission, 2*time.Second)
		request := httptest.NewRequest(http.MethodPost, "http://internal.invalid/conformance", bytes.NewReader(invocationEnvelope(t, mutate)))
		request.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(httptest.NewRecorder(), request)
		if captured.IdempotencyKey == "" {
			t.Fatal("Admission received no idempotency key for a trusted delivery")
		}
		return captured.IdempotencyKey
	}

	base := keyFor(t, nil)
	if !strings.HasPrefix(base, "opaque-http-delivery:") {
		t.Fatalf("idempotency key = %q, want the opaque delivery prefix", base)
	}
	if strings.Contains(base, syntheticDeliveryID) {
		t.Fatalf("idempotency key %q carries the raw delivery identity", base)
	}
	if again := keyFor(t, nil); again != base {
		t.Fatalf("the same delivery produced two keys: %q and %q", base, again)
	}

	for _, test := range []struct {
		name   string
		mutate func(*OpaqueHTTPInvocationV1)
	}{
		{
			name: "another delivery identity",
			mutate: func(invocation *OpaqueHTTPInvocationV1) {
				invocation.TrustedIngress.DeliveryID = "synthetic-delivery-0002"
			},
		},
		{
			name: "another route generation",
			mutate: func(invocation *OpaqueHTTPInvocationV1) {
				invocation.TrustedIngress.RouteGeneration = 8
			},
		},
		{
			name: "another credential snapshot",
			mutate: func(invocation *OpaqueHTTPInvocationV1) {
				invocation.TrustedIngress.CredentialRef.Revision = "sha256:" + strings.Repeat("b", 64)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := keyFor(t, test.mutate); got == base {
				t.Fatalf("%s produced the same admission identity %q", test.name, got)
			}
		})
	}
}

func TestConcurrentIdenticalDeliveriesConvergeOnOneRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	publishDeliveryTestRelease(ctx, t, store)
	admission := &recordingAdmission{inner: execution.NewAdmissionService(store, store, nil)}
	handler := mustHandler(t, resolverFunc(func(context.Context, ResolutionRequest) (ResolvedAdmission, error) {
		return validResolvedAdmission(), nil
	}), admission, 2*time.Second)

	// No worker completes the Run, so every delivery ends in the bounded
	// deadline response. The evidence is the durable state, not the status.
	envelope := invocationEnvelope(t, shortTrustedDeadline)
	var waitGroup sync.WaitGroup
	for range 4 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			request := httptest.NewRequest(http.MethodPost, "http://internal.invalid/conformance", bytes.NewReader(envelope))
			request.Header.Set("Content-Type", "application/json")
			handler.ServeHTTP(httptest.NewRecorder(), request)
		}()
	}
	waitGroup.Wait()

	snapshot, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if len(snapshot.Runs) != 1 || len(snapshot.Jobs) != 1 {
		t.Fatalf("concurrent identical deliveries created Runs=%d Jobs=%d, want one pair", len(snapshot.Runs), len(snapshot.Jobs))
	}
	requests, results := admission.snapshot()
	if len(requests) != 4 || len(results) == 0 {
		t.Fatalf("admission attempts = %d, successful results = %d", len(requests), len(results))
	}
	created := 0
	for _, result := range results {
		if result.Run.ID != results[0].Run.ID {
			t.Fatalf("deliveries resolved to different Runs: %q and %q", results[0].Run.ID, result.Run.ID)
		}
		if !result.Replayed {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("non-replayed admissions = %d, want exactly one", created)
	}

	for _, run := range snapshot.Runs {
		if run.IdempotencyHash == "" {
			t.Fatal("the admitted Run recorded no idempotency identity")
		}
		if strings.Contains(run.IdempotencyHash, syntheticDeliveryID) {
			t.Fatalf("durable state carries the raw delivery identity: %q", run.IdempotencyHash)
		}
		if strings.Contains(string(run.Input), "deliveryId") {
			t.Fatalf("the App input carries the delivery identity: %s", run.Input)
		}
	}
}

func TestSameDeliveryIdentityWithAnotherPayloadIsAConflict(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	publishDeliveryTestRelease(ctx, t, store)
	admission := &recordingAdmission{inner: execution.NewAdmissionService(store, store, nil)}
	handler := mustHandler(t, resolverFunc(func(context.Context, ResolutionRequest) (ResolvedAdmission, error) {
		return validResolvedAdmission(), nil
	}), admission, 2*time.Second)

	deliver := func(envelope []byte) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "http://internal.invalid/conformance", bytes.NewReader(envelope))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	deliver(invocationEnvelope(t, shortTrustedDeadline))
	replay := deliver(invocationEnvelope(t, shortTrustedDeadline))
	if replay.Code == http.StatusConflict {
		t.Fatalf("an identical redelivery must replay, not conflict: %s", replay.Body.String())
	}

	other := []byte{9, 8, 7}
	digest := sha256.Sum256(other)
	conflict := deliver(invocationEnvelope(t, func(invocation *OpaqueHTTPInvocationV1) {
		shortTrustedDeadline(invocation)
		invocation.Body.Data = base64.StdEncoding.EncodeToString(other)
		invocation.Body.ByteLength = int64(len(other))
		invocation.Body.Digest = "sha256:" + hex.EncodeToString(digest[:])
	}))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", conflict.Code, http.StatusConflict, conflict.Body.String())
	}
	assertPlatformFailureCategory(t, conflict.Body.Bytes(), FailureApplicationProtocolViolation)

	snapshot, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if len(snapshot.Runs) != 1 || len(snapshot.Jobs) != 1 {
		t.Fatalf("a conflicting redelivery changed durable state: Runs=%d Jobs=%d", len(snapshot.Runs), len(snapshot.Jobs))
	}
}

func publishDeliveryTestRelease(ctx context.Context, t *testing.T, store *state.LocalStore) {
	t.Helper()
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
}

// envelopeWithoutDeliveryIdentity removes the field instead of emptying it, so
// the strict envelope decoder sees an absent member.
func envelopeWithoutDeliveryIdentity(t *testing.T) []byte {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(invocationEnvelope(t, nil), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	trusted, ok := envelope["trustedIngress"].(map[string]any)
	if !ok {
		t.Fatal("envelope fixture has no trustedIngress object")
	}
	delete(trusted, "deliveryId")
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("encode envelope: %v", err)
	}
	return raw
}

// shortTrustedDeadline keeps the declared wait budget inside the handler limit
// while ending the synchronous wait quickly: no worker completes these Runs.
func shortTrustedDeadline(invocation *OpaqueHTTPInvocationV1) {
	invocation.ReceivedAt = time.Now().UTC()
	invocation.DeadlineAt = invocation.ReceivedAt.Add(300 * time.Millisecond)
}
