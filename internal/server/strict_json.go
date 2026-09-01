package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const maxStrictJSONNestingDepth = 32

func readStrictJSONBody(reader io.Reader, maxBytes int64, target any) error {
	if reader == nil || maxBytes <= 0 || target == nil {
		return errors.New("request body is unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return errors.New("could not read request body")
	}
	if int64(len(raw)) > maxBytes {
		return errors.New("request body exceeds the configured limit")
	}
	if len(raw) == 0 || !json.Valid(raw) {
		return errors.New("request body must be one valid JSON value")
	}
	if err := rejectStrictJSONDuplicateMembers(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body has a trailing JSON value")
		}
		return err
	}
	return nil
}

func rejectStrictJSONDuplicateMembers(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := scanStrictJSONValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body has a trailing JSON value")
		}
		return err
	}
	return nil
}

func scanStrictJSONValue(decoder *json.Decoder, depth int) error {
	if depth > maxStrictJSONNestingDepth {
		return errors.New("JSON value exceeds the maximum nesting depth")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object member name must be a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON field %q", key)
			}
			seen[key] = struct{}{}
			if err := scanStrictJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := scanStrictJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}
