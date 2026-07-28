package state

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	defaultTriggerWaitTimeoutSeconds = 30
	maxTriggerWaitTimeoutSeconds     = 60
)

func NormalizeTriggerCompletionPolicy(policy TriggerCompletionPolicy) (TriggerCompletionPolicy, error) {
	policy.Mode = strings.ToLower(strings.TrimSpace(policy.Mode))
	if policy.Mode == "" {
		policy.Mode = TriggerCompletionModeNone
	}
	switch policy.Mode {
	case TriggerCompletionModeNone, TriggerCompletionModePoll:
		policy.Callback = nil
		policy.Publish = nil
	case TriggerCompletionModeCallback:
		if policy.Callback == nil {
			return TriggerCompletionPolicy{}, fmt.Errorf("%w: callback completion requires callback settings", ErrInvalidState)
		}
		policy.Callback.Endpoint = strings.TrimSpace(policy.Callback.Endpoint)
		endpoint, err := url.Parse(policy.Callback.Endpoint)
		if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
			return TriggerCompletionPolicy{}, fmt.Errorf("%w: callback completion endpoint must be an absolute URL", ErrInvalidState)
		}
		policy.Publish = nil
	case TriggerCompletionModePublish:
		if policy.Publish == nil {
			return TriggerCompletionPolicy{}, fmt.Errorf("%w: publish completion requires publish settings", ErrInvalidState)
		}
		policy.Publish.Exchange = strings.TrimSpace(policy.Publish.Exchange)
		policy.Publish.RoutingKey = strings.TrimSpace(policy.Publish.RoutingKey)
		if policy.Publish.RoutingKey == "" {
			return TriggerCompletionPolicy{}, fmt.Errorf("%w: publish completion routing_key is required", ErrInvalidState)
		}
		policy.Callback = nil
	default:
		return TriggerCompletionPolicy{}, fmt.Errorf("%w: unsupported trigger completion mode %q", ErrInvalidState, policy.Mode)
	}
	return policy, nil
}

func NormalizeTriggerResponsePolicy(kind string, policy TriggerResponsePolicy) (TriggerResponsePolicy, error) {
	policy.Mode = strings.ToLower(strings.TrimSpace(policy.Mode))
	if policy.Mode == "" {
		policy.Mode = TriggerResponseAsync
	}
	switch policy.Mode {
	case TriggerResponseAsync:
		policy.TimeoutSeconds = 0
	case TriggerResponseWait:
		if strings.ToLower(strings.TrimSpace(kind)) != "webhook" {
			return TriggerResponsePolicy{}, fmt.Errorf("%w: wait response mode is available only for webhook triggers", ErrInvalidState)
		}
		if policy.TimeoutSeconds == 0 {
			policy.TimeoutSeconds = defaultTriggerWaitTimeoutSeconds
		}
		if policy.TimeoutSeconds < 1 || policy.TimeoutSeconds > maxTriggerWaitTimeoutSeconds {
			return TriggerResponsePolicy{}, fmt.Errorf("%w: wait response timeout must be between 1 and %d seconds", ErrInvalidState, maxTriggerWaitTimeoutSeconds)
		}
	default:
		return TriggerResponsePolicy{}, fmt.Errorf("%w: unsupported trigger response mode %q", ErrInvalidState, policy.Mode)
	}
	return policy, nil
}

func InitialTriggerCompletionState(policy TriggerCompletionPolicy) string {
	switch policy.Mode {
	case TriggerCompletionModeNone:
		return TriggerCompletionIgnored
	default:
		return TriggerCompletionWaiting
	}
}

func TriggerCompletionTerminal(state string) bool {
	switch state {
	case TriggerCompletionIgnored, TriggerCompletionAvailable, TriggerCompletionSucceeded, TriggerCompletionFailed:
		return true
	default:
		return false
	}
}

func validateTriggerCompletionResult(result TriggerCompletionResult) error {
	switch result.State {
	case TriggerCompletionRetrying:
		if result.NextAttemptAt.IsZero() {
			return fmt.Errorf("%w: completion retry requires next_attempt_at", ErrInvalidState)
		}
	case TriggerCompletionSucceeded, TriggerCompletionFailed:
	default:
		return fmt.Errorf("%w: invalid completion result state %q", ErrInvalidState, result.State)
	}
	return nil
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
