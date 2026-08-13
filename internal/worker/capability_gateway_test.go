package worker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCapabilityGatewayBindingDiscoversBindsAndClosesRun(t *testing.T) {
	const (
		workerToken = "worker-secret"
		runToken    = "run-secret"
	)
	var creates atomic.Int32
	var closes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/capabilities":
			writeCapabilityTestJSON(t, w, http.StatusOK, map[string]any{
				"capabilities": []map[string]any{
					{"id": "document.pdf/v1", "operations": []string{"recover", "transform", "validate"}, "ready": true, "maxConcurrency": 1},
					{"id": "spreadsheet/v1", "operations": []string{"parse"}, "ready": false, "maxConcurrency": 1},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/runs":
			creates.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer "+workerToken {
				t.Errorf("create authorization = %q", got)
			}
			var body struct {
				TTLSeconds uint64 `json:"ttlSeconds"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			if body.TTLSeconds != 360 {
				t.Errorf("ttlSeconds = %d, want 360", body.TTLSeconds)
			}
			writeCapabilityTestJSON(t, w, http.StatusCreated, map[string]any{
				"runRef": "run-123", "runToken": runToken, "expiresInSeconds": 360,
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/runs/run-123":
			closes.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer "+runToken {
				t.Errorf("close authorization = %q", got)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("TEST_CAPABILITY_GATEWAY_TOKEN", workerToken)

	binding, err := NewCapabilityGatewayBinding(
		server.URL,
		"TEST_CAPABILITY_GATEWAY_TOKEN",
		"",
		time.Second,
		[]string{"document.pdf.v1"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(binding.Capabilities) != 1 || binding.Capabilities[0] != "document.pdf/v1" {
		t.Fatalf("ready capabilities = %#v", binding.Capabilities)
	}
	runtimeBindings := RuntimeBindings{
		AuthSession:       AuthSessionBinding{ServiceURL: "http://auth-session:8005", JWT: "auth-secret", Timeout: time.Second},
		CapabilityGateway: binding,
	}
	result, err := runtimeBindings.Bind(
		context.Background(),
		json.RawMessage(`{"region":"kr"}`),
		[]string{"browser", "document.pdf.v1"},
		5*time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	if creates.Load() != 1 {
		t.Fatalf("create count = %d", creates.Load())
	}
	if strings.Contains(string(result.Input), workerToken) {
		t.Fatal("worker token leaked into action input")
	}
	if len(result.SecretValues) != 2 || result.SecretValues[0] != "auth-secret" || result.SecretValues[1] != runToken {
		t.Fatalf("secret values = %#v", result.SecretValues)
	}
	var input struct {
		Region  string `json:"region"`
		Runtime struct {
			Capabilities struct {
				BaseURL   string   `json:"baseUrl"`
				RunRef    string   `json:"runRef"`
				RunToken  string   `json:"runToken"`
				Available []string `json:"available"`
			} `json:"capabilities"`
		} `json:"_SCRAPING_RUNTIME"`
	}
	if err := json.Unmarshal(result.Input, &input); err != nil {
		t.Fatal(err)
	}
	if input.Region != "kr" || input.Runtime.Capabilities.BaseURL != server.URL ||
		input.Runtime.Capabilities.RunRef != "run-123" || input.Runtime.Capabilities.RunToken != runToken ||
		len(input.Runtime.Capabilities.Available) != 1 || input.Runtime.Capabilities.Available[0] != "document.pdf/v1" {
		t.Fatalf("bound input = %#v", input)
	}
	if err := result.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if closes.Load() != 1 {
		t.Fatalf("close count = %d", closes.Load())
	}

	unmatched, err := runtimeBindings.Bind(
		context.Background(),
		json.RawMessage(`{"region":"kr"}`),
		[]string{"browser"},
		5*time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	if creates.Load() != 1 || strings.Contains(string(unmatched.Input), `"capabilities"`) {
		t.Fatalf("unmatched binding created a run: count=%d input=%s", creates.Load(), unmatched.Input)
	}
}

func TestCapabilityGatewayBindingRejectsUnsafeOrInvalidGateway(t *testing.T) {
	t.Setenv("TEST_CAPABILITY_GATEWAY_TOKEN", "worker-secret")
	if _, err := NewCapabilityGatewayBinding(
		"http://example.com:18092",
		"TEST_CAPABILITY_GATEWAY_TOKEN",
		"",
		time.Second,
		[]string{"document.pdf.v1"},
	); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("non-loopback URL error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "do-not-leak-response-secret")
	}))
	defer server.Close()
	_, err := NewCapabilityGatewayBinding(
		server.URL,
		"TEST_CAPABILITY_GATEWAY_TOKEN",
		"",
		time.Second,
		[]string{"document.pdf.v1"},
	)
	if err == nil {
		t.Fatal("expected discovery error")
	}
	if strings.Contains(err.Error(), "do-not-leak") || strings.Contains(err.Error(), "worker-secret") {
		t.Fatalf("discovery error leaked secret material: %v", err)
	}
}

func writeCapabilityTestJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}
