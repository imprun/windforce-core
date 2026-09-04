package opaquehttp

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/execution"
	"github.com/imprun/windforce-core/internal/state"
)

type projectionStoreFunc func(context.Context, state.OpaqueIngressResolutionRequest) (state.OpaqueIngressResolvedProjection, error)

func (f projectionStoreFunc) ResolveOpaqueIngressProjection(ctx context.Context, request state.OpaqueIngressResolutionRequest) (state.OpaqueIngressResolvedProjection, error) {
	return f(ctx, request)
}

func TestStoreResolverReturnsExactAdmissionAuthority(t *testing.T) {
	t.Parallel()

	request, projection := storeResolverFixture()
	if err := validateAtomicOpaqueIngressProjection(request, projection); err != nil {
		t.Fatalf("invalid fixture: %v", err)
	}
	var captured state.OpaqueIngressResolutionRequest
	resolver, err := NewStoreResolver(projectionStoreFunc(func(_ context.Context, request state.OpaqueIngressResolutionRequest) (state.OpaqueIngressResolvedProjection, error) {
		captured = request
		return projection, nil
	}))
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}

	resolved, err := resolver.ResolveOpaqueHTTPInvocation(context.Background(), request)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if captured.Issuer != request.TrustedIngress.Issuer || captured.Audience != request.TrustedIngress.Audience ||
		captured.PublicationRef != request.TrustedIngress.PublicationRef || captured.RouteGeneration != request.TrustedIngress.RouteGeneration ||
		captured.CredentialID != request.TrustedIngress.CredentialRef.ID || captured.CredentialRevision != request.TrustedIngress.CredentialRef.Revision ||
		captured.Method != request.HTTP.Method || captured.ExactEscapedPath != request.HTTP.ExactEscapedPath ||
		captured.ContentType != request.HTTP.ContentType || captured.BodyByteLength != request.BodyByteLength {
		t.Fatalf("store request does not exactly mirror the trusted body-blind request: %#v", captured)
	}
	if resolved.Workspace != projection.Publication.WorkspaceID || resolved.App != projection.Publication.App || resolved.Action != projection.Publication.Action {
		t.Fatalf("resolved target = %s/%s/%s", resolved.Workspace, resolved.App, resolved.Action)
	}
	if resolved.ExpectedRelease.DeploymentID != projection.Publication.Release.DeploymentID ||
		resolved.ExpectedRelease.Commit != projection.Publication.Release.Commit ||
		resolved.ExpectedRelease.BundleDigest != projection.Publication.Release.BundleDigest {
		t.Fatalf("resolved Release = %#v", resolved.ExpectedRelease)
	}
	principal := resolved.Principal.Normalized()
	if principal.Kind != execution.PrincipalService || principal.Workspace != projection.Publication.WorkspaceID ||
		len(principal.Scopes) != 2 || !principal.HasScope(execution.ScopeRunsCreate) || !principal.HasScope(execution.ScopeRunsReadOwn) ||
		len(principal.AllowedTargets) != 1 || principal.AllowedTargets[0] != projection.Publication.App+"/"+projection.Publication.Action {
		t.Fatalf("resolved principal is not least privilege: %#v", principal)
	}
	if resolved.InvocationPins.PublicationRef != projection.Publication.PublicationRef ||
		resolved.InvocationPins.RouteGeneration != projection.Activation.Generation ||
		resolved.InvocationPins.OperationRef != projection.Publication.OperationRef ||
		resolved.InvocationPins.CredentialRef.ID != projection.Credential.Reference.ID ||
		resolved.InvocationPins.CredentialRef.Version != projection.Credential.Reference.Revision {
		t.Fatalf("resolved pins = %#v", resolved.InvocationPins)
	}
	if len(resolved.InvocationPins.References) != 3 || resolved.InvocationPins.References[0].Name != opaqueHTTPPublicationPinName ||
		resolved.InvocationPins.References[0].Reference.Version != projection.Publication.Digest {
		t.Fatalf("resolved immutable references = %#v", resolved.InvocationPins.References)
	}
}

func TestStoreResolverFailsClosedWithoutLeakingStoreErrors(t *testing.T) {
	t.Parallel()

	request, _ := storeResolverFixture()
	resolver, err := NewStoreResolver(projectionStoreFunc(func(context.Context, state.OpaqueIngressResolutionRequest) (state.OpaqueIngressResolvedProjection, error) {
		return state.OpaqueIngressResolvedProjection{}, errors.New("postgres host and credential snapshot detail")
	}))
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}

	_, err = resolver.ResolveOpaqueHTTPInvocation(context.Background(), request)
	assertProjectionUnavailable(t, err)
	if err.Error() != "opaque HTTP invocation could not be resolved" {
		t.Fatalf("error leaked store detail: %q", err)
	}
}

func TestStoreResolverRejectsMixedOrTamperedTuple(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		mutate func(*state.OpaqueIngressResolvedProjection)
	}{
		{
			name: "mixed generation",
			mutate: func(projection *state.OpaqueIngressResolvedProjection) {
				projection.Activation.Generation++
			},
		},
		{
			name: "publication digest",
			mutate: func(projection *state.OpaqueIngressResolvedProjection) {
				projection.Publication.Action = "other"
			},
		},
		{
			name: "credential digest",
			mutate: func(projection *state.OpaqueIngressResolvedProjection) {
				projection.Credential.OperationRef = "operations/other"
			},
		},
		{
			name: "missing deployment id",
			mutate: func(projection *state.OpaqueIngressResolvedProjection) {
				projection.Publication.Release.DeploymentID = ""
				projection.Publication.Digest = state.OpaqueIngressPublicationRevisionDigest(projection.Publication)
				projection.Activation.PublicationDigest = projection.Publication.Digest
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request, projection := storeResolverFixture()
			test.mutate(&projection)
			resolver, err := NewStoreResolver(projectionStoreFunc(func(context.Context, state.OpaqueIngressResolutionRequest) (state.OpaqueIngressResolvedProjection, error) {
				return projection, nil
			}))
			if err != nil {
				t.Fatalf("new resolver: %v", err)
			}
			_, err = resolver.ResolveOpaqueHTTPInvocation(context.Background(), request)
			assertProjectionUnavailable(t, err)
		})
	}
}

func TestStoreResolverFailureCreatesZeroRuns(t *testing.T) {
	t.Parallel()

	requestFixture, projection := storeResolverFixture()
	for _, test := range []struct {
		name    string
		resolve func(context.Context, state.OpaqueIngressResolutionRequest) (state.OpaqueIngressResolvedProjection, error)
	}{
		{
			name: "stale or revoked projection",
			resolve: func(context.Context, state.OpaqueIngressResolutionRequest) (state.OpaqueIngressResolvedProjection, error) {
				return state.OpaqueIngressResolvedProjection{}, state.ErrOpaqueIngressProjectionRejected
			},
		},
		{
			name: "mixed projection tuple",
			resolve: func(context.Context, state.OpaqueIngressResolutionRequest) (state.OpaqueIngressResolvedProjection, error) {
				mixed := projection
				mixed.Activation.PublicationDigest = "sha256:mixed"
				return mixed, nil
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			resolver, err := NewStoreResolver(projectionStoreFunc(test.resolve))
			if err != nil {
				t.Fatalf("new resolver: %v", err)
			}
			admission := &admissionFake{}
			handler := mustHandler(t, resolver, admission, 2*time.Second)
			request := httptest.NewRequest(http.MethodPost, "http://internal.invalid/conformance", bytes.NewReader(invocationEnvelope(t, func(invocation *OpaqueHTTPInvocationV1) {
				invocation.TrustedIngress = requestFixture.TrustedIngress
				invocation.HTTP = requestFixture.HTTP
			})))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			assertPlatformFailureCategory(t, response.Body.Bytes(), FailureCapacityUnavailable)
			createCalls, _ := admission.counts()
			if createCalls != 0 {
				t.Fatalf("CreateRun calls = %d, want zero", createCalls)
			}
		})
	}
}

func assertProjectionUnavailable(t *testing.T, err error) {
	t.Helper()
	var failure *ResolutionFailure
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want ResolutionFailure", err)
	}
	if failure.Category != FailureCapacityUnavailable || failure.Retryable {
		t.Fatalf("failure = %#v, want non-retryable capacityUnavailable", failure)
	}
}

func storeResolverFixture() (ResolutionRequest, state.OpaqueIngressResolvedProjection) {
	now := time.Date(2026, time.September, 2, 0, 0, 0, 0, time.UTC)
	credential := state.OpaqueIngressCredentialSnapshot{
		WorkspaceID: "workspace-a",
		Issuer:      "gateway.example",
		Audience:    "windforce-core",
		Reference: state.OpaqueIngressCredentialSnapshotRef{
			ID:       "credential-a",
			Revision: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		OperationRef: "operations/id-verification/v1",
		References: []contract.NamedImmutableReferencePin{{
			Name:      "credential-policy",
			Reference: contract.ImmutableReference{ID: "policy-a", Version: "version-1"},
		}},
		ProjectedAt:         now,
		NotAfter:            now.Add(24 * time.Hour),
		MaxStalenessSeconds: 3600,
	}
	credential.Reference.Digest = state.OpaqueIngressCredentialSnapshotDigest(credential)
	publication := state.OpaqueIngressPublicationRevision{
		WorkspaceID:    credential.WorkspaceID,
		Issuer:         credential.Issuer,
		Audience:       credential.Audience,
		PublicationRef: "id-verification",
		Revision:       "revision-7",
		App:            "identity_verification",
		Action:         "verify",
		Release: state.OpaqueIngressReleasePin{
			DeploymentID: "deployment-7",
			Commit:       "0123456789abcdef",
			BundleDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		HTTP: state.OpaqueIngressHTTPContract{
			Method:              http.MethodPost,
			ExactEscapedPath:    "/v2/identity/verify",
			ContentType:         "application/json",
			MaxRequestBodyBytes: 4096,
			ResponsePolicy: contract.HTTPPolicy{
				ContentTypes: []string{"application/json"},
				MaxBodyBytes: 4096,
			},
		},
		OperationRef:   credential.OperationRef,
		CredentialRefs: []state.OpaqueIngressCredentialSnapshotRef{credential.Reference},
		References: []contract.NamedImmutableReferencePin{{
			Name:      "input-schema",
			Reference: contract.ImmutableReference{ID: "schema-a", Version: "version-1"},
		}},
		ProjectedAt:         now,
		NotAfter:            now.Add(24 * time.Hour),
		MaxStalenessSeconds: 3600,
		RetainUntil:         now.Add(30 * 24 * time.Hour),
	}
	publication.Digest = state.OpaqueIngressPublicationRevisionDigest(publication)
	activation := state.OpaqueIngressActivation{
		WorkspaceID:       publication.WorkspaceID,
		Issuer:            publication.Issuer,
		Audience:          publication.Audience,
		PublicationRef:    publication.PublicationRef,
		Generation:        7,
		Revision:          publication.Revision,
		PublicationDigest: publication.Digest,
		State:             state.OpaqueIngressActivationActive,
		Kind:              state.OpaqueIngressActivationKindActivate,
	}
	return ResolutionRequest{
			TrustedIngress: TrustedIngressV1{
				Issuer:          publication.Issuer,
				Audience:        publication.Audience,
				PublicationRef:  publication.PublicationRef,
				RouteGeneration: activation.Generation,
				CredentialRef: ImmutableRefV1{
					ID:       credential.Reference.ID,
					Revision: credential.Reference.Revision,
				},
				DeliveryID: "delivery-7f3a",
			},
			HTTP: HTTPMediaV1{
				Method:           publication.HTTP.Method,
				ExactEscapedPath: publication.HTTP.ExactEscapedPath,
				ContentType:      publication.HTTP.ContentType,
			},
			BodyByteLength: 2,
		}, state.OpaqueIngressResolvedProjection{
			Publication: publication,
			Activation:  activation,
			Credential:  credential,
		}
}
