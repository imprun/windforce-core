package controlcli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

type repeatedStringFlag []string

func (values *repeatedStringFlag) String() string {
	return strings.Join(*values, ",")
}

func (values *repeatedStringFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func (r *runner) api(args []string) error {
	if len(args) == 0 {
		return usageError{"usage: " + r.program + " api <endpoint> [flags]"}
	}
	endpoint := strings.TrimSpace(args[0])
	fs := r.flags("api")
	method := fs.String("method", "", "HTTP method")
	inputFile := fs.String("input", "", "JSON request body file, or - for standard input")
	var fields repeatedStringFlag
	var rawFields repeatedStringFlag
	fs.Var(&fields, "field", "typed JSON field in key=value form; repeatable")
	fs.Var(&rawFields, "raw-field", "string JSON field in key=value form; repeatable")
	if err := fs.Parse(args[1:]); err != nil {
		return usageError{err.Error()}
	}
	if fs.NArg() != 0 {
		return usageError{"usage: " + r.program + " api <endpoint> [flags]"}
	}
	path, err := r.apiPath(endpoint)
	if err != nil {
		return err
	}

	var body any
	if *inputFile != "" {
		if len(fields) > 0 || len(rawFields) > 0 {
			return usageError{"--input cannot be combined with --field or --raw-field"}
		}
		body, err = r.readJSON("{}", *inputFile)
		if err != nil {
			return err
		}
	} else if len(fields) > 0 || len(rawFields) > 0 {
		payload := map[string]any{}
		for _, field := range fields {
			key, value, err := parseAPIField(field, false)
			if err != nil {
				return err
			}
			payload[key] = value
		}
		for _, field := range rawFields {
			key, value, err := parseAPIField(field, true)
			if err != nil {
				return err
			}
			payload[key] = value
		}
		body = payload
	}

	httpMethod := strings.ToUpper(strings.TrimSpace(*method))
	if httpMethod == "" {
		httpMethod = http.MethodGet
		if body != nil {
			httpMethod = http.MethodPost
		}
	}
	switch httpMethod {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return usageError{"--method must be GET, POST, PUT, PATCH, or DELETE"}
	}
	raw, err := r.client.DoJSON(context.Background(), httpMethod, path, body)
	if err != nil {
		return err
	}
	return r.outputJSON(raw)
}

func (r *runner) apiPath(endpoint string) (string, error) {
	if endpoint == "" || strings.IndexFunc(endpoint, unicode.IsControl) >= 0 {
		return "", usageError{"API endpoint must be a non-empty path"}
	}
	if strings.Contains(endpoint, "://") || strings.HasPrefix(endpoint, "//") {
		return "", usageError{"API endpoint must be relative to the selected context host"}
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Fragment != "" {
		return "", usageError{"API endpoint must be a path without a fragment"}
	}
	if strings.HasPrefix(endpoint, "/") {
		return endpoint, nil
	}
	parts := strings.Split(strings.Trim(endpoint, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", usageError{"API endpoint must be a non-empty path"}
	}
	query := parsed.RawQuery
	parts = strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for _, part := range parts {
		if part == "." || part == ".." || part == "" {
			return "", usageError{"relative API endpoint contains an invalid path segment"}
		}
	}
	path := r.client.WorkspacePath(parts...)
	if query != "" {
		path += "?" + query
	}
	return path, nil
}

func parseAPIField(value string, raw bool) (string, any, error) {
	key, encoded, ok := strings.Cut(value, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" || strings.ContainsAny(key, ".[]\x00\r\n") {
		return "", nil, usageError{"API fields must use a top-level key=value form"}
	}
	if raw {
		return key, encoded, nil
	}
	encoded = strings.TrimSpace(encoded)
	switch encoded {
	case "true":
		return key, true, nil
	case "false":
		return key, false, nil
	case "null":
		return key, nil, nil
	}
	if integer, err := strconv.ParseInt(encoded, 10, 64); err == nil {
		return key, integer, nil
	}
	if number, err := strconv.ParseFloat(encoded, 64); err == nil {
		return key, number, nil
	}
	if strings.HasPrefix(encoded, "{") || strings.HasPrefix(encoded, "[") || strings.HasPrefix(encoded, `"`) {
		var decoded any
		if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
			return "", nil, usageError{fmt.Sprintf("invalid JSON value for field %q: %v", key, err)}
		}
		return key, decoded, nil
	}
	return key, encoded, nil
}
