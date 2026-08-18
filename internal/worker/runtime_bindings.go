package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/state"
)

const (
	runtimeBindingPhase             = "runtime_binding"
	runtimeBindingReasonFailed      = "binding_failed"
	runtimeBindingReasonCanceled    = "binding_canceled"
	capabilityRunOpenPhase          = "capability_run_open"
	capabilityGatewayReasonRejected = "gateway_rejected"
	capabilityGatewayReasonTimeout  = "gateway_timeout"
	capabilityGatewayReasonOffline  = "gateway_unreachable"
)

type RuntimeBindings struct {
	AuthSession       AuthSessionBinding
	CapabilityGateway CapabilityGatewayBinding
}

type RuntimeBindingContext struct {
	RunID   string
	JobID   string
	Attempt int
}

// RuntimeBindingFailure carries classification data from a trusted runtime
// binding. Its codes must be validated again before public translation; syntax
// validation cannot determine whether an opaque value is semantically secret.
type RuntimeBindingFailure struct {
	Phase     string
	Reason    string
	Retryable bool
}

func (f *RuntimeBindingFailure) Error() string {
	if f == nil {
		return "runtime binding failed"
	}
	return fmt.Sprintf("runtime binding failed at %s: %s", f.Phase, f.Reason)
}

type RuntimeBindingResult struct {
	Input        json.RawMessage
	SecretValues []string
	close        func(context.Context) error
}

func (r RuntimeBindingResult) Close(ctx context.Context) error {
	if r.close == nil {
		return nil
	}
	return r.close(ctx)
}

type AuthSessionBinding struct {
	ServiceURL string
	JWT        string
	Timeout    time.Duration
}

func NewRuntimeBindings(authSessionURL string, authSessionTokenEnv string, authSessionTokenFile string, authSessionTimeout time.Duration) (RuntimeBindings, error) {
	binding := AuthSessionBinding{
		ServiceURL: strings.TrimSpace(authSessionURL),
		Timeout:    authSessionTimeout,
	}
	if binding.ServiceURL == "" {
		return RuntimeBindings{}, nil
	}
	tokenEnv := strings.TrimSpace(authSessionTokenEnv)
	if tokenEnv != "" {
		binding.JWT = strings.TrimSpace(os.Getenv(tokenEnv))
	}
	tokenFile := strings.TrimSpace(authSessionTokenFile)
	if binding.JWT == "" && tokenFile != "" {
		data, err := os.ReadFile(tokenFile)
		if err != nil {
			return RuntimeBindings{}, fmt.Errorf("read auth-session token file: %w", err)
		}
		binding.JWT = strings.TrimSpace(string(data))
	}
	if binding.JWT == "" {
		return RuntimeBindings{}, fmt.Errorf("auth-session binding requires a token from --auth-session-token-env or --auth-session-token-file")
	}
	return RuntimeBindings{AuthSession: binding}, nil
}

func (b RuntimeBindings) Apply(input json.RawMessage) (json.RawMessage, error) {
	var object map[string]json.RawMessage
	if len(input) > 0 {
		if err := json.Unmarshal(input, &object); err != nil || object == nil {
			return nil, fmt.Errorf("runtime bindings require object input")
		}
	} else {
		object = map[string]json.RawMessage{}
	}
	delete(object, state.ReservedRuntimeInputKey)
	if b.AuthSession.ServiceURL != "" {
		timeoutMs := int64(b.AuthSession.Timeout / time.Millisecond)
		if timeoutMs <= 0 {
			timeoutMs = 15000
		}
		payload, err := json.Marshal(map[string]any{
			"authSession": map[string]any{
				"serviceUrl": b.AuthSession.ServiceURL,
				"jwt":        b.AuthSession.JWT,
				"timeoutMs":  timeoutMs,
			},
		})
		if err != nil {
			return nil, err
		}
		object[state.ReservedRuntimeInputKey] = payload
	}
	return json.Marshal(object)
}

func (b RuntimeBindings) Bind(
	ctx context.Context,
	input json.RawMessage,
	execution RuntimeBindingContext,
	requiredLabels []string,
	ttl time.Duration,
) (RuntimeBindingResult, error) {
	bound, err := b.Apply(input)
	if err != nil {
		return RuntimeBindingResult{}, err
	}
	result := RuntimeBindingResult{Input: bound}
	if b.AuthSession.JWT != "" {
		result.SecretValues = append(result.SecretValues, b.AuthSession.JWT)
	}
	if !b.CapabilityGateway.Matches(requiredLabels) {
		return result, nil
	}
	session, err := b.CapabilityGateway.open(ctx, execution, ttl)
	if err != nil {
		return RuntimeBindingResult{}, err
	}
	bound, err = addCapabilityRuntimePayload(bound, b.CapabilityGateway, session)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), b.CapabilityGateway.Timeout)
		defer cancel()
		_ = b.CapabilityGateway.close(cleanupCtx, session)
		return RuntimeBindingResult{}, err
	}
	result.Input = bound
	result.SecretValues = append(result.SecretValues, session.RunToken)
	result.close = func(cleanupCtx context.Context) error {
		return b.CapabilityGateway.close(cleanupCtx, session)
	}
	return result, nil
}

func addCapabilityRuntimePayload(
	input json.RawMessage,
	binding CapabilityGatewayBinding,
	session capabilityGatewaySession,
) (json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(input, &object); err != nil || object == nil {
		return nil, errors.New("capability runtime binding requires object input")
	}
	runtimePayload := map[string]json.RawMessage{}
	if raw := object[state.ReservedRuntimeInputKey]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &runtimePayload); err != nil {
			return nil, errors.New("worker runtime metadata is invalid")
		}
	}
	capabilityPayload, err := json.Marshal(map[string]any{
		"baseUrl":   binding.ServiceURL,
		"runRef":    session.RunRef,
		"runToken":  session.RunToken,
		"available": append([]string(nil), binding.Capabilities...),
	})
	if err != nil {
		return nil, err
	}
	runtimePayload["capabilities"] = capabilityPayload
	rawRuntime, err := json.Marshal(runtimePayload)
	if err != nil {
		return nil, err
	}
	object[state.ReservedRuntimeInputKey] = rawRuntime
	return json.Marshal(object)
}
