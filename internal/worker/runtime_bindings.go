package worker

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/state"
)

type RuntimeBindings struct {
	AuthSession AuthSessionBinding
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
