package runtime

import (
	"encoding/json"
	"errors"
)

type namedError struct {
	name string
	err  error
}

func (e *namedError) Error() string {
	if e.err == nil {
		return e.name
	}
	return e.err.Error()
}

func (e *namedError) Unwrap() error {
	return e.err
}

func newNamedError(name string, err error) error {
	if err == nil {
		return nil
	}
	return &namedError{name: name, err: err}
}

func ErrorName(err error) (string, bool) {
	var named *namedError
	if errors.As(err, &named) && named.name != "" {
		return named.name, true
	}
	return "", false
}

func ErrorResult(name string, message string) json.RawMessage {
	if name == "" {
		name = "Error"
	}
	body, _ := json.Marshal(map[string]string{"name": name, "message": message})
	return body
}

// FailureMetadata carries a machine-readable classification for an engine
// failure that happened before an App could return its own result. Producers
// must supply registered non-secret codes; syntax alone cannot establish that a
// value is semantically safe.
type FailureMetadata struct {
	Phase     string `json:"phase"`
	Reason    string `json:"reason"`
	Retryable bool   `json:"retryable"`
}

// ErrorResultWithMetadata preserves the existing name/message error contract
// while adding an optional engine-owned classification for newer consumers.
func ErrorResultWithMetadata(name string, message string, metadata FailureMetadata) json.RawMessage {
	if name == "" {
		name = "Error"
	}
	body, _ := json.Marshal(struct {
		Name    string `json:"name"`
		Message string `json:"message"`
		FailureMetadata
	}{
		Name:            name,
		Message:         message,
		FailureMetadata: metadata,
	})
	return body
}
