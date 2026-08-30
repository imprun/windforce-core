package opaquehttp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/execution"
	"github.com/imprun/windforce-core/internal/state"
)

const (
	adapterName           = "opaque-http"
	maxConfiguredWait     = 5 * time.Minute
	envelopeOverheadBytes = 32 << 10
)

// Resolver atomically validates the trusted publication tuple, route
// generation, credential snapshot, and route media contract. It returns an
// exact target, scoped principal, and active-Release precondition for Admission.
type Resolver interface {
	ResolveOpaqueHTTPInvocation(ctx context.Context, invocation OpaqueHTTPInvocationV1) (ResolvedAdmission, error)
}

type ResolvedAdmission struct {
	Workspace       string
	App             string
	Action          string
	ExpectedRelease execution.ActiveReleasePrecondition
	Principal       execution.Principal
}

// Admission is the existing in-process execution boundary used by the
// conformance handler. *execution.AdmissionService implements this interface.
type Admission interface {
	CreateRun(ctx context.Context, request execution.CreateRunRequest) (execution.Admission, error)
	GetRunForPrincipal(ctx context.Context, principal execution.Principal, workspace string, runID string) (state.Run, error)
}

type Limits struct {
	MaxRequestBytes  int64
	MaxResponseBytes int64
	MaxWait          time.Duration
	PollInterval     time.Duration
}

type Handler struct {
	resolver         Resolver
	admission        Admission
	maxEnvelopeBytes int64
	limits           Limits
}

// NewHandler creates an opt-in handler only. It does not open a listener or
// mount itself on Core's primary HTTP server.
func NewHandler(resolver Resolver, admission Admission, limits Limits) (*Handler, error) {
	if resolver == nil || admission == nil {
		return nil, errors.New("opaque HTTP resolver and Admission are required")
	}
	if limits.MaxRequestBytes <= 0 || limits.MaxRequestBytes > MaxWireBodyBytes ||
		limits.MaxResponseBytes <= 0 || limits.MaxResponseBytes > MaxWireBodyBytes {
		return nil, fmt.Errorf("opaque HTTP request and response byte limits must be between 1 and %d", MaxWireBodyBytes)
	}
	if limits.MaxWait <= 0 || limits.MaxWait > maxConfiguredWait {
		return nil, errors.New("opaque HTTP max wait must be positive and no greater than five minutes")
	}
	if limits.PollInterval <= 0 || limits.PollInterval > limits.MaxWait {
		return nil, errors.New("opaque HTTP poll interval must be positive and no greater than max wait")
	}
	maxEnvelopeBytes := int64(base64.StdEncoding.EncodedLen(int(limits.MaxRequestBytes))) + envelopeOverheadBytes
	return &Handler{
		resolver:         resolver,
		admission:        admission,
		maxEnvelopeBytes: maxEnvelopeBytes,
		limits:           limits,
	}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		h.writePlatformFailure(w, http.StatusMethodNotAllowed, FailureApplicationProtocolViolation, false)
		return
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		h.writePlatformFailure(w, http.StatusUnsupportedMediaType, FailureApplicationProtocolViolation, false)
		return
	}
	invocation, _, err := decodeInvocation(request.Body, h.maxEnvelopeBytes, h.limits.MaxRequestBytes)
	if err != nil {
		h.writePlatformFailure(w, http.StatusBadRequest, FailureApplicationProtocolViolation, false)
		return
	}
	if err := h.validateDeadline(invocation, time.Now().UTC()); err != nil {
		h.writePlatformFailure(w, http.StatusBadRequest, FailureDeadlineExceeded, false)
		return
	}
	appInput, err := encodeAppInput(invocation)
	if err != nil {
		h.writePlatformFailure(w, http.StatusInternalServerError, FailureInternal, false)
		return
	}
	resolved, err := h.resolver.ResolveOpaqueHTTPInvocation(request.Context(), invocation)
	if err != nil {
		status, category, retryable := resolutionFailure(err)
		h.writePlatformFailure(w, status, category, retryable)
		return
	}
	admissionRequest, err := prepareAdmissionRequest(resolved, appInput)
	if err != nil {
		h.writePlatformFailure(w, http.StatusInternalServerError, FailureInternal, false)
		return
	}
	admitted, err := h.admission.CreateRun(request.Context(), admissionRequest)
	if err != nil {
		status, category, retryable := admissionFailure(err)
		h.writePlatformFailure(w, status, category, retryable)
		return
	}
	run, err := h.waitForRun(request.Context(), admitted.Run, admissionRequest.Principal, invocation.DeadlineAt)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			h.writePlatformFailure(w, http.StatusGatewayTimeout, FailureDeadlineExceeded, false)
			return
		}
		status, category, retryable := admissionFailure(err)
		h.writePlatformFailure(w, status, category, retryable)
		return
	}
	if run.State != state.RunSucceeded {
		h.writePlatformFailure(w, http.StatusBadGateway, FailureInternal, false)
		return
	}
	rawResult := runOutput(run)
	if len(rawResult) == 0 {
		h.writePlatformFailure(w, http.StatusBadGateway, FailureInternal, false)
		return
	}
	wireResponse, responseBody, err := decodeApplicationWireResponse(rawResult, h.limits.MaxResponseBytes)
	if err != nil || wireResponse.Status < 200 || wireResponse.Status == http.StatusNoContent && len(responseBody) > 0 || wireResponse.Status == http.StatusNotModified && len(responseBody) > 0 {
		h.writePlatformFailure(w, http.StatusBadGateway, FailureApplicationProtocolViolation, false)
		return
	}
	if len(wireResponse.Headers) == 1 {
		w.Header().Set("Content-Type", wireResponse.Headers[0].Value)
	} else {
		// Presence with an empty value suppresses net/http content sniffing while
		// emitting no application-supplied content-type value.
		w.Header()["Content-Type"] = nil
	}
	w.WriteHeader(wireResponse.Status)
	_, _ = w.Write(responseBody)
}

func (h *Handler) validateDeadline(invocation OpaqueHTTPInvocationV1, now time.Time) error {
	if invocation.ReceivedAt.IsZero() || invocation.DeadlineAt.IsZero() {
		return errors.New("receivedAt and deadlineAt are required")
	}
	declared := invocation.DeadlineAt.Sub(invocation.ReceivedAt)
	remaining := invocation.DeadlineAt.Sub(now)
	if declared <= 0 || declared > h.limits.MaxWait || remaining <= 0 || remaining > h.limits.MaxWait {
		return errors.New("opaque HTTP deadline is outside the configured wait budget")
	}
	return nil
}

func prepareAdmissionRequest(resolved ResolvedAdmission, appInput []byte) (execution.CreateRunRequest, error) {
	workspace := contract.NormalizeWorkspace(resolved.Workspace)
	app := strings.TrimSpace(resolved.App)
	action := strings.TrimSpace(resolved.Action)
	if !contract.ValidAppKey(app) || !contract.ValidActionKey(action) {
		return execution.CreateRunRequest{}, errors.New("resolver returned an invalid App target")
	}
	if strings.TrimSpace(resolved.ExpectedRelease.Commit) == "" || strings.TrimSpace(resolved.ExpectedRelease.BundleDigest) == "" {
		return execution.CreateRunRequest{}, errors.New("resolver did not pin an active Release")
	}
	expected := resolved.ExpectedRelease
	principal := resolved.Principal.Normalized()
	target := app + "/" + action
	if principal.Kind != execution.PrincipalService || principal.Workspace != workspace || principal.ID == "" ||
		!principal.HasScope(execution.ScopeRunsCreate) || !principal.HasScope(execution.ScopeRunsReadOwn) ||
		principal.HasScope(execution.ScopeRunsReadAny) || len(principal.AllowedTargets) != 1 || principal.AllowedTargets[0] != target {
		return execution.CreateRunRequest{}, errors.New("resolver did not return an exact scoped service principal")
	}
	return execution.CreateRunRequest{
		Workspace:       workspace,
		App:             app,
		Action:          action,
		ExpectedRelease: &expected,
		Input:           append(json.RawMessage(nil), appInput...),
		Adapter:         adapterName,
		Principal:       principal,
	}, nil
}

func (h *Handler) waitForRun(ctx context.Context, initial state.Run, principal execution.Principal, deadline time.Time) (state.Run, error) {
	run := initial
	for {
		if state.TerminalRunState(run.State) {
			return run, nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return state.Run{}, context.DeadlineExceeded
		}
		wait := min(h.limits.PollInterval, remaining)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return state.Run{}, ctx.Err()
		case <-timer.C:
		}
		var err error
		run, err = h.admission.GetRunForPrincipal(ctx, principal, principal.Workspace, initial.ID)
		if err != nil {
			return state.Run{}, err
		}
	}
}

func runOutput(run state.Run) json.RawMessage {
	if len(run.Output) > 0 {
		return append(json.RawMessage(nil), run.Output...)
	}
	if run.Result != nil && len(run.Result.Output) > 0 {
		return append(json.RawMessage(nil), run.Result.Output...)
	}
	return nil
}

// ResolutionFailure is a provider-neutral fail-closed result from an atomic
// Resolver. It intentionally carries no route, credential, or secret detail.
type ResolutionFailure struct {
	Category  FailureCategory
	Retryable bool
}

func (e *ResolutionFailure) Error() string {
	return "opaque HTTP invocation could not be resolved"
}

func resolutionFailure(err error) (int, FailureCategory, bool) {
	var failure *ResolutionFailure
	if errors.As(err, &failure) && validFailureCategory(failure.Category) {
		return failureStatus(failure.Category), failure.Category, failure.Retryable
	}
	return http.StatusInternalServerError, FailureInternal, false
}

func admissionFailure(err error) (int, FailureCategory, bool) {
	switch execution.FaultKindOf(err) {
	case execution.FaultInvalidRequest:
		return http.StatusBadRequest, FailureApplicationProtocolViolation, false
	case execution.FaultForbidden:
		return http.StatusForbidden, FailureApplicationProtocolViolation, false
	case execution.FaultAppNotFound, execution.FaultActionNotFound:
		return http.StatusNotFound, FailureApplicationProtocolViolation, false
	case execution.FaultRoutingConflict, execution.FaultConflict:
		return http.StatusConflict, FailureApplicationProtocolViolation, false
	case execution.FaultUnavailable:
		return http.StatusServiceUnavailable, FailureCapacityUnavailable, true
	default:
		return http.StatusInternalServerError, FailureInternal, false
	}
}

func failureStatus(category FailureCategory) int {
	switch category {
	case FailureDeadlineExceeded:
		return http.StatusGatewayTimeout
	case FailureCapacityUnavailable:
		return http.StatusServiceUnavailable
	case FailureWorkerLost, FailureApplicationProtocolViolation:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

func validFailureCategory(category FailureCategory) bool {
	switch category {
	case FailureDeadlineExceeded, FailureCapacityUnavailable, FailureWorkerLost, FailureApplicationProtocolViolation, FailureInternal:
		return true
	default:
		return false
	}
}

func (h *Handler) writePlatformFailure(w http.ResponseWriter, status int, category FailureCategory, retryable bool) {
	if !validFailureCategory(category) {
		category = FailureInternal
		retryable = false
	}
	outcome := ExecutionOutcomeV1{
		Kind:    ExecutionOutcomeKindV1,
		Outcome: ExecutionOutcomePlatformFailed,
		Failure: &PlatformFailureV1{Category: category, Retryable: retryable},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(outcome)
}
