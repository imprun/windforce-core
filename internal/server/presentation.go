package server

import (
	"fmt"
	"strings"
)

const (
	UIModeEmbedded = "embedded"
	UIModeDisabled = "disabled"

	WorkerGroupOperatorSelfManaged = "self-managed"
	WorkerGroupOperatorExternal    = "external"
)

// ParseUIMode validates the independent Web UI presentation mode. An empty
// value preserves the historical behavior and serves the embedded UI.
func ParseUIMode(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return UIModeEmbedded, nil
	}
	switch value {
	case UIModeEmbedded, UIModeDisabled:
		return value, nil
	default:
		return "", fmt.Errorf("must be %q or %q", UIModeEmbedded, UIModeDisabled)
	}
}

// ParseWorkerGroupOperator validates who owns Worker Group lifecycle and
// capacity. It is presentation metadata only; it never disables Core APIs.
func ParseWorkerGroupOperator(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return WorkerGroupOperatorSelfManaged, nil
	}
	switch value {
	case WorkerGroupOperatorSelfManaged, WorkerGroupOperatorExternal:
		return value, nil
	default:
		return "", fmt.Errorf("must be %q or %q", WorkerGroupOperatorSelfManaged, WorkerGroupOperatorExternal)
	}
}
