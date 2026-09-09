package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/execution"
	"github.com/imprun/windforce-core/internal/opaquehttp"
	"github.com/imprun/windforce-core/internal/server"
	"github.com/imprun/windforce-core/internal/state"
)

type stubProjectionStore struct{}

func (stubProjectionStore) ResolveOpaqueIngressProjection(
	context.Context,
	state.OpaqueIngressResolutionRequest,
) (state.OpaqueIngressResolvedProjection, error) {
	return state.OpaqueIngressResolvedProjection{}, nil
}

type recordingProjectionStore struct {
	requests chan state.OpaqueIngressResolutionRequest
}

func (s recordingProjectionStore) ResolveOpaqueIngressProjection(
	_ context.Context,
	request state.OpaqueIngressResolutionRequest,
) (state.OpaqueIngressResolvedProjection, error) {
	s.requests <- request
	// Deliberately unavailable after capturing the validated request. This
	// distinguishes reaching the resolver from a transport or timeout fault.
	return state.OpaqueIngressResolvedProjection{}, nil
}

type stubAdmission struct{}

func (stubAdmission) CreateRun(context.Context, execution.CreateRunRequest) (execution.Admission, error) {
	return execution.Admission{}, nil
}

func (stubAdmission) GetRunForPrincipal(context.Context, execution.Principal, string, string) (state.Run, error) {
	return state.Run{}, nil
}

func testOpaqueIngressFlags(t *testing.T, args ...string) opaqueIngressFlags {
	t.Helper()
	for _, name := range []string{
		"WINDFORCE_CORE_OPAQUE_INGRESS_ADDR",
		"WINDFORCE_CORE_OPAQUE_INGRESS_TRANSPORT",
		"WINDFORCE_CORE_OPAQUE_INGRESS_MAX_REQUEST_BYTES",
		"WINDFORCE_CORE_OPAQUE_INGRESS_MAX_RESPONSE_BYTES",
		"WINDFORCE_CORE_OPAQUE_INGRESS_MAX_WAIT",
		"WINDFORCE_CORE_OPAQUE_INGRESS_POLL_INTERVAL",
		"WINDFORCE_CORE_OPAQUE_INGRESS_MAX_CONCURRENT",
		"WINDFORCE_CORE_OPAQUE_INGRESS_ACQUIRE_WAIT",
		"WINDFORCE_CORE_EXECUTION_ATTESTATION_KEY_FILE",
		"WINDFORCE_CORE_EXECUTION_ATTESTATION_KEY_ID",
		"WINDFORCE_CORE_EXECUTION_ATTESTATION_AUDIENCE",
	} {
		t.Setenv(name, "")
	}
	set := flag.NewFlagSet("opaque-ingress-test", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	parsed := bindOpaqueIngressFlags(set, "opaque-ingress-")
	if err := set.Parse(args); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	return parsed
}

func writeTestSigningKey(t *testing.T) string {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	path := filepath.Join(t.TempDir(), "execution-attestation.pem")
	block := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
	if err := os.WriteFile(path, block, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return path
}

func TestOpaqueIngressStaysUnmountedWithoutAnAddress(t *testing.T) {
	// Settings for an unmounted listener are ignored, including a transport
	// another installed version might not understand yet.
	flags := testOpaqueIngressFlags(t, "-opaque-ingress-transport", "unknown", "-opaque-ingress-max-wait", "0s")
	if flags.enabled() {
		t.Fatal("the ingress is mounted without an address")
	}
	ingress, err := startOpaqueIngress(flags, "server", stubProjectionStore{}, stubAdmission{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if ingress != nil {
		t.Fatal("an unconfigured ingress produced a server")
	}
	if ingress.wait() != nil {
		t.Fatal("an unmounted ingress reports a serve channel")
	}
	if err := ingress.stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestOpaqueIngressFailsClosedOnUnusableConfiguration(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "unbindable address", args: []string{"-opaque-ingress-addr", "127.0.0.1:-1"}},
		{name: "concurrency above the bound", args: []string{"-opaque-ingress-addr", "127.0.0.1:0", "-opaque-ingress-max-concurrent", "100000"}},
		{name: "acquire wait above the bound", args: []string{"-opaque-ingress-addr", "127.0.0.1:0", "-opaque-ingress-acquire-wait", "10s"}},
		{name: "request bytes above the wire limit", args: []string{"-opaque-ingress-addr", "127.0.0.1:0", "-opaque-ingress-max-request-bytes", "0"}},
		{name: "unknown transport", args: []string{"-opaque-ingress-addr", "127.0.0.1:0", "-opaque-ingress-transport", "unknown"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			flags := testOpaqueIngressFlags(t, testCase.args...)
			ingress, err := startOpaqueIngress(flags, "server", stubProjectionStore{}, stubAdmission{})
			if err == nil {
				_ = ingress.stop()
				t.Fatal("expected the ingress to fail closed")
			}
			if ingress != nil {
				t.Fatal("a failed start produced a server")
			}
		})
	}
}

func TestOpaqueIngressTransportConfiguration(t *testing.T) {
	t.Run("default preserves envelope", func(t *testing.T) {
		flags := testOpaqueIngressFlags(t)
		if got := *flags.transport; got != string(opaquehttp.TransportEnvelopeV1) {
			t.Fatalf("transport %q, want envelope-v1", got)
		}
	})
	t.Run("raw flag", func(t *testing.T) {
		flags := testOpaqueIngressFlags(t, "-opaque-ingress-transport", "raw-body-v1")
		if got := *flags.transport; got != string(opaquehttp.TransportRawBodyV1) {
			t.Fatalf("transport %q, want raw-body-v1", got)
		}
	})
	t.Run("environment and flag override", func(t *testing.T) {
		_ = testOpaqueIngressFlags(t) // Clear unrelated deployment settings.
		t.Setenv("WINDFORCE_CORE_OPAQUE_INGRESS_TRANSPORT", "raw-body-v1")
		set := flag.NewFlagSet("opaque-ingress-environment-test", flag.ContinueOnError)
		set.SetOutput(io.Discard)
		flags := bindOpaqueIngressFlags(set, "opaque-ingress-")
		if got := *flags.transport; got != string(opaquehttp.TransportRawBodyV1) {
			t.Fatalf("environment transport %q, want raw-body-v1", got)
		}
		if err := set.Parse([]string{"-opaque-ingress-transport", "envelope-v1"}); err != nil {
			t.Fatal(err)
		}
		if got := *flags.transport; got != string(opaquehttp.TransportEnvelopeV1) {
			t.Fatalf("flag override transport %q, want envelope-v1", got)
		}
	})
}

func rawOpaqueIngressContext(t *testing.T, lifetime time.Duration) string {
	t.Helper()
	now := time.Now().UTC()
	value := map[string]any{
		"kind": "windforce.opaque-http-ingress-context/v1",
		"trustedIngress": map[string]any{
			"issuer": "synthetic-private-gateway", "audience": "windforce-opaque-http-ingress",
			"publicationRef": "synthetic-byte-roundtrip", "routeGeneration": 7,
			"credentialRef": map[string]string{"id": "credential/synthetic", "revision": "sha256:" + strings.Repeat("a", 64)},
			"deliveryId":    "synthetic-delivery-0001",
		},
		"http": map[string]string{
			"method": "PATCH", "exactEscapedPath": "/synthetic/bytes", "contentType": "application/octet-stream",
		},
		"receivedAt": now.Format(time.RFC3339Nano), "deadlineAt": now.Add(lifetime).Format(time.RFC3339Nano),
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestOpaqueIngressRawTransportReachesResolverThroughTelemetry(t *testing.T) {
	flags := testOpaqueIngressFlags(t, "-opaque-ingress-addr", "127.0.0.1:0", "-opaque-ingress-transport", "raw-body-v1")
	store := recordingProjectionStore{requests: make(chan state.OpaqueIngressResolutionRequest, 1)}
	ingress, err := startOpaqueIngress(flags, "server", store, stubAdmission{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = ingress.stop() })
	if ingress.server.ReadHeaderTimeout != 5*time.Second || ingress.server.ReadTimeout != *flags.maxWait || ingress.server.MaxHeaderBytes != 32<<10 {
		t.Fatalf("unbounded server read configuration: %+v", ingress.server)
	}
	if ingress.server.WriteTimeout != 0 {
		t.Fatal("the listener must not truncate a terminal response with a write timeout")
	}
	body := []byte{0, 1, 0xff, '\r', '\n'}
	request, err := http.NewRequest(http.MethodPost, "http://"+ingress.addr+opaquehttp.IngressPath, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("X-Windforce-Opaque-Context", rawOpaqueIngressContext(t, 5*time.Second))
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("raw transport status %d, want resolver's 503", response.StatusCode)
	}
	select {
	case captured := <-store.requests:
		if captured.Issuer != "synthetic-private-gateway" || captured.Method != http.MethodPatch || captured.ExactEscapedPath != "/synthetic/bytes" || captured.BodyByteLength != int64(len(body)) {
			t.Fatalf("resolved request %+v", captured)
		}
	default:
		t.Fatal("raw delivery did not reach the projection resolver")
	}

	unknown, err := client.Post("http://"+ingress.addr+"/synthetic/bytes", "application/octet-stream", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer unknown.Body.Close()
	if unknown.StatusCode != http.StatusNotFound {
		t.Fatalf("original path status %d, want 404", unknown.StatusCode)
	}
}

func TestOpaqueIngressRawTransportBoundsSlowBodyReads(t *testing.T) {
	flags := testOpaqueIngressFlags(t, "-opaque-ingress-addr", "127.0.0.1:0", "-opaque-ingress-transport", "raw-body-v1")
	store := recordingProjectionStore{requests: make(chan state.OpaqueIngressResolutionRequest, 1)}
	ingress, err := startOpaqueIngress(flags, "server", store, stubAdmission{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ingress.stop() })
	connection, err := net.DialTimeout("tcp", ingress.addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	// The final byte never arrives. The metadata deadline, not the 30-second
	// server bound or the client's timeout, must end this body read.
	_, err = fmt.Fprintf(connection, "POST %s HTTP/1.1\r\nHost: %s\r\nContent-Type: application/octet-stream\r\nX-Windforce-Opaque-Context: %s\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{", opaquehttp.IngressPath, ingress.addr, rawOpaqueIngressContext(t, 200*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	if err != nil {
		t.Fatalf("read bounded response: %v", err)
	}
	defer response.Body.Close()
	var outcome opaquehttp.ExecutionOutcomeV1
	if err := json.NewDecoder(response.Body).Decode(&outcome); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusGatewayTimeout || outcome.Failure == nil || outcome.Failure.Category != opaquehttp.FailureDeadlineExceeded {
		t.Fatalf("slow body status %d outcome %+v, want 504 deadlineExceeded", response.StatusCode, outcome)
	}
	select {
	case <-store.requests:
		t.Fatal("an incomplete raw body reached the projection resolver")
	default:
	}
}

func TestOpaqueIngressRawTransportRejectsTrailersThroughTelemetry(t *testing.T) {
	for _, declared := range []bool{false, true} {
		name := "undeclared"
		if declared {
			name = "declared"
		}
		t.Run(name, func(t *testing.T) {
			flags := testOpaqueIngressFlags(t, "-opaque-ingress-addr", "127.0.0.1:0", "-opaque-ingress-transport", "raw-body-v1")
			store := recordingProjectionStore{requests: make(chan state.OpaqueIngressResolutionRequest, 1)}
			ingress, err := startOpaqueIngress(flags, "server", store, stubAdmission{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = ingress.stop() })
			connection, err := net.DialTimeout("tcp", ingress.addr, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
				t.Fatal(err)
			}
			declaration := ""
			if declared {
				declaration = "Trailer: X-Untrusted-Trailer\r\n"
			}
			// Real net/http populates trailers on its original request only once
			// Body reaches EOF. A telemetry request clone must not hide them from
			// the adapter, including when there was no Trailer declaration.
			_, err = fmt.Fprintf(connection, "POST %s HTTP/1.1\r\nHost: %s\r\nContent-Type: application/octet-stream\r\nX-Windforce-Opaque-Context: %s\r\nTransfer-Encoding: chunked\r\n%sConnection: close\r\n\r\n2\r\n{}\r\n0\r\nX-Untrusted-Trailer: forged\r\n\r\n", opaquehttp.IngressPath, ingress.addr, rawOpaqueIngressContext(t, 5*time.Second), declaration)
			if err != nil {
				t.Fatal(err)
			}
			response, err := http.ReadResponse(bufio.NewReader(connection), nil)
			if err != nil {
				t.Fatalf("read trailer rejection: %v", err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("trailer status %d, want 400", response.StatusCode)
			}
			select {
			case <-store.requests:
				t.Fatal("a raw delivery with trailers reached the projection resolver")
			default:
			}
		})
	}
}

func TestOpaqueIngressServesOnlyItsOwnSurface(t *testing.T) {
	flags := testOpaqueIngressFlags(t, "-opaque-ingress-addr", "127.0.0.1:0")
	ingress, err := startOpaqueIngress(flags, "server", stubProjectionStore{}, stubAdmission{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = ingress.stop() })

	addr := ingress.addr

	response, err := http.Get("http://" + addr + opaquehttp.ReadinessPath)
	if err != nil {
		t.Fatalf("readiness request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("readiness status %d, want 200", response.StatusCode)
	}
	var readiness map[string]bool
	if err := json.NewDecoder(response.Body).Decode(&readiness); err != nil {
		t.Fatalf("decode readiness: %v", err)
	}
	if !readiness["ready"] {
		t.Fatalf("readiness body %+v", readiness)
	}

	unknown, err := http.Post("http://"+addr+"/api/v1/runs", "application/json", nil)
	if err != nil {
		t.Fatalf("unknown path request: %v", err)
	}
	defer unknown.Body.Close()
	if unknown.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown path status %d, want 404", unknown.StatusCode)
	}

	if err := ingress.stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, err := http.Get("http://" + addr + opaquehttp.ReadinessPath); err == nil {
		t.Fatal("the listener still answers after shutdown")
	}
}

func TestPrimaryListenerDoesNotServeTheIngressPath(t *testing.T) {
	handler := server.New(server.Config{})

	// A route the primary listener does own, so a 404 below means the ingress
	// path is absent from its table rather than the whole handler refusing.
	control := httptest.NewRecorder()
	handler.ServeHTTP(control, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if control.Code == http.StatusNotFound {
		t.Fatalf("the primary listener 404s its own health path; this test cannot discriminate")
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, opaquehttp.IngressPath, nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("primary listener answered the ingress path with %d, want 404", recorder.Code)
	}

	readiness := httptest.NewRecorder()
	handler.ServeHTTP(readiness, httptest.NewRequest(http.MethodGet, opaquehttp.IngressPath, nil))
	if readiness.Code != http.StatusNotFound {
		t.Fatalf("primary listener answered a GET on the ingress path with %d, want 404", readiness.Code)
	}
}

func TestExecutionAttestationIssuerRequiresACompleteConfiguration(t *testing.T) {
	keyFile := writeTestSigningKey(t)

	unset := testOpaqueIngressFlags(t)
	issuer, err := unset.executionAttestationIssuer()
	if err != nil {
		t.Fatalf("unconfigured issuer: %v", err)
	}
	if issuer != nil {
		t.Fatal("an unconfigured deployment built an issuer")
	}

	partial := testOpaqueIngressFlags(t, "-opaque-ingress-attestation-key-file", keyFile)
	if _, err := partial.executionAttestationIssuer(); err == nil {
		t.Fatal("a partial attestation configuration was accepted")
	}

	complete := testOpaqueIngressFlags(t,
		"-opaque-ingress-attestation-key-file", keyFile,
		"-opaque-ingress-attestation-key-id", "core-execution-1",
		"-opaque-ingress-attestation-audience", "capability.internal",
	)
	issuer, err = complete.executionAttestationIssuer()
	if err != nil {
		t.Fatalf("complete issuer: %v", err)
	}
	if issuer == nil {
		t.Fatal("a complete configuration built no issuer")
	}
}
