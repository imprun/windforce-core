package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/state"
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
			if got := r.Header.Get(capabilityRunIDHeader); got != "run-456" {
				t.Errorf("run ID header = %q", got)
			}
			if got := r.Header.Get(capabilityJobIDHeader); got != "job-789" {
				t.Errorf("job ID header = %q", got)
			}
			if got := r.Header.Get(capabilityJobAttemptHeader); got != "2" {
				t.Errorf("job attempt header = %q", got)
			}
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			var ttlSeconds uint64
			if len(body) != 1 || json.Unmarshal(body["ttlSeconds"], &ttlSeconds) != nil || ttlSeconds != 360 {
				t.Errorf("create body = %#v, want only ttlSeconds=360", body)
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
		RuntimeBindingContext{RunID: "run-456", JobID: "job-789", Attempt: 2},
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
		RuntimeBindingContext{},
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

func TestProcessorClosesAndMasksCapabilityGatewayRun(t *testing.T) {
	const (
		workerToken = "worker-secret-value"
		runToken    = "job-run-secret-value"
	)
	var creates atomic.Int32
	var closes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/capabilities":
			writeCapabilityTestJSON(t, w, http.StatusOK, map[string]any{
				"capabilities": []map[string]any{
					{"id": "document.pdf/v1", "operations": []string{"transform"}, "ready": true, "maxConcurrency": 1},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/runs":
			creates.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer "+workerToken {
				t.Errorf("create authorization = %q", got)
			}
			writeCapabilityTestJSON(t, w, http.StatusCreated, map[string]any{
				"runRef": "run-processor", "runToken": runToken, "expiresInSeconds": 3600,
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/runs/run-processor":
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

	processor, stateStore, run := newProcessorTestHarnessWithDeployment(t, "echo", func(deployment *contract.Deployment) {
		runsOn := []string{"document.pdf.v1"}
		action := deployment.Actions["echo"]
		action.RunsOn = &runsOn
		deployment.Actions["echo"] = action
	})
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
	processor.Labels = append(processor.Labels, binding.Labels...)
	processor.RuntimeBindings.CapabilityGateway = binding

	processed, err := processor.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("ProcessOne = %v, %v", processed, err)
	}
	if creates.Load() != 1 || closes.Load() != 1 {
		t.Fatalf("gateway lifecycle creates=%d closes=%d", creates.Load(), closes.Load())
	}
	completed, err := stateStore.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(completed.Output), workerToken) || strings.Contains(string(completed.Output), runToken) {
		t.Fatalf("completed output leaked gateway credential: %s", completed.Output)
	}
	if !strings.Contains(string(completed.Output), strings.Repeat("*", len(runToken))) {
		t.Fatalf("completed output did not mask the Job token: %s", completed.Output)
	}
}

func TestProcessorPreservesSafeCapabilityGatewayFailureMetadata(t *testing.T) {
	const responseSecret = "gateway-response-secret"
	var creates atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/capabilities":
			writeCapabilityTestJSON(t, w, http.StatusOK, map[string]any{
				"capabilities": []map[string]any{
					{"id": "document.pdf/v1", "operations": []string{"transform"}, "ready": true, "maxConcurrency": 1},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/runs":
			creates.Add(1)
			writeCapabilityTestJSON(t, w, http.StatusServiceUnavailable, map[string]any{
				"error":     "capability_unavailable",
				"phase":     "provider_selection",
				"reason":    "capacity_unavailable",
				"retryable": true,
				"detail":    responseSecret,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("TEST_CAPABILITY_GATEWAY_TOKEN", "worker-secret")

	processor, stateStore, run := newProcessorTestHarnessWithDeployment(t, "echo", func(deployment *contract.Deployment) {
		runsOn := []string{"document.pdf.v1"}
		action := deployment.Actions["echo"]
		action.RunsOn = &runsOn
		deployment.Actions["echo"] = action
	})
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
	processor.Labels = append(processor.Labels, binding.Labels...)
	processor.RuntimeBindings.CapabilityGateway = binding

	processed, err := processor.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("ProcessOne = %v, %v", processed, err)
	}
	completed, err := stateStore.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if creates.Load() != 1 || completed.State != state.RunFailed || completed.Result == nil {
		t.Fatalf("binding failure execution = creates=%d run=%#v", creates.Load(), completed)
	}
	if completed.Result.ExitCode != -1 || completed.Result.Error != "could not apply runtime bindings" {
		t.Fatalf("legacy failure fields = %#v", completed.Result)
	}
	var output struct {
		Name      string `json:"name"`
		Message   string `json:"message"`
		Phase     string `json:"phase"`
		Reason    string `json:"reason"`
		Retryable bool   `json:"retryable"`
	}
	if err := json.Unmarshal(completed.Result.Output, &output); err != nil {
		t.Fatal(err)
	}
	if output.Name != "RuntimeBindingError" || output.Message != "could not apply runtime bindings" ||
		output.Phase != "provider_selection" || output.Reason != "capacity_unavailable" || !output.Retryable {
		t.Fatalf("runtime binding output = %#v", output)
	}
	if strings.Contains(string(completed.Result.Output), responseSecret) {
		t.Fatalf("runtime binding output leaked gateway response detail: %s", completed.Result.Output)
	}
}

func TestCapabilityRunTTLUsesPinnedTimeoutAndFallsBackToGatewayMaximum(t *testing.T) {
	timeout := int32(15)
	if got := capabilityRunTTL(contract.Deployment{TimeoutS: 30}, contract.Action{TimeoutS: &timeout}); got != 15*time.Second {
		t.Fatalf("action timeout TTL = %s", got)
	}
	if got := capabilityRunTTL(contract.Deployment{TimeoutS: 30}, contract.Action{}); got != 30*time.Second {
		t.Fatalf("deployment timeout TTL = %s", got)
	}
	if got := capabilityRunTTL(contract.Deployment{}, contract.Action{}); got != 0 || capabilityTTLSeconds(got) != 3600 {
		t.Fatalf("unbounded timeout TTL = %s (%d seconds)", got, capabilityTTLSeconds(got))
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

func TestCapabilityGatewayBindingRejectsRedirects(t *testing.T) {
	t.Setenv("TEST_CAPABILITY_GATEWAY_TOKEN", "worker-secret")
	var followed atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		followed.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	_, err := NewCapabilityGatewayBinding(
		redirect.URL,
		"TEST_CAPABILITY_GATEWAY_TOKEN",
		"",
		time.Second,
		[]string{"document.pdf.v1"},
	)
	if err == nil || !strings.Contains(err.Error(), "discovery failed") {
		t.Fatalf("redirect error = %v", err)
	}
	if followed.Load() {
		t.Fatal("capability gateway redirect was followed")
	}
}

func TestCapabilityGatewayBindingRejectsExcessiveLifetimeAndCleanupTimeout(t *testing.T) {
	const runToken = "run-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			writeCapabilityTestJSON(t, w, http.StatusCreated, map[string]any{
				"runRef": "run-123", "runToken": runToken, "expiresInSeconds": 3601,
			})
		case http.MethodDelete:
			w.WriteHeader(http.StatusRequestTimeout)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	binding := CapabilityGatewayBinding{
		ServiceURL:  server.URL,
		WorkerToken: "worker-secret",
		Timeout:     time.Second,
		client:      newCapabilityGatewayHTTPClient(time.Second),
	}
	if _, err := binding.open(
		context.Background(),
		RuntimeBindingContext{RunID: "run-456", JobID: "job-789", Attempt: 1},
		time.Minute,
	); err == nil || !strings.Contains(err.Error(), "lifetime") {
		t.Fatalf("excessive lifetime error = %v", err)
	}
	if err := binding.close(context.Background(), capabilityGatewaySession{RunRef: "run-123", RunToken: runToken}); err == nil || !strings.Contains(err.Error(), "408") {
		t.Fatalf("cleanup timeout error = %v", err)
	}
}

func TestCapabilityGatewayRunFailureUsesBoundedSafeMetadata(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		body          string
		wantPhase     string
		wantReason    string
		wantRetryable bool
		secret        string
	}{
		{
			name:          "safe additive envelope",
			status:        http.StatusServiceUnavailable,
			body:          `{"error":"capability_unavailable","phase":"provider_selection","reason":"capacity_unavailable","retryable":true,"detail":"response-secret"}`,
			wantPhase:     "provider_selection",
			wantReason:    "capacity_unavailable",
			wantRetryable: true,
			secret:        "response-secret",
		},
		{
			name:          "legacy error only",
			status:        http.StatusBadRequest,
			body:          `{"error":"invalid_request"}`,
			wantPhase:     capabilityRunOpenPhase,
			wantReason:    "invalid_request",
			wantRetryable: false,
		},
		{
			name:          "empty",
			status:        http.StatusServiceUnavailable,
			wantPhase:     capabilityRunOpenPhase,
			wantReason:    capabilityGatewayReasonRejected,
			wantRetryable: true,
		},
		{
			name:          "non JSON",
			status:        http.StatusServiceUnavailable,
			body:          "response-secret",
			wantPhase:     capabilityRunOpenPhase,
			wantReason:    capabilityGatewayReasonRejected,
			wantRetryable: true,
			secret:        "response-secret",
		},
		{
			name:          "oversized",
			status:        http.StatusServiceUnavailable,
			body:          `{"error":"` + strings.Repeat("x", maxCapabilityGatewayErrorBytes) + `"}`,
			wantPhase:     capabilityRunOpenPhase,
			wantReason:    capabilityGatewayReasonRejected,
			wantRetryable: true,
		},
		{
			name:          "unsafe codes",
			status:        http.StatusBadRequest,
			body:          `{"phase":"provider selection","reason":"token=response-secret","retryable":true}`,
			wantPhase:     capabilityRunOpenPhase,
			wantReason:    capabilityGatewayReasonRejected,
			wantRetryable: true,
			secret:        "response-secret",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()
			binding := CapabilityGatewayBinding{
				ServiceURL:  server.URL,
				WorkerToken: "worker-secret",
				Timeout:     time.Second,
				client:      newCapabilityGatewayHTTPClient(time.Second),
			}
			_, err := binding.open(
				context.Background(),
				RuntimeBindingContext{RunID: "run-456", JobID: "job-789", Attempt: 1},
				time.Minute,
			)
			var failure *RuntimeBindingFailure
			if !errors.As(err, &failure) {
				t.Fatalf("open error = %v, want RuntimeBindingFailure", err)
			}
			if failure.Phase != tt.wantPhase || failure.Reason != tt.wantReason || failure.Retryable != tt.wantRetryable {
				t.Fatalf("failure = %#v", failure)
			}
			if tt.secret != "" && strings.Contains(err.Error(), tt.secret) {
				t.Fatalf("failure leaked response content: %v", err)
			}
		})
	}
}

func TestCapabilityGatewayRunTransportFailureIsSafeAndRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	serviceURL := server.URL
	server.Close()
	binding := CapabilityGatewayBinding{
		ServiceURL:  serviceURL,
		WorkerToken: "worker-secret",
		Timeout:     time.Second,
		client:      newCapabilityGatewayHTTPClient(time.Second),
	}
	_, err := binding.open(
		context.Background(),
		RuntimeBindingContext{RunID: "run-456", JobID: "job-789", Attempt: 1},
		time.Minute,
	)
	var failure *RuntimeBindingFailure
	if !errors.As(err, &failure) || failure.Phase != capabilityRunOpenPhase ||
		failure.Reason != capabilityGatewayReasonOffline || !failure.Retryable {
		t.Fatalf("transport failure = %#v, err=%v", failure, err)
	}
	if strings.Contains(err.Error(), serviceURL) || strings.Contains(err.Error(), "worker-secret") {
		t.Fatalf("transport failure leaked connection or credential details: %v", err)
	}
}

func TestCapabilityGatewayTransportFailureClassifiesTimeoutAndCancellation(t *testing.T) {
	timeout := capabilityGatewayTransportFailure(context.DeadlineExceeded)
	if timeout.Phase != capabilityRunOpenPhase || timeout.Reason != capabilityGatewayReasonTimeout || !timeout.Retryable {
		t.Fatalf("timeout failure = %#v", timeout)
	}
	canceled := capabilityGatewayTransportFailure(context.Canceled)
	if canceled.Phase != capabilityRunOpenPhase || canceled.Reason != runtimeBindingReasonCanceled || canceled.Retryable {
		t.Fatalf("canceled failure = %#v", canceled)
	}
}

func TestCapabilityGatewayRunSuccessResponseRemainsStrict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeCapabilityTestJSON(t, w, http.StatusCreated, map[string]any{
			"runRef": "run-123", "runToken": "run-secret", "expiresInSeconds": 60,
			"phase": capabilityRunOpenPhase,
		})
	}))
	defer server.Close()
	binding := CapabilityGatewayBinding{
		ServiceURL:  server.URL,
		WorkerToken: "worker-secret",
		Timeout:     time.Second,
		client:      newCapabilityGatewayHTTPClient(time.Second),
	}
	_, err := binding.open(
		context.Background(),
		RuntimeBindingContext{RunID: "run-456", JobID: "job-789", Attempt: 1},
		time.Minute,
	)
	var failure *RuntimeBindingFailure
	if err == nil || !strings.Contains(err.Error(), "invalid run response") || errors.As(err, &failure) {
		t.Fatalf("strict success response error = %v, failure=%#v", err, failure)
	}
}

func TestRuntimeBindingFailureMetadataRejectsUnsafeTypedCodes(t *testing.T) {
	fallback := runtimeBindingFailureMetadata(errors.New("raw-secret-detail"))
	if fallback.Phase != runtimeBindingPhase || fallback.Reason != runtimeBindingReasonFailed || fallback.Retryable {
		t.Fatalf("generic binding failure = %#v", fallback)
	}
	metadata := runtimeBindingFailureMetadata(&RuntimeBindingFailure{
		Phase: "unsafe phase", Reason: "token=response-secret", Retryable: true,
	})
	if metadata.Phase != runtimeBindingPhase || metadata.Reason != runtimeBindingReasonFailed || !metadata.Retryable {
		t.Fatalf("sanitized typed failure = %#v", metadata)
	}
}

func TestCapabilityGatewayBindingRejectsInvalidRunContext(t *testing.T) {
	binding := CapabilityGatewayBinding{ServiceURL: "http://127.0.0.1:1", WorkerToken: "worker-secret"}
	tests := []struct {
		name      string
		execution RuntimeBindingContext
		want      string
	}{
		{name: "missing run", execution: RuntimeBindingContext{JobID: "job-789", Attempt: 1}, want: "run ID"},
		{name: "unsafe job", execution: RuntimeBindingContext{RunID: "run-456", JobID: "job\r\nunsafe", Attempt: 1}, want: "job ID"},
		{name: "missing attempt", execution: RuntimeBindingContext{RunID: "run-456", JobID: "job-789"}, want: "attempt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := binding.open(context.Background(), tt.execution, time.Minute); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("open error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestCapabilityGatewayDialerRejectsNonLoopbackTarget(t *testing.T) {
	if _, err := dialCapabilityGatewayLoopback(context.Background(), "tcp", "192.0.2.1:80"); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("non-loopback dial error = %v", err)
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
