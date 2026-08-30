package opaquehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/execution"
	"github.com/imprun/windforce-core/internal/state"
)

const (
	testWorkspace    = "synthetic-workspace"
	testApp          = "synthetic_app"
	testAction       = "invoke"
	testCommit       = "synthetic-commit-v1"
	testBundleDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testDeploymentID = "deployment-synthetic-v1"
)

type resolverFunc func(context.Context, OpaqueHTTPInvocationV1) (ResolvedAdmission, error)

func (f resolverFunc) ResolveOpaqueHTTPInvocation(ctx context.Context, invocation OpaqueHTTPInvocationV1) (ResolvedAdmission, error) {
	return f(ctx, invocation)
}

type admissionFake struct {
	mu          sync.Mutex
	createCalls int
	getCalls    int
	create      func(context.Context, execution.CreateRunRequest) (execution.Admission, error)
	get         func(context.Context, execution.Principal, string, string) (state.Run, error)
}

func TestResolvedAdmissionRequiresExactScopedServicePrincipal(t *testing.T) {
	t.Parallel()

	resolved := validResolvedAdmission()
	if _, err := prepareAdmissionRequest(resolved, []byte(`{}`)); err != nil {
		t.Fatalf("prepare valid Admission request: %v", err)
	}
	resolved.Principal.AllowedTargets = append(resolved.Principal.AllowedTargets, "other/action")
	if _, err := prepareAdmissionRequest(resolved, []byte(`{}`)); err == nil {
		t.Fatal("broader service principal unexpectedly accepted")
	}
}

func (f *admissionFake) CreateRun(ctx context.Context, request execution.CreateRunRequest) (execution.Admission, error) {
	f.mu.Lock()
	f.createCalls++
	f.mu.Unlock()
	if f.create == nil {
		return execution.Admission{}, errors.New("unexpected CreateRun")
	}
	return f.create(ctx, request)
}

func (f *admissionFake) GetRunForPrincipal(ctx context.Context, principal execution.Principal, workspace, runID string) (state.Run, error) {
	f.mu.Lock()
	f.getCalls++
	f.mu.Unlock()
	if f.get == nil {
		return state.Run{}, errors.New("unexpected GetRunForPrincipal")
	}
	return f.get(ctx, principal, workspace, runID)
}

func (f *admissionFake) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.createCalls, f.getCalls
}

func TestHandlerRejectsBeforeAdmission(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		outerMethod string
		outerType   string
		mutate      func(*OpaqueHTTPInvocationV1)
		raw         func(*testing.T) []byte
	}{
		{name: "outer method", outerMethod: http.MethodGet, outerType: "application/json"},
		{name: "outer content type", outerMethod: http.MethodPost, outerType: "application/octet-stream"},
		{
			name:        "inner method",
			outerMethod: http.MethodPost,
			outerType:   "application/json",
			mutate:      func(invocation *OpaqueHTTPInvocationV1) { invocation.HTTP.Method = "post" },
		},
		{
			name:        "inner path",
			outerMethod: http.MethodPost,
			outerType:   "application/json",
			mutate:      func(invocation *OpaqueHTTPInvocationV1) { invocation.HTTP.ExactEscapedPath = "/a/../b" },
		},
		{
			name:        "inner content type",
			outerMethod: http.MethodPost,
			outerType:   "application/json",
			mutate:      func(invocation *OpaqueHTTPInvocationV1) { invocation.HTTP.ContentType = "not a media type" },
		},
		{
			name:        "expired deadline",
			outerMethod: http.MethodPost,
			outerType:   "application/json",
			mutate: func(invocation *OpaqueHTTPInvocationV1) {
				invocation.ReceivedAt = time.Now().UTC().Add(-time.Second)
				invocation.DeadlineAt = time.Now().UTC().Add(-time.Millisecond)
			},
		},
		{
			name:        "body length mismatch",
			outerMethod: http.MethodPost,
			outerType:   "application/json",
			raw: func(t *testing.T) []byte {
				return contractFixture(t, "opaque-http-invocation.invalid-length.example.json")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			admission := &admissionFake{}
			resolverCalls := 0
			resolver := resolverFunc(func(context.Context, OpaqueHTTPInvocationV1) (ResolvedAdmission, error) {
				resolverCalls++
				return validResolvedAdmission(), nil
			})
			handler := mustHandler(t, resolver, admission, 2*time.Second)
			raw := invocationEnvelope(t, test.mutate)
			if test.raw != nil {
				raw = test.raw(t)
			}
			request := httptest.NewRequest(test.outerMethod, "http://internal.invalid/conformance", bytes.NewReader(raw))
			request.Header.Set("Content-Type", test.outerType)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code < 400 {
				t.Fatalf("status = %d, want platform failure", response.Code)
			}
			assertPlatformFailure(t, response.Body.Bytes())
			createCalls, _ := admission.counts()
			if createCalls != 0 {
				t.Fatalf("CreateRun calls = %d, want zero", createCalls)
			}
			if test.name == "outer method" || test.name == "outer content type" || test.name == "inner method" ||
				test.name == "inner path" || test.name == "inner content type" || test.name == "body length mismatch" || test.name == "expired deadline" {
				if resolverCalls != 0 {
					t.Fatalf("Resolver calls = %d, want zero", resolverCalls)
				}
			}
		})
	}
}

func TestHandlerResolverFencesRouteAndCredentialSnapshotsBeforeAdmission(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		mutate func(*OpaqueHTTPInvocationV1)
	}{
		{
			name: "route generation mismatch",
			mutate: func(invocation *OpaqueHTTPInvocationV1) {
				invocation.TrustedIngress.RouteGeneration++
			},
		},
		{
			name: "credential revision mismatch",
			mutate: func(invocation *OpaqueHTTPInvocationV1) {
				invocation.TrustedIngress.CredentialRef.Revision = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			admission := &admissionFake{}
			resolver := resolverFunc(func(_ context.Context, invocation OpaqueHTTPInvocationV1) (ResolvedAdmission, error) {
				if invocation.TrustedIngress.RouteGeneration != 7 || invocation.TrustedIngress.CredentialRef.Revision != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
					return ResolvedAdmission{}, &ResolutionFailure{Category: FailureApplicationProtocolViolation, Retryable: false}
				}
				return validResolvedAdmission(), nil
			})
			handler := mustHandler(t, resolver, admission, 2*time.Second)
			request := httptest.NewRequest(http.MethodPost, "http://internal.invalid/conformance", bytes.NewReader(invocationEnvelope(t, test.mutate)))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			assertPlatformFailure(t, response.Body.Bytes())
			createCalls, _ := admission.counts()
			if createCalls != 0 {
				t.Fatalf("CreateRun calls = %d, want zero", createCalls)
			}
		})
	}
}

func TestHandlerWaitIsBoundedWithoutCancelingTheAdmittedRun(t *testing.T) {
	t.Parallel()

	queued := state.Run{ID: "run-queued", State: state.RunQueued, Deployment: contract.Deployment{Workspace: testWorkspace}}
	admission := &admissionFake{
		create: func(context.Context, execution.CreateRunRequest) (execution.Admission, error) {
			return execution.Admission{Run: queued}, nil
		},
		get: func(context.Context, execution.Principal, string, string) (state.Run, error) {
			return queued, nil
		},
	}
	handler := mustHandler(t, resolverFunc(func(context.Context, OpaqueHTTPInvocationV1) (ResolvedAdmission, error) {
		return validResolvedAdmission(), nil
	}), admission, time.Second)
	raw := invocationEnvelope(t, func(invocation *OpaqueHTTPInvocationV1) {
		invocation.DeadlineAt = invocation.ReceivedAt.Add(250 * time.Millisecond)
	})
	request := httptest.NewRequest(http.MethodPost, "http://internal.invalid/conformance", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	started := time.Now()
	handler.ServeHTTP(response, request)
	elapsed := time.Since(started)

	if response.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusGatewayTimeout)
	}
	assertPlatformFailureCategory(t, response.Body.Bytes(), FailureDeadlineExceeded)
	createCalls, getCalls := admission.counts()
	if createCalls != 1 || getCalls == 0 {
		t.Fatalf("calls = CreateRun:%d GetRun:%d, want one admitted run and bounded polling", createCalls, getCalls)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("bounded wait took %v", elapsed)
	}
}

func TestHandlerMapsMissingOrInvalidAppWireResultToPlatformFailure(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		state  state.RunState
		output json.RawMessage
	}{
		{name: "missing", state: state.RunSucceeded, output: nil},
		{name: "domain JSON instead of wire response", state: state.RunSucceeded, output: json.RawMessage(`{"verified":true}`)},
		{name: "body larger than configured response bound", state: state.RunSucceeded, output: contractFixture(t, "application-wire-response.example.json")},
		{name: "failed Run with wire-shaped output", state: state.RunFailed, output: contractFixture(t, "application-wire-response.example.json")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			admission := &admissionFake{
				create: func(context.Context, execution.CreateRunRequest) (execution.Admission, error) {
					return execution.Admission{Run: state.Run{ID: "run-result", State: test.state, Output: test.output}}, nil
				},
			}
			limits := testLimits(2 * time.Second)
			if test.name == "body larger than configured response bound" {
				limits.MaxResponseBytes = 6
			}
			handler, err := NewHandler(resolverFunc(func(context.Context, OpaqueHTTPInvocationV1) (ResolvedAdmission, error) {
				return validResolvedAdmission(), nil
			}), admission, limits)
			if err != nil {
				t.Fatalf("new handler: %v", err)
			}
			request := httptest.NewRequest(http.MethodPost, "http://internal.invalid/conformance", bytes.NewReader(invocationEnvelope(t, nil)))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusBadGateway)
			}
			assertPlatformFailure(t, response.Body.Bytes())
		})
	}
}

func TestHandlerReturnsExactApplicationStatusHeaderAndBytes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		status      int
		withHeader  bool
		contentType string
	}{
		{name: "application error status", status: http.StatusUnprocessableEntity, withHeader: true, contentType: "application/octet-stream"},
		{name: "no application content type", status: http.StatusOK, withHeader: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var wire ApplicationWireResponseV1
			if err := json.Unmarshal(contractFixture(t, "application-wire-response.example.json"), &wire); err != nil {
				t.Fatalf("decode wire response fixture: %v", err)
			}
			wire.Status = test.status
			if test.withHeader {
				wire.Headers = []ResponseHeaderV1{{Name: "content-type", Value: test.contentType}}
			} else {
				wire.Headers = []ResponseHeaderV1{}
			}
			raw, err := json.Marshal(wire)
			if err != nil {
				t.Fatalf("marshal wire response: %v", err)
			}
			admission := &admissionFake{create: func(context.Context, execution.CreateRunRequest) (execution.Admission, error) {
				return execution.Admission{Run: state.Run{ID: "run-exact-response", State: state.RunSucceeded, Output: raw}}, nil
			}}
			handler := mustHandler(t, resolverFunc(func(context.Context, OpaqueHTTPInvocationV1) (ResolvedAdmission, error) {
				return validResolvedAdmission(), nil
			}), admission, 2*time.Second)
			request := httptest.NewRequest(http.MethodPost, "http://internal.invalid/conformance", bytes.NewReader(invocationEnvelope(t, nil)))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if got := response.Header().Get("Content-Type"); got != test.contentType {
				t.Fatalf("content type = %q, want %q", got, test.contentType)
			}
			if want := []byte{255, 0, 16, 32, 123, 125, 10}; !bytes.Equal(response.Body.Bytes(), want) {
				t.Fatalf("response bytes = %v, want %v", response.Body.Bytes(), want)
			}
		})
	}
}

func TestHandlerComposesWithAdmissionForExactByteRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	inputSchema := contractFixture(t, "opaque-http-app-input.schema.json")
	outputSchema := contractFixture(t, "application-wire-response.schema.json")
	appInput := contractFixture(t, "opaque-http-app-input.example.json")
	wireResponse := contractFixture(t, "application-wire-response.example.json")
	publicInterface := json.RawMessage(`{"kind":"example.synthetic-http/v1","opaque":{"route":"not-interpreted-by-core"}}`)
	deploymentID := testDeploymentID
	publication, err := store.PublishRelease(ctx, contract.Deployment{
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
				InputSchemaBody:  inputSchema,
				OutputSchemaBody: outputSchema,
				PublicInterfaces: []json.RawMessage{publicInterface},
			},
		},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("publish release: %v", err)
	}

	resolver := resolverFunc(func(_ context.Context, invocation OpaqueHTTPInvocationV1) (ResolvedAdmission, error) {
		if invocation.TrustedIngress.PublicationRef != "synthetic-byte-roundtrip" || invocation.TrustedIngress.RouteGeneration != 7 ||
			invocation.TrustedIngress.CredentialRef.ID != "credential/synthetic" ||
			invocation.TrustedIngress.CredentialRef.Revision != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
			return ResolvedAdmission{}, &ResolutionFailure{Category: FailureApplicationProtocolViolation, Retryable: false}
		}
		return ResolvedAdmission{
			Workspace: testWorkspace,
			App:       testApp,
			Action:    testAction,
			ExpectedRelease: execution.ActiveReleasePrecondition{
				DeploymentID: testDeploymentID,
				Commit:       publication.Deployment.Commit,
				BundleDigest: publication.Deployment.BundleDigest,
			},
			Principal: testPrincipal(),
		}, nil
	})
	admission := execution.NewAdmissionService(store, store, nil)
	handler := mustHandler(t, resolver, admission, 2*time.Second)
	workerResult := make(chan error, 1)
	go func() {
		workerResult <- executeSyntheticJob(ctx, store, inputSchema, outputSchema, publicInterface, appInput, wireResponse)
	}()

	request := httptest.NewRequest(http.MethodPost, "http://internal.invalid/conformance", bytes.NewReader(invocationEnvelope(t, nil)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if err := <-workerResult; err != nil {
		t.Fatalf("synthetic worker: %v", err)
	}

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("content type = %q, want application/octet-stream", got)
	}
	if want := []byte{255, 0, 16, 32, 123, 125, 10}; !bytes.Equal(response.Body.Bytes(), want) {
		t.Fatalf("response bytes = %v, want %v", response.Body.Bytes(), want)
	}
}

func TestAdmissionReleasePreconditionMismatchCreatesZeroRuns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
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
		Actions: map[string]contract.Action{
			testAction: {
				Action:          testAction,
				InputSchemaBody: contractFixture(t, "opaque-http-app-input.schema.json"),
			},
		},
	}, time.Now().UTC()); err != nil {
		t.Fatalf("publish release: %v", err)
	}
	resolver := resolverFunc(func(context.Context, OpaqueHTTPInvocationV1) (ResolvedAdmission, error) {
		resolved := validResolvedAdmission()
		resolved.ExpectedRelease.Commit = "stale-commit"
		return resolved, nil
	})
	handler := mustHandler(t, resolver, execution.NewAdmissionService(store, store, nil), 2*time.Second)
	request := httptest.NewRequest(http.MethodPost, "http://internal.invalid/conformance", bytes.NewReader(invocationEnvelope(t, nil)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusConflict)
	}
	assertPlatformFailure(t, response.Body.Bytes())
	snapshot, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if len(snapshot.Runs) != 0 || len(snapshot.Jobs) != 0 {
		t.Fatalf("release mismatch created Runs=%d Jobs=%d, want zero", len(snapshot.Runs), len(snapshot.Jobs))
	}
}

func TestOpaqueAppInputMustMatchPinnedActionSchemaBeforeRunCreation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	deploymentID := testDeploymentID
	domainSchema := json.RawMessage(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "required": ["domainValue"],
  "properties": {"domainValue": {"type": "string"}}
}`)
	if _, err := store.PublishRelease(ctx, contract.Deployment{
		Workspace:    testWorkspace,
		GitSourceID:  "synthetic-source",
		APIVersion:   contract.AppManifestV2,
		App:          testApp,
		Commit:       testCommit,
		DeploymentID: &deploymentID,
		BundleDigest: testBundleDigest,
		Actions: map[string]contract.Action{
			testAction: {Action: testAction, InputSchemaBody: domainSchema},
		},
	}, time.Now().UTC()); err != nil {
		t.Fatalf("publish release: %v", err)
	}
	handler := mustHandler(t, resolverFunc(func(context.Context, OpaqueHTTPInvocationV1) (ResolvedAdmission, error) {
		return validResolvedAdmission(), nil
	}), execution.NewAdmissionService(store, store, nil), 2*time.Second)
	request := httptest.NewRequest(http.MethodPost, "http://internal.invalid/conformance", bytes.NewReader(invocationEnvelope(t, nil)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	assertPlatformFailureCategory(t, response.Body.Bytes(), FailureApplicationProtocolViolation)
	snapshot, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if len(snapshot.Runs) != 0 || len(snapshot.Jobs) != 0 {
		t.Fatalf("schema mismatch created Runs=%d Jobs=%d, want zero", len(snapshot.Runs), len(snapshot.Jobs))
	}
}

func executeSyntheticJob(
	ctx context.Context,
	store *state.LocalStore,
	inputSchema json.RawMessage,
	outputSchema json.RawMessage,
	publicInterface json.RawMessage,
	wantInput json.RawMessage,
	response json.RawMessage,
) error {
	var job state.Job
	var lease state.Lease
	for {
		var err error
		job, lease, err = store.ClaimJob(ctx, "synthetic-worker", time.Minute)
		if err == nil {
			break
		}
		if !errors.Is(err, state.ErrNoQueuedJob) {
			return fmt.Errorf("claim job: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}

	if !jsonBytesEqual(job.Payload.Input, wantInput) {
		return fmt.Errorf("App input does not match opaque wrapper: %s", job.Payload.Input)
	}
	if !jsonBytesEqual(job.Payload.InputSchema, inputSchema) || !jsonBytesEqual(job.Payload.OutputSchema, outputSchema) {
		return errors.New("pinned action schemas were not preserved on the Job")
	}
	if job.Payload.APIVersion != contract.AppManifestV2 {
		return fmt.Errorf("Job apiVersion = %q", job.Payload.APIVersion)
	}
	if len(job.Payload.ActionSpec.PublicInterfaces) != 1 || !jsonBytesEqual(job.Payload.ActionSpec.PublicInterfaces[0], publicInterface) {
		return fmt.Errorf("opaque public interface declaration was not preserved on the Job: got=%q want=%q", job.Payload.ActionSpec.PublicInterfaces, publicInterface)
	}
	if err := store.CompleteJobSucceeded(ctx, lease, contract.JobResult{
		JobID:  job.ID,
		App:    testApp,
		Action: testAction,
		Output: response,
	}); err != nil {
		return fmt.Errorf("complete job: %w", err)
	}
	return nil
}

func mustHandler(t *testing.T, resolver Resolver, admission Admission, maxWait time.Duration) *Handler {
	t.Helper()
	handler, err := NewHandler(resolver, admission, testLimits(maxWait))
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	return handler
}

func testLimits(maxWait time.Duration) Limits {
	return Limits{
		MaxRequestBytes:  1024,
		MaxResponseBytes: 1024,
		MaxWait:          maxWait,
		PollInterval:     time.Millisecond,
	}
}

func testPrincipal() execution.Principal {
	return execution.Principal{
		Kind:           execution.PrincipalService,
		ID:             "synthetic-credential",
		Workspace:      testWorkspace,
		Subject:        "service:synthetic-credential",
		Scopes:         []execution.Scope{execution.ScopeRunsCreate, execution.ScopeRunsReadOwn},
		AllowedTargets: []string{testApp + "/" + testAction},
	}.Normalized()
}

func validResolvedAdmission() ResolvedAdmission {
	return ResolvedAdmission{
		Workspace: testWorkspace,
		App:       testApp,
		Action:    testAction,
		ExpectedRelease: execution.ActiveReleasePrecondition{
			DeploymentID: testDeploymentID,
			Commit:       testCommit,
			BundleDigest: testBundleDigest,
		},
		Principal: testPrincipal(),
	}
}

func invocationEnvelope(t *testing.T, mutate func(*OpaqueHTTPInvocationV1)) []byte {
	t.Helper()
	var invocation OpaqueHTTPInvocationV1
	if err := json.Unmarshal(contractFixture(t, "opaque-http-invocation.example.json"), &invocation); err != nil {
		t.Fatalf("decode invocation fixture: %v", err)
	}
	invocation.ReceivedAt = time.Now().UTC()
	invocation.DeadlineAt = invocation.ReceivedAt.Add(2 * time.Second)
	if mutate != nil {
		mutate(&invocation)
	}
	raw, err := json.Marshal(invocation)
	if err != nil {
		t.Fatalf("marshal invocation: %v", err)
	}
	return raw
}

func assertPlatformFailure(t *testing.T, raw []byte) {
	t.Helper()
	var outcome ExecutionOutcomeV1
	if err := json.Unmarshal(raw, &outcome); err != nil {
		t.Fatalf("decode platform failure: %v; body=%s", err, raw)
	}
	if outcome.Kind != ExecutionOutcomeKindV1 || outcome.Outcome != ExecutionOutcomePlatformFailed || outcome.Failure == nil || outcome.Response != nil {
		t.Fatalf("invalid platform failure: %+v", outcome)
	}
}

func assertPlatformFailureCategory(t *testing.T, raw []byte, category FailureCategory) {
	t.Helper()
	assertPlatformFailure(t, raw)
	var outcome ExecutionOutcomeV1
	if err := json.Unmarshal(raw, &outcome); err != nil {
		t.Fatal(err)
	}
	if outcome.Failure.Category != category {
		t.Fatalf("failure category = %q, want %q", outcome.Failure.Category, category)
	}
}

func jsonBytesEqual(left, right []byte) bool {
	var leftValue any
	var rightValue any
	return json.Unmarshal(left, &leftValue) == nil && json.Unmarshal(right, &rightValue) == nil && reflect.DeepEqual(leftValue, rightValue)
}
