package controlcli

import (
	"encoding/json"
	"regexp"
	"strings"
)

const redactedValue = "[REDACTED]"

var (
	bearerDiagnosticPattern = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`)
	wfTokenPattern          = regexp.MustCompile(`\b(?:wfjob|wfw|wfk|wfs|wfr)_[A-Za-z0-9_-]+\b`)
)

func redactDiagnostic(value string) string {
	value = bearerDiagnosticPattern.ReplaceAllString(value, "Bearer "+redactedValue)
	return wfTokenPattern.ReplaceAllString(value, redactedValue)
}

func redactErrorJSON(data []byte) []byte {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return []byte(redactDiagnostic(string(data)))
	}
	redactJSONValue(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		return []byte(redactedValue)
	}
	return encoded
}

func redactJSONValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if sensitiveDiagnosticKey(key) {
				typed[key] = redactedValue
				continue
			}
			if text, ok := item.(string); ok {
				typed[key] = redactDiagnostic(text)
				continue
			}
			redactJSONValue(item)
		}
	case []any:
		for index, item := range typed {
			if text, ok := item.(string); ok {
				typed[index] = redactDiagnostic(text)
				continue
			}
			redactJSONValue(item)
		}
	}
}

func sensitiveDiagnosticKey(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "authorization",
		"cookie",
		"set-cookie",
		"password",
		"secret",
		"client_secret",
		"access_token",
		"refresh_token",
		"id_token",
		"api_token",
		"device_code",
		"user_code",
		"authorization_code",
		"code_verifier":
		return true
	default:
		return false
	}
}
