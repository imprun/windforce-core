package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
	flags := testOpaqueIngressFlags(t)
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
