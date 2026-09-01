package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/execution"
	"github.com/imprun/windforce-core/internal/state"
)

type opaqueProjectionAPITestStore struct {
	state.Store

	credentialRequest state.OpaqueIngressCredentialSnapshotRequest
	credentialReplay  bool
	credentialError   error

	revocationRequest state.OpaqueIngressCredentialRevocationRequest
	revocationReplay  bool

	publicationRequest state.OpaqueIngressPublicationRevisionRequest
	publicationReplay  bool
	publicationError   error

	activationRequest state.OpaqueIngressActivationRequest
	activationReplay  bool

	retentionRequest state.OpaqueIngressRetentionRequest
	retentionReplay  bool

	audits []state.OpaqueIngressAudit
}

func (s *opaqueProjectionAPITestStore) PutOpaqueIngressCredentialSnapshot(_ context.Context, request state.OpaqueIngressCredentialSnapshotRequest) (state.OpaqueIngressCredentialSnapshot, bool, error) {
	s.credentialRequest = request
	if s.credentialError != nil {
		return state.OpaqueIngressCredentialSnapshot{}, false, s.credentialError
	}
	return state.OpaqueIngressCredentialSnapshot{
		WorkspaceID: request.WorkspaceID, Issuer: request.Issuer, Audience: request.Audience,
		Reference: request.Reference, OperationRef: request.OperationRef, References: request.References,
		ProjectedAt: request.ProjectedAt, NotAfter: request.NotAfter, MaxStalenessSeconds: request.MaxStalenessSeconds,
		OperationID: request.OperationID, RequestFingerprint: "secret-fingerprint", Actor: request.Actor,
		CreatedAt: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
	}, s.credentialReplay, nil
}

func (s *opaqueProjectionAPITestStore) RevokeOpaqueIngressCredentialSnapshot(_ context.Context, request state.OpaqueIngressCredentialRevocationRequest) (state.OpaqueIngressCredentialRevocation, bool, error) {
	s.revocationRequest = request
	return state.OpaqueIngressCredentialRevocation{
		ID: "revocation-1", WorkspaceID: request.WorkspaceID, Issuer: request.Issuer, Audience: request.Audience,
		Reference: request.Reference, Reason: "secret-reason", OperationID: request.OperationID,
		RequestFingerprint: "secret-fingerprint", Actor: request.Actor,
		CreatedAt: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
	}, s.revocationReplay, nil
}

func (s *opaqueProjectionAPITestStore) PutOpaqueIngressPublicationRevision(_ context.Context, request state.OpaqueIngressPublicationRevisionRequest) (state.OpaqueIngressPublicationRevision, bool, error) {
	s.publicationRequest = request
	if s.publicationError != nil {
		return state.OpaqueIngressPublicationRevision{}, false, s.publicationError
	}
	result := request.Revision
	result.OperationID = request.OperationID
	result.RequestFingerprint = "secret-fingerprint"
	result.Actor = request.Actor
	result.CreatedAt = time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	return result, s.publicationReplay, nil
}

func (s *opaqueProjectionAPITestStore) ActivateOpaqueIngressPublication(_ context.Context, request state.OpaqueIngressActivationRequest) (state.OpaqueIngressActivation, bool, error) {
	s.activationRequest = request
	return state.OpaqueIngressActivation{
		WorkspaceID: request.WorkspaceID, Issuer: request.Issuer, Audience: request.Audience,
		PublicationRef: request.PublicationRef, Generation: request.ExpectedGeneration + 1,
		Revision: request.TargetRevision, PublicationDigest: testOpaqueSHA("d"), State: state.OpaqueIngressActivationActive,
		Kind: request.Kind, AuthorizedTarget: request.AuthorizedTarget, OperationID: request.OperationID,
		RequestFingerprint: "secret-fingerprint", Actor: request.Actor,
		CreatedAt: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
	}, s.activationReplay, nil
}

func (s *opaqueProjectionAPITestStore) ListOpaqueIngressProjectionAudit(_ context.Context, _, _ string, _ int) ([]state.OpaqueIngressAudit, error) {
	return append([]state.OpaqueIngressAudit(nil), s.audits...), nil
}

func (s *opaqueProjectionAPITestStore) PruneOpaqueIngressProjectionHistory(_ context.Context, request state.OpaqueIngressRetentionRequest) (state.OpaqueIngressRetentionResult, bool, error) {
	s.retentionRequest = request
	return state.OpaqueIngressRetentionResult{PublicationRevisions: 2, CredentialSnapshots: 3}, s.retentionReplay, nil
}

func TestOpaqueProjectionAPIAdminMutationIsStrictDerivedAndSanitized(t *testing.T) {
	store := &opaqueProjectionAPITestStore{}
	handler := &Handler{store: store}
	admin := &workspacePrincipal{Workspace: "ws-a", Admin: true}

	request := validOpaqueCredentialInput()
	response := performOpaqueProjectionAPI(t, handler, admin, http.MethodPost, "/api/w/ws-a/opaque-http-projections/credential-snapshots", mustOpaqueProjectionJSON(t, request), "application/json; charset=utf-8", map[string]string{"X-Windforce-Actor": "spoofed-actor"})
	if response.Code != http.StatusCreated {
		t.Fatalf("credential response status = %d, want 201: %s", response.Code, response.Body.String())
	}
	if store.credentialRequest.Actor != "admin" {
		t.Fatalf("credential actor = %q, want server-derived admin", store.credentialRequest.Actor)
	}
	if store.credentialRequest.RequestFingerprint == "" || store.credentialRequest.RequestFingerprint == "secret-fingerprint" {
		t.Fatalf("credential fingerprint was not derived: %q", store.credentialRequest.RequestFingerprint)
	}
	assertOpaqueProjectionResponseSanitized(t, response.Body.String(), "secret-fingerprint", "requestFingerprint")

	store.credentialReplay = true
	response = performOpaqueProjectionAPI(t, handler, admin, http.MethodPost, "/api/w/ws-a/opaque-http-projections/credential-snapshots", mustOpaqueProjectionJSON(t, request), "application/json", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("credential replay status = %d, want 200", response.Code)
	}

	tests := []struct {
		name        string
		body        string
		contentType string
	}{
		{name: "unknown actor field", body: strings.TrimSuffix(string(mustOpaqueProjectionJSON(t, request)), "}") + `,"actor":"spoof"}`, contentType: "application/json"},
		{name: "duplicate member", body: `{"issuer":"issuer.test","issuer":"other"}`, contentType: "application/json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := performOpaqueProjectionAPI(t, handler, admin, http.MethodPost, "/api/w/ws-a/opaque-http-projections/credential-snapshots", []byte(test.body), test.contentType, nil)
			if response.Code != http.StatusBadRequest || response.Body.String() != "{\"error\":\"malformed request\"}\n" {
				t.Fatalf("strict response = %d %q", response.Code, response.Body.String())
			}
		})
	}
	response = performOpaqueProjectionAPI(t, handler, admin, http.MethodPost, "/api/w/ws-a/opaque-http-projections/credential-snapshots", mustOpaqueProjectionJSON(t, request), "text/plain", nil)
	if response.Code != http.StatusUnsupportedMediaType || response.Body.String() != "{\"error\":\"application/json required\"}\n" {
		t.Fatalf("content type response = %d %q", response.Code, response.Body.String())
	}

	response = performOpaqueProjectionAPI(t, handler, admin, http.MethodGet, "/api/w/ws-a/opaque-http-projections/credential-snapshots", nil, "", nil)
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("method response = %d Allow=%q", response.Code, response.Header().Get("Allow"))
	}
}

func TestOpaqueProjectionAPIAdminOnlyAndSanitizedRevocationRetention(t *testing.T) {
	store := &opaqueProjectionAPITestStore{retentionReplay: true}
	handler := &Handler{store: store}
	admin := &workspacePrincipal{Workspace: "ws-a", Admin: true}
	service := projectionServicePrincipal("ws-a", "service:writer", "identity/check")

	credentialBody := mustOpaqueProjectionJSON(t, validOpaqueCredentialInput())
	response := performOpaqueProjectionAPI(t, handler, service, http.MethodPost, "/api/w/ws-a/opaque-http-projections/credential-snapshots", credentialBody, "application/json", nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("service credential status = %d, want 403", response.Code)
	}

	revocation := opaqueIngressCredentialRevocationInput{
		Issuer: "issuer.test", Audience: "audience.test", Reference: validOpaqueCredentialInput().Reference, OperationID: "revoke-1",
	}
	response = performOpaqueProjectionAPI(t, handler, admin, http.MethodPost, "/api/w/ws-a/opaque-http-projections/credential-revocations", mustOpaqueProjectionJSON(t, revocation), "application/json", nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("revocation status = %d: %s", response.Code, response.Body.String())
	}
	assertOpaqueProjectionResponseSanitized(t, response.Body.String(), "secret-reason", "reason", "secret-fingerprint", "requestFingerprint")

	retention := opaqueIngressRetentionInput{Before: time.Now().UTC().Add(-time.Hour), Limit: 10, OperationID: "prune-1"}
	response = performOpaqueProjectionAPI(t, handler, service, http.MethodPost, "/api/w/ws-a/opaque-http-projections/retention/prune", mustOpaqueProjectionJSON(t, retention), "application/json", nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("service retention status = %d, want 403", response.Code)
	}
	response = performOpaqueProjectionAPI(t, handler, admin, http.MethodPost, "/api/w/ws-a/opaque-http-projections/retention/prune", mustOpaqueProjectionJSON(t, retention), "application/json", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("retention replay status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if store.retentionRequest.Actor != "admin" || store.retentionRequest.RequestFingerprint == "" {
		t.Fatalf("retention derived fields = actor %q fingerprint %q", store.retentionRequest.Actor, store.retentionRequest.RequestFingerprint)
	}
}

func TestOpaqueProjectionAPIPublicationRequiresOneExactServiceTarget(t *testing.T) {
	input := validOpaquePublicationInput()
	tests := []struct {
		name        string
		targets     []string
		wantStatus  int
		wantInvoked bool
	}{
		{name: "exact app action", targets: []string{"identity/check"}, wantStatus: http.StatusCreated, wantInvoked: true},
		{name: "app wildcard", targets: []string{"identity"}, wantStatus: http.StatusForbidden},
		{name: "empty wildcard", wantStatus: http.StatusForbidden},
		{name: "multiple targets", targets: []string{"identity/check", "identity/other"}, wantStatus: http.StatusForbidden},
		{name: "wrong exact target", targets: []string{"identity/other"}, wantStatus: http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &opaqueProjectionAPITestStore{}
			handler := &Handler{store: store}
			principal := projectionServicePrincipal("ws-a", "service:publisher", test.targets...)
			response := performOpaqueProjectionAPI(t, handler, principal, http.MethodPost, "/api/w/ws-a/opaque-http-projections/publication-revisions", mustOpaqueProjectionJSON(t, input), "application/json", nil)
			if response.Code != test.wantStatus {
				t.Fatalf("publication status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if got := store.publicationRequest.OperationID != ""; got != test.wantInvoked {
				t.Fatalf("store invoked = %v, want %v", got, test.wantInvoked)
			}
			if test.wantInvoked && store.publicationRequest.Actor != "service:publisher" {
				t.Fatalf("publication actor = %q", store.publicationRequest.Actor)
			}
		})
	}
}

func TestOpaqueProjectionAPIActivationPinsServiceTargetAndSanitizesErrorsAndAudit(t *testing.T) {
	store := &opaqueProjectionAPITestStore{}
	handler := &Handler{store: store}
	service := projectionServicePrincipal("ws-a", "service:activator", "identity/check")
	activation := opaqueIngressActivationInput{
		Issuer: "issuer.test", Audience: "audience.test", PublicationRef: "identity-check",
		ExpectedGeneration: 4, TargetRevision: "revision-2", Kind: state.OpaqueIngressActivationKindActivate, OperationID: "activate-1",
	}
	response := performOpaqueProjectionAPI(t, handler, service, http.MethodPost, "/api/w/ws-a/opaque-http-projections/activations", mustOpaqueProjectionJSON(t, activation), "application/json", nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("activation status = %d: %s", response.Code, response.Body.String())
	}
	if store.activationRequest.AuthorizedTarget != "identity/check" || store.activationRequest.Actor != "service:activator" {
		t.Fatalf("activation derived target/actor = %q %q", store.activationRequest.AuthorizedTarget, store.activationRequest.Actor)
	}
	assertOpaqueProjectionResponseSanitized(t, response.Body.String(), "authorizedTarget", "secret-fingerprint", "requestFingerprint")

	store.publicationError = fmt.Errorf("%w: raw database secret", state.ErrConflict)
	response = performOpaqueProjectionAPI(t, handler, service, http.MethodPost, "/api/w/ws-a/opaque-http-projections/publication-revisions", mustOpaqueProjectionJSON(t, validOpaquePublicationInput()), "application/json", nil)
	if response.Code != http.StatusConflict || response.Body.String() != "{\"error\":\"projection conflict\"}\n" || strings.Contains(response.Body.String(), "database") {
		t.Fatalf("sanitized conflict response = %d %q", response.Code, response.Body.String())
	}

	store.audits = []state.OpaqueIngressAudit{{
		ID: "audit-1", WorkspaceID: "ws-a", Issuer: "issuer.test", Audience: "audience.test",
		PublicationRef: "identity-check", SubjectKind: "publication", SubjectID: "identity-check",
		Kind: "activated", Detail: "secret audit detail", OperationID: "activate-1", Actor: "service:activator",
		CreatedAt: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
	}}
	response = performOpaqueProjectionAPI(t, handler, service, http.MethodGet, "/api/w/ws-a/opaque-http-projections/audit?publicationRef=identity-check&limit=10", nil, "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("audit status = %d: %s", response.Code, response.Body.String())
	}
	assertOpaqueProjectionResponseSanitized(t, response.Body.String(), "secret audit detail", "detail")
	for _, query := range []string{"?unknown=value", "?limit=1&limit=2", "?limit=1001", "?publicationRef=%zz"} {
		response = performOpaqueProjectionAPI(t, handler, service, http.MethodGet, "/api/w/ws-a/opaque-http-projections/audit"+query, nil, "", nil)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("audit query %q status = %d, want 400", query, response.Code)
		}
	}

	wrongWorkspace := projectionServicePrincipal("", "service:activator", "identity/check")
	response = performOpaqueProjectionAPI(t, handler, wrongWorkspace, http.MethodGet, "/api/w/ws-a/opaque-http-projections/audit", nil, "", nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("empty principal workspace status = %d, want 403", response.Code)
	}
}

func TestOpaqueProjectionStateErrorsAreStableAndSanitized(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{name: "missing", err: fmt.Errorf("%w: secret missing row", state.ErrNotFound), wantStatus: http.StatusNotFound, wantBody: "projection not found"},
		{name: "conflict", err: fmt.Errorf("%w: secret generation", state.ErrConflict), wantStatus: http.StatusConflict, wantBody: "projection conflict"},
		{name: "invalid", err: fmt.Errorf("%w: secret input", state.ErrInvalidState), wantStatus: http.StatusBadRequest, wantBody: "malformed request"},
		{name: "rejected", err: fmt.Errorf("%w: secret mixed tuple", state.ErrOpaqueIngressProjectionRejected), wantStatus: http.StatusUnprocessableEntity, wantBody: "projection rejected"},
		{name: "internal", err: errors.New("secret database failure"), wantStatus: http.StatusInternalServerError, wantBody: "internal error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeOpaqueProjectionStateError(response, test.err)
			if response.Code != test.wantStatus || response.Body.String() != fmt.Sprintf("{\"error\":%q}\n", test.wantBody) {
				t.Fatalf("response = %d %q", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "secret") {
				t.Fatalf("response leaked raw error: %s", response.Body.String())
			}
		})
	}
}

func TestOpaqueProjectionInputValidationEnforcesContractBounds(t *testing.T) {
	validCredential := validOpaqueCredentialInput()
	if !validateOpaqueCredentialSnapshotInput(validCredential) {
		t.Fatal("valid credential input was rejected")
	}
	credentialCases := []struct {
		name   string
		mutate func(*opaqueIngressCredentialSnapshotInput)
	}{
		{name: "unsafe operation id", mutate: func(input *opaqueIngressCredentialSnapshotInput) { input.OperationID = "bad operation" }},
		{name: "issuer too long", mutate: func(input *opaqueIngressCredentialSnapshotInput) { input.Issuer = strings.Repeat("i", 161) }},
		{name: "uppercase sha", mutate: func(input *opaqueIngressCredentialSnapshotInput) {
			input.Reference.Revision = "sha256:" + strings.Repeat("A", 64)
		}},
		{name: "staleness too high", mutate: func(input *opaqueIngressCredentialSnapshotInput) { input.MaxStalenessSeconds = 2_592_001 }},
		{name: "operation ref shape", mutate: func(input *opaqueIngressCredentialSnapshotInput) { input.OperationRef = "single-segment" }},
	}
	for _, test := range credentialCases {
		t.Run("credential "+test.name, func(t *testing.T) {
			input := validCredential
			test.mutate(&input)
			if validateOpaqueCredentialSnapshotInput(input) {
				t.Fatal("invalid credential input was accepted")
			}
		})
	}

	validPublication := validOpaquePublicationInput()
	if !validateOpaquePublicationRevisionInput(validPublication) {
		t.Fatal("valid publication input was rejected")
	}
	publicationCases := []struct {
		name   string
		mutate func(*opaqueIngressPublicationRevisionInput)
	}{
		{name: "publication ref pattern", mutate: func(input *opaqueIngressPublicationRevisionInput) { input.PublicationRef = "Upper_Case" }},
		{name: "path too long", mutate: func(input *opaqueIngressPublicationRevisionInput) {
			input.HTTP.ExactEscapedPath = "/" + strings.Repeat("p", 1024)
		}},
		{name: "request limit", mutate: func(input *opaqueIngressPublicationRevisionInput) {
			input.HTTP.MaxRequestBodyBytes = maxOpaqueHTTPRequestBodyBytes + 1
		}},
		{name: "response limit", mutate: func(input *opaqueIngressPublicationRevisionInput) {
			input.HTTP.ResponsePolicy.MaxBodyBytes = contract.MaxApplicationWireResponseBodyBytes + 1
		}},
		{name: "duplicate credential", mutate: func(input *opaqueIngressPublicationRevisionInput) {
			input.CredentialRefs = append(input.CredentialRefs, input.CredentialRefs[0])
		}},
		{name: "too many credentials", mutate: func(input *opaqueIngressPublicationRevisionInput) {
			input.CredentialRefs = make([]state.OpaqueIngressCredentialSnapshotRef, maxOpaqueIngressCredentialRefs+1)
			for index := range input.CredentialRefs {
				input.CredentialRefs[index] = state.OpaqueIngressCredentialSnapshotRef{ID: fmt.Sprintf("credential-%d", index), Revision: testOpaqueSHA("a"), Digest: testOpaqueSHA("b")}
			}
		}},
	}
	for _, test := range publicationCases {
		t.Run("publication "+test.name, func(t *testing.T) {
			input := validPublication
			input.CredentialRefs = append([]state.OpaqueIngressCredentialSnapshotRef(nil), validPublication.CredentialRefs...)
			test.mutate(&input)
			if validateOpaquePublicationRevisionInput(input) {
				t.Fatal("invalid publication input was accepted")
			}
		})
	}
}

func performOpaqueProjectionAPI(t *testing.T, handler *Handler, principal *workspacePrincipal, method, path string, body []byte, contentType string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "http://example.test"+path, bytes.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	request = request.WithContext(context.WithValue(request.Context(), workspacePrincipalContextKey{}, principal))
	response := httptest.NewRecorder()
	if !handler.handleAPI(response, request) {
		t.Fatalf("handleAPI did not route %s", path)
	}
	return response
}

func projectionServicePrincipal(workspace, subject string, targets ...string) *workspacePrincipal {
	service := execution.Principal{
		Kind: execution.PrincipalService, ID: strings.TrimPrefix(subject, "service:"), Workspace: workspace,
		Subject: subject, AllowedTargets: append([]string(nil), targets...),
	}
	return &workspacePrincipal{Workspace: workspace, Subject: subject, Service: &service}
}

func validOpaqueCredentialInput() opaqueIngressCredentialSnapshotInput {
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	return opaqueIngressCredentialSnapshotInput{
		Issuer: "issuer.test", Audience: "audience.test",
		Reference:    state.OpaqueIngressCredentialSnapshotRef{ID: "credential-a", Revision: testOpaqueSHA("a"), Digest: testOpaqueSHA("b")},
		OperationRef: "identity/verify", ProjectedAt: now, NotAfter: now.Add(time.Hour), MaxStalenessSeconds: 3600,
		OperationID: "credential-1",
	}
}

func validOpaquePublicationInput() opaqueIngressPublicationRevisionInput {
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	credential := validOpaqueCredentialInput().Reference
	return opaqueIngressPublicationRevisionInput{
		Issuer: "issuer.test", Audience: "audience.test", PublicationRef: "identity-check", Revision: "revision-1",
		Digest: testOpaqueSHA("c"), App: "identity", Action: "check",
		Release: state.OpaqueIngressReleasePin{DeploymentID: "deployment-1", Commit: "0123456789abcdef", BundleDigest: testOpaqueSHA("d")},
		HTTP: state.OpaqueIngressHTTPContract{
			Method: "POST", ExactEscapedPath: "/v2/identity/check", ContentType: "application/json", MaxRequestBodyBytes: 4096,
			ResponsePolicy: contract.HTTPPolicy{ContentTypes: []string{"application/json"}, MaxBodyBytes: 4096},
		},
		OperationRef: "identity/verify", CredentialRefs: []state.OpaqueIngressCredentialSnapshotRef{credential},
		ProjectedAt: now, NotAfter: now.Add(time.Hour), MaxStalenessSeconds: 3600, RetainUntil: now.Add(2 * time.Hour),
		OperationID: "publication-1",
	}
}

func testOpaqueSHA(character string) string {
	return "sha256:" + strings.Repeat(character, 64)
}

func mustOpaqueProjectionJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	return raw
}

func assertOpaqueProjectionResponseSanitized(t *testing.T, body string, forbidden ...string) {
	t.Helper()
	for _, value := range forbidden {
		if strings.Contains(body, value) {
			t.Fatalf("response contains forbidden value %q: %s", value, body)
		}
	}
}
