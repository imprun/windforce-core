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
	ResolveOpaqueHTTPInvocation(ctx context.Context, request ResolutionRequest) (ResolvedAdmission, error)
}

// ResolutionRequest is the body-blind view needed for one atomic publication
// and credential-snapshot decision. The Resolver never receives Base64 body
// data, decoded bytes, or caller-controlled timestamps.
type ResolutionRequest struct {
	TrustedIngress TrustedIngressV1
	HTTP           HTTPMediaV1
	BodyByteLength int64
}

type ResolvedAdmission struct {
	Workspace       string
	App             string
	Action          string
	ExpectedRelease execution.ActiveReleasePrecondition
	Principal       execution.Principal
	InvocationPins  contract.InvocationPins
	ResponsePolicy  contract.HTTPPolicy
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

// resolverContext preserves cancellation without exposing the caller-supplied
// absolute deadline carried by the trusted ingress envelope.
type resolverContext struct {
	context.Context
}

func (resolverContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

// NewHandler creates an opt-in handler only. It does not open a listener or
// mount itself on Core's primary HTTP server.
func NewHandler(resolver Resolver, admission Admission, limits Limits) (*Handler, error) {
	if resolver == nil || admission == nil {
		return nil, errors.New("opaque HTTP resolver and Admission are required")
	}
	if limits.MaxRequestBytes <= 0 || limits.MaxRequestBytes > MaxWireBodyBytes {
		return nil, fmt.Errorf("opaque HTTP request byte limit must be between 1 and %d", MaxWireBodyBytes)
	}
	if limits.MaxResponseBytes <= 0 || limits.MaxResponseBytes > contract.MaxApplicationWireResponseBodyBytes {
		return nil, fmt.Errorf("opaque HTTP response byte limit must be between 1 and %d", contract.MaxApplicationWireResponseBodyBytes)
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
	operationContext, cancel := context.WithDeadline(request.Context(), invocation.DeadlineAt)
	defer cancel()
	appInput, err := encodeAppInput(invocation)
	if err != nil {
		h.writePlatformFailure(w, http.StatusInternalServerError, FailureInternal, false)
		return
	}
	resolved, err := h.resolver.ResolveOpaqueHTTPInvocation(resolverContext{Context: operationContext}, ResolutionRequest{
		TrustedIngress: invocation.TrustedIngress,
		HTTP:           invocation.HTTP,
		BodyByteLength: invocation.Body.ByteLength,
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(operationContext.Err(), context.DeadlineExceeded) {
			h.writePlatformFailure(w, http.StatusGatewayTimeout, FailureDeadlineExceeded, false)
			return
		}
		status, category, retryable := resolutionFailure(err)
		h.writePlatformFailure(w, status, category, retryable)
		return
	}
	admissionRequest, err := prepareAdmissionRequest(resolved, invocation, appInput, h.limits.MaxResponseBytes)
	if err != nil {
		h.writePlatformFailure(w, http.StatusInternalServerError, FailureInternal, false)
		return
	}
	admitted, err := h.admission.CreateRun(operationContext, admissionRequest)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(operationContext.Err(), context.DeadlineExceeded) {
			h.writePlatformFailure(w, http.StatusGatewayTimeout, FailureDeadlineExceeded, false)
			return
		}
		status, category, retryable := admissionFailure(err)
		h.writePlatformFailure(w, status, category, retryable)
		return
	}
	run, err := h.waitForRun(operationContext, admitted.Run, admissionRequest.Principal, invocation.DeadlineAt)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			h.writePlatformFailure(w, http.StatusGatewayTimeout, FailureDeadlineExceeded, false)
			return
		}
		status, category, retryable := pollFailure(err)
		h.writePlatformFailure(w, status, category, retryable)
		return
	}
	rawResult := runOutput(run)
	wireResponse, responseBody, wireErr := decodeApplicationWireResponse(rawResult, admissionRequest.ResponsePolicy.MaxBodyBytes)
	validWireResponse := wireErr == nil && wireResponse.Status >= 200 &&
		(wireResponse.Status != http.StatusNoContent || len(responseBody) == 0) &&
		(wireResponse.Status != http.StatusNotModified || len(responseBody) == 0) &&
		(len(wireResponse.Headers) == 0 && admissionRequest.ResponsePolicy.AllowMissingContentType ||
			len(wireResponse.Headers) == 1 && allowedResponseContentType(admissionRequest.ResponsePolicy, wireResponse.Headers[0].Value))
	if validWireResponse {
		if len(wireResponse.Headers) == 1 {
			w.Header().Set("Content-Type", wireResponse.Headers[0].Value)
		} else {
			// Presence with an empty value suppresses net/http content sniffing while
			// emitting no application-supplied content-type value.
			w.Header()["Content-Type"] = nil
		}
		w.WriteHeader(wireResponse.Status)
		_, _ = w.Write(responseBody)
		return
	}
	if run.State != state.RunSucceeded {
		status, category, retryable := terminalRunFailure(run)
		h.writePlatformFailure(w, status, category, retryable)
		return
	}
	if len(rawResult) == 0 {
		h.writePlatformFailure(w, http.StatusBadGateway, FailureInternal, false)
		return
	}
	h.writePlatformFailure(w, http.StatusBadGateway, FailureApplicationProtocolViolation, false)
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

func prepareAdmissionRequest(
	resolved ResolvedAdmission,
	invocation OpaqueHTTPInvocationV1,
	appInput []byte,
	maxResponseBytes int64,
) (execution.CreateRunRequest, error) {
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
		len(principal.Scopes) != 2 || len(principal.AllowedTargets) != 1 || principal.AllowedTargets[0] != target {
		return execution.CreateRunRequest{}, errors.New("resolver did not return an exact scoped service principal")
	}
	if err := validateResolvedInvocationPins(resolved.InvocationPins, invocation); err != nil {
		return execution.CreateRunRequest{}, err
	}
	if err := validateResolvedResponsePolicy(resolved.ResponsePolicy, maxResponseBytes); err != nil {
		return execution.CreateRunRequest{}, err
	}
	return execution.CreateRunRequest{
		Workspace:           workspace,
		App:                 app,
		Action:              action,
		ExpectedRelease:     &expected,
		InvocationPins:      contract.CloneInvocationPins(resolved.InvocationPins),
		ResponsePolicy:      contract.CloneHTTPPolicy(resolved.ResponsePolicy),
		Input:               append(json.RawMessage(nil), appInput...),
		InputConfigResolved: true,
		Adapter:             adapterName,
		Principal:           principal,
	}, nil
}

func validateResolvedInvocationPins(pins contract.InvocationPins, invocation OpaqueHTTPInvocationV1) error {
	trusted := invocation.TrustedIngress
	if pins.PublicationRef != trusted.PublicationRef || pins.RouteGeneration != trusted.RouteGeneration {
		return errors.New("resolver invocation pins do not match the trusted route")
	}
	if len(pins.OperationRef) == 0 || len(pins.OperationRef) > 200 || !operationRefPattern.MatchString(pins.OperationRef) {
		return errors.New("resolver returned an invalid operation reference")
	}
	if pins.CredentialRef.ID != trusted.CredentialRef.ID || pins.CredentialRef.Version != trusted.CredentialRef.Revision {
		return errors.New("resolver credential pin does not match the trusted credential reference")
	}
	if len(pins.References) == 0 || len(pins.References) > 32 {
		return errors.New("resolver must return bounded immutable invocation references")
	}
	seen := make(map[string]struct{}, len(pins.References))
	for _, pin := range pins.References {
		if len(pin.Name) == 0 || len(pin.Name) > 80 || !pinNamePattern.MatchString(pin.Name) {
			return errors.New("resolver returned an invalid invocation reference name")
		}
		if _, duplicate := seen[pin.Name]; duplicate {
			return errors.New("resolver returned duplicate invocation reference names")
		}
		seen[pin.Name] = struct{}{}
		if err := validateTrimmedString("invocation reference id", pin.Reference.ID, 200); err != nil {
			return err
		}
		if err := validateTrimmedString("invocation reference version", pin.Reference.Version, 200); err != nil {
			return err
		}
	}
	return nil
}

func validateResolvedResponsePolicy(policy contract.HTTPPolicy, maxResponseBytes int64) error {
	if policy.MaxBodyBytes <= 0 || policy.MaxBodyBytes > maxResponseBytes {
		return errors.New("resolver response byte limit exceeds the handler limit")
	}
	if len(policy.ContentTypes) == 0 || len(policy.ContentTypes) > 16 {
		return errors.New("resolver must return bounded response content types")
	}
	seen := make(map[string]struct{}, len(policy.ContentTypes))
	for _, contentType := range policy.ContentTypes {
		if len(contentType) == 0 || len(contentType) > 160 || strings.TrimSpace(contentType) != contentType || hasControlCharacter(contentType) {
			return errors.New("resolver returned an invalid response content type")
		}
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || !strings.Contains(mediaType, "/") || strings.Contains(mediaType, "*") {
			return errors.New("resolver returned an invalid response content type")
		}
		if _, duplicate := seen[contentType]; duplicate {
			return errors.New("resolver returned duplicate response content types")
		}
		seen[contentType] = struct{}{}
	}
	return nil
}

func allowedResponseContentType(policy contract.HTTPPolicy, contentType string) bool {
	for _, allowed := range policy.ContentTypes {
		if contentType == allowed {
			return true
		}
	}
	return false
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
		// PollInterval is a maximum cadence. Never sleep the entire remaining
		// budget, otherwise a Run that finishes during that sleep cannot be
		// observed before the deadline.
		wait := h.limits.PollInterval
		if halfRemaining := remaining / 2; halfRemaining > 0 && wait > halfRemaining {
			wait = halfRemaining
		}
		if wait <= 0 {
			wait = remaining
		}
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

func pollFailure(err error) (int, FailureCategory, bool) {
	switch execution.FaultKindOf(err) {
	case execution.FaultUnavailable:
		return http.StatusServiceUnavailable, FailureCapacityUnavailable, false
	default:
		// Once Admission returned a Run, target, ownership, and not-found faults
		// are internal consistency failures rather than caller protocol errors.
		return http.StatusInternalServerError, FailureInternal, false
	}
}

func terminalRunFailure(run state.Run) (int, FailureCategory, bool) {
	if run.State == state.RunExpired || run.Result != nil && run.Result.Interruption != nil &&
		(run.Result.Interruption.Cause == contract.InterruptionLeaseLost ||
			run.Result.Interruption.Cause == contract.InterruptionWorkerShutdown) {
		return http.StatusBadGateway, FailureWorkerLost, false
	}
	return http.StatusBadGateway, FailureInternal, false
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
