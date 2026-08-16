package worker

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/secretmask"
)

func TestRuntimeConfigProxyRegistersSecretBeforeAuthenticatedForward(t *testing.T) {
	const coreToken = "core-job-token"
	const secret = "new-runtime-secret"
	var gotAuthorization, gotAttestation string
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		gotAttestation = r.Header.Get(secretMaskRegistrationHeader)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer core.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	registry := secretmask.NewRegistry(nil)
	proxyURL, localToken, closeProxy, err := startRuntimeConfigProxy(ctx, core.URL, coreToken, registry, contract.RuntimeAccess{
		WriteVariables: []contract.RuntimeVariableWriteTarget{{
			RuntimeConfigTarget: contract.RuntimeConfigTarget{Scope: contract.RuntimeConfigScopeApp, Path: "credentials/token"},
			Storage:             contract.RuntimeVariableStorageSecret,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeProxy()

	body := `{"value":"` + secret + `","operationId":"op-1"}`
	req, err := http.NewRequest(http.MethodPut, proxyURL+"/api/w/ws/variables/p/credentials/token", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+localToken)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if gotAuthorization != "Bearer "+coreToken {
		t.Fatalf("Core authorization = %q", gotAuthorization)
	}
	if gotAttestation != secretMaskAttestation(coreToken, []byte(body)) {
		t.Fatal("Core did not receive the body-bound mask attestation")
	}
	if got := registry.String("before " + secret + " after"); strings.Contains(got, secret) {
		t.Fatalf("dynamic Secret was not masked: %q", got)
	}
}

func TestRuntimeConfigProxyRegistersSecretsFromTrustedCoreResponse(t *testing.T) {
	const secret = "later-job-session-secret"
	const secondSecret = "coalesced-header-secret"
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add(secretmask.ResponseDigestHeader, secretmask.Digest(secret)+", "+secretmask.Digest(secondSecret))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"label":"ready","nested":{"storageState":"`+secret+`","token":"`+secondSecret+`"}}`)
	}))
	defer core.Close()

	registry := secretmask.NewRegistry(nil)
	proxyURL, localToken, closeProxy, err := startRuntimeConfigProxy(context.Background(), core.URL, "core-job-token", registry, contract.RuntimeAccess{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeProxy()
	req, _ := http.NewRequest(http.MethodGet, proxyURL+"/api/w/ws/resources/p/sessions/meta?scope=app", nil)
	req.Header.Set("Authorization", "Bearer "+localToken)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if response.Header.Get(secretmask.ResponseDigestHeader) != "" {
		t.Fatal("internal Secret digest header escaped to the Action")
	}
	if !strings.Contains(string(body), secret) {
		t.Fatalf("Action did not receive the resolved value: %s", body)
	}
	if got := registry.String("later log " + secret); strings.Contains(got, secret) {
		t.Fatalf("Secret read from Core was not registered for masking: %q", got)
	}
	if got := registry.String("later log " + secondSecret); strings.Contains(got, secondSecret) {
		t.Fatalf("Secret from coalesced digest header was not registered for masking: %q", got)
	}
}

func TestRuntimeConfigProxyRejectsCoreTokenFromAction(t *testing.T) {
	core := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("unauthorized request reached Core")
	}))
	defer core.Close()
	proxyURL, _, closeProxy, err := startRuntimeConfigProxy(context.Background(), core.URL, "core-job-token", secretmask.NewRegistry(nil), contract.RuntimeAccess{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeProxy()
	req, _ := http.NewRequest(http.MethodGet, proxyURL+"/api/w/ws/variables/p/a", nil)
	req.Header.Set("Authorization", "Bearer core-job-token")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.StatusCode)
	}
}

func TestBearerEqualsRequiresBearerScheme(t *testing.T) {
	if bearerEquals("proxy-token", "proxy-token") {
		t.Fatal("bare proxy token was accepted without the Bearer scheme")
	}
	if !bearerEquals("bearer proxy-token", "proxy-token") {
		t.Fatal("case-insensitive Bearer scheme was rejected")
	}
}
