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
