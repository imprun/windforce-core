package controlcli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunWFHostedDeviceLoginAndRefresh(t *testing.T) {
	var server *httptest.Server
	var appRequests atomic.Int32
	var refreshRequests atomic.Int32
	var revocationRequests atomic.Int32
	var openedURL string
	var formMu sync.Mutex
	seenForms := make(map[string]url.Values)

	handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case cliMetadataPath:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"schema_version": 1,
				"authentication": map[string]any{
					"type":      authTypeOAuth2Device,
					"issuer":    server.URL,
					"client_id": "wf-cli",
					"audience":  "windforce-api",
					"scopes":    []string{"openid", "profile", "email", "offline_access"},
				},
			})
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                        server.URL,
				"device_authorization_endpoint": server.URL + "/oauth2/device/auth",
				"token_endpoint":                server.URL + "/oauth2/token",
				"revocation_endpoint":           server.URL + "/oauth2/revoke",
			})
		case "/oauth2/device/auth":
			if err := request.ParseForm(); err != nil {
				t.Errorf("parse device form: %v", err)
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			formMu.Lock()
			seenForms["device"] = request.Form
			formMu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":               "server-only-device-code",
				"user_code":                 "ABCD-EFGH",
				"verification_uri":          server.URL + "/oauth/device",
				"verification_uri_complete": server.URL + "/oauth/device?user_code=ABCD-EFGH",
				"expires_in":                600,
				"interval":                  1,
			})
		case "/oauth2/token":
			if err := request.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			grantType := request.Form.Get("grant_type")
			formMu.Lock()
			seenForms[grantType] = request.Form
			formMu.Unlock()
			switch grantType {
			case "urn:ietf:params:oauth:grant-type:device_code":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token":  "hosted-access-token",
					"refresh_token": "hosted-refresh-token",
					"token_type":    "Bearer",
					"expires_in":    3600,
				})
			case "refresh_token":
				refreshRequests.Add(1)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token":  "refreshed-access-token",
					"refresh_token": "rotated-refresh-token",
					"token_type":    "Bearer",
					"expires_in":    3600,
				})
			default:
				http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
			}
		case "/oauth2/revoke":
			if err := request.ParseForm(); err != nil {
				t.Errorf("parse revocation form: %v", err)
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			revocationRequests.Add(1)
			formMu.Lock()
			seenForms["revocation"] = request.Form
			formMu.Unlock()
			w.WriteHeader(http.StatusOK)
		case "/api/w/team/apps":
			appRequests.Add(1)
			authorization := request.Header.Get("Authorization")
			if authorization != "Bearer hosted-access-token" && authorization != "Bearer refreshed-access-token" {
				t.Errorf("unexpected application authorization %q", authorization)
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"apps":[]}`))
		default:
			http.NotFound(w, request)
		}
	})
	server = httptest.NewServer(handler)
	defer server.Close()

	configPath := t.TempDir() + "/config.json"
	t.Setenv("WF_CONFIG", configPath)
	var stdout, stderr bytes.Buffer
	exit := RunWF(
		[]string{"context", "set", "hosted", "--api-url", server.URL, "--workspace", "team", "--use"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitOK {
		t.Fatalf("set context exit=%d stderr=%s", exit, stderr.String())
	}

	store := &memoryCredentialStore{values: map[string]string{}}
	stdout.Reset()
	stderr.Reset()
	exit = runWithProgramDependencies(
		wfProgram,
		[]string{"auth", "login"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		store,
		func(rawURL string) error {
			openedURL = rawURL
			return nil
		},
	)
	if exit != ExitOK {
		t.Fatalf("hosted login exit=%d stderr=%s", exit, stderr.String())
	}
	if !strings.Contains(stderr.String(), "ABCD-EFGH") || !strings.Contains(stderr.String(), server.URL+"/oauth/device") {
		t.Fatalf("device instructions = %q", stderr.String())
	}
	if openedURL != server.URL+"/oauth/device?user_code=ABCD-EFGH" ||
		!strings.Contains(stderr.String(), "Opening ") {
		t.Fatalf("browser URL=%q instructions=%q", openedURL, stderr.String())
	}
	for _, secret := range []string{"server-only-device-code", "hosted-access-token", "hosted-refresh-token"} {
		if strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
			t.Fatalf("OAuth secret %q leaked to command output", secret)
		}
	}
	if len(store.values) != 1 {
		t.Fatalf("stored credentials = %#v", store.values)
	}

	var key, encoded string
	for key, encoded = range store.values {
	}
	credential, hosted, err := decodeStoredCredential(encoded)
	if err != nil || !hosted {
		t.Fatalf("decode stored credential hosted=%v err=%v", hosted, err)
	}
	if credential.AccessToken != "hosted-access-token" || credential.RefreshToken != "hosted-refresh-token" {
		t.Fatalf("stored credential = %#v", credential)
	}
	if credential.RevocationURL != server.URL+"/oauth2/revoke" {
		t.Fatalf("stored revocation endpoint = %q", credential.RevocationURL)
	}
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"server-only-device-code", "hosted-access-token", "hosted-refresh-token"} {
		if bytes.Contains(configBytes, []byte(secret)) {
			t.Fatalf("configuration contains OAuth secret %q", secret)
		}
	}
	config, err := loadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	profile := config.Profiles["hosted"]
	if profile.Account != "identity" || profile.AuthType != authTypeOAuth2Device {
		t.Fatalf("hosted profile = %#v", profile)
	}

	formMu.Lock()
	deviceForm := seenForms["device"]
	tokenForm := seenForms["urn:ietf:params:oauth:grant-type:device_code"]
	formMu.Unlock()
	if deviceForm.Get("client_id") != "wf-cli" ||
		deviceForm.Get("audience") != "windforce-api" ||
		deviceForm.Get("scope") != "openid profile email offline_access" {
		t.Fatalf("device authorization form = %#v", deviceForm)
	}
	if tokenForm.Get("client_id") != "wf-cli" ||
		tokenForm.Get("device_code") != "server-only-device-code" ||
		tokenForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
		t.Fatalf("device token form = %#v", tokenForm)
	}

	credential.ExpiresAt = time.Now().Add(-time.Minute)
	credential.AccessToken = "expired-access-token"
	encoded, err = encodeStoredCredential(credential)
	if err != nil {
		t.Fatal(err)
	}
	store.values[key] = encoded

	stdout.Reset()
	stderr.Reset()
	exit = RunWFWithCredentialStore(
		[]string{"app", "list"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		store,
	)
	if exit != ExitOK {
		t.Fatalf("app list after refresh exit=%d stderr=%s", exit, stderr.String())
	}
	if refreshRequests.Load() != 1 {
		t.Fatalf("refresh requests = %d, want 1", refreshRequests.Load())
	}
	if appRequests.Load() != 2 {
		t.Fatalf("application requests = %d, want login probe plus app list", appRequests.Load())
	}
	refreshed, hosted, err := decodeStoredCredential(store.values[key])
	if err != nil || !hosted {
		t.Fatalf("decode refreshed credential hosted=%v err=%v", hosted, err)
	}
	if refreshed.AccessToken != "refreshed-access-token" || refreshed.RefreshToken != "rotated-refresh-token" {
		t.Fatalf("refreshed credential = %#v", refreshed)
	}
	if refreshed.RevocationURL != server.URL+"/oauth2/revoke" {
		t.Fatalf("refreshed revocation endpoint = %q", refreshed.RevocationURL)
	}
	formMu.Lock()
	refreshForm := seenForms["refresh_token"]
	formMu.Unlock()
	if refreshForm.Get("client_id") != "wf-cli" ||
		refreshForm.Get("refresh_token") != "hosted-refresh-token" ||
		refreshForm.Get("grant_type") != "refresh_token" {
		t.Fatalf("refresh form = %#v", refreshForm)
	}

	stdout.Reset()
	stderr.Reset()
	exit = RunWFWithCredentialStore(
		[]string{"auth", "status"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		store,
	)
	if exit != ExitOK || !strings.Contains(stdout.String(), `"auth_type":"oauth2-device"`) {
		t.Fatalf("auth status exit=%d stdout=%s stderr=%s", exit, stdout.String(), stderr.String())
	}

	// Credentials written before revocation metadata was added remain usable:
	// logout re-discovers the endpoint from the stored issuer.
	refreshed.RevocationURL = ""
	encoded, err = encodeStoredCredential(refreshed)
	if err != nil {
		t.Fatal(err)
	}
	store.values[key] = encoded

	stdout.Reset()
	stderr.Reset()
	exit = RunWFWithCredentialStore(
		[]string{"auth", "logout"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		store,
	)
	if exit != ExitOK ||
		!strings.Contains(stdout.String(), `"remote_revoked":true`) ||
		!strings.Contains(stdout.String(), `"credential_removed":true`) {
		t.Fatalf("auth logout exit=%d stdout=%s stderr=%s", exit, stdout.String(), stderr.String())
	}
	if revocationRequests.Load() != 1 || len(store.values) != 0 {
		t.Fatalf("revocation requests=%d credentials=%#v", revocationRequests.Load(), store.values)
	}
	formMu.Lock()
	revocationForm := seenForms["revocation"]
	formMu.Unlock()
	if revocationForm.Get("client_id") != "wf-cli" ||
		revocationForm.Get("token") != "rotated-refresh-token" ||
		revocationForm.Get("token_type_hint") != "refresh_token" {
		t.Fatalf("revocation form = %#v", revocationForm)
	}
	for _, secret := range []string{"refreshed-access-token", "rotated-refresh-token"} {
		if strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
			t.Fatalf("OAuth secret %q leaked during logout", secret)
		}
	}
	config, err = loadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if config.Profiles["hosted"].Account != "" || config.Profiles["hosted"].AuthType != "" {
		t.Fatalf("logout profile = %#v", config.Profiles["hosted"])
	}

	var browserCalled bool
	stdout.Reset()
	stderr.Reset()
	exit = runWithProgramDependencies(
		wfProgram,
		[]string{"auth", "login", "--no-browser", "--account", "headless"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		store,
		func(string) error {
			browserCalled = true
			return nil
		},
	)
	if exit != ExitOK ||
		browserCalled ||
		!strings.Contains(stderr.String(), "Open this URL to continue:") {
		t.Fatalf(
			"headless login exit=%d browserCalled=%v stdout=%q stderr=%q",
			exit,
			browserCalled,
			stdout.String(),
			stderr.String(),
		)
	}
	stdout.Reset()
	stderr.Reset()
	exit = RunWFWithCredentialStore(
		[]string{"auth", "logout", "--local-only"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		store,
	)
	if exit != ExitOK || len(store.values) != 0 {
		t.Fatalf("headless cleanup exit=%d credentials=%#v stderr=%q", exit, store.values, stderr.String())
	}
}

func TestRunWFHostedLogoutPreservesCredentialWhenRemoteRevocationFails(t *testing.T) {
	var revocationRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/oauth2/revoke" {
			http.NotFound(w, request)
			return
		}
		revocationRequests.Add(1)
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	configPath := t.TempDir() + "/config.json"
	t.Setenv("WF_CONFIG", configPath)
	profile := Profile{
		APIURL:    server.URL,
		Workspace: "team",
		Account:   "identity",
		AuthType:  authTypeOAuth2Device,
	}
	config := ConfigFile{
		CurrentProfile: "hosted",
		Profiles:       map[string]Profile{"hosted": profile},
	}
	if err := saveConfig(configPath, config); err != nil {
		t.Fatal(err)
	}
	credential := storedCredential{
		Version:       1,
		Kind:          authTypeOAuth2Device,
		AccessToken:   "hosted-access-token",
		RefreshToken:  "hosted-refresh-token",
		TokenType:     "Bearer",
		ExpiresAt:     time.Now().Add(time.Hour),
		TokenURL:      server.URL + "/oauth2/token",
		RevocationURL: server.URL + "/oauth2/revoke",
		ClientID:      "wf-cli",
		Issuer:        server.URL,
	}
	encoded, err := encodeStoredCredential(credential)
	if err != nil {
		t.Fatal(err)
	}
	key, err := credentialKey(profile)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryCredentialStore{values: map[string]string{key: encoded}}

	var stdout, stderr bytes.Buffer
	exit := RunWFWithCredentialStore(
		[]string{"auth", "logout"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		store,
	)
	if exit == ExitOK ||
		!strings.Contains(stderr.String(), "HTTP 503") ||
		!strings.Contains(stderr.String(), "no local credential was removed") {
		t.Fatalf("logout exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	if store.values[key] != encoded {
		t.Fatalf("credential changed after failed revocation: %#v", store.values)
	}
	loaded, err := loadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Profiles["hosted"].Account != "identity" {
		t.Fatalf("profile changed after failed revocation: %#v", loaded.Profiles["hosted"])
	}
	for _, secret := range []string{"hosted-access-token", "hosted-refresh-token"} {
		if strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
			t.Fatalf("OAuth secret %q leaked after revocation failure", secret)
		}
	}

	stdout.Reset()
	stderr.Reset()
	exit = RunWFWithCredentialStore(
		[]string{"auth", "logout", "--local-only"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		store,
	)
	if exit != ExitOK ||
		!strings.Contains(stdout.String(), `"remote_revoked":false`) ||
		!strings.Contains(stdout.String(), `"credential_removed":true`) {
		t.Fatalf("local logout exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	if revocationRequests.Load() != 1 || len(store.values) != 0 {
		t.Fatalf("revocation requests=%d credentials=%#v", revocationRequests.Load(), store.values)
	}
	loaded, err = loadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Profiles["hosted"].Account != "" || loaded.Profiles["hosted"].AuthType != "" {
		t.Fatalf("local logout profile = %#v", loaded.Profiles["hosted"])
	}
}

func TestRunWFHostedLoginRejectsSecretBearingMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"schema_version": 1,
			"authentication": {
				"type": "oauth2-device",
				"issuer": "https://identity.example.test",
				"client_id": "wf-cli",
				"client_secret": "must-never-be-public",
				"audience": "windforce-api",
				"scopes": ["openid", "offline_access"]
			}
		}`))
	}))
	defer server.Close()

	configPath := t.TempDir() + "/config.json"
	t.Setenv("WF_CONFIG", configPath)
	var stdout, stderr bytes.Buffer
	exit := RunWF(
		[]string{"context", "set", "hosted", "--api-url", server.URL, "--use"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitOK {
		t.Fatalf("set context exit=%d stderr=%s", exit, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	exit = RunWFWithCredentialStore(
		[]string{"auth", "login", "--no-browser"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		&memoryCredentialStore{values: map[string]string{}},
	)
	if exit == ExitOK || !strings.Contains(stderr.String(), "must not contain a client secret") {
		t.Fatalf("login exit=%d stderr=%s", exit, stderr.String())
	}
	if strings.Contains(stdout.String(), "must-never-be-public") || strings.Contains(stderr.String(), "must-never-be-public") {
		t.Fatal("client secret leaked to command output")
	}
}

func TestSafeWebURLRequiresTLSOutsideLoopback(t *testing.T) {
	for _, rawURL := range []string{
		"http://identity.example.test",
		"https://user:pass@identity.example.test",
		"https://identity.example.test?redirect=elsewhere",
		"https://identity.example.test/#fragment",
	} {
		if _, err := safeWebURL(rawURL); err == nil {
			t.Fatalf("safeWebURL(%q) unexpectedly succeeded", rawURL)
		}
	}
	for _, rawURL := range []string{
		"http://127.0.0.1:4444",
		"http://[::1]:4444",
		"http://localhost:4444",
		"https://identity.example.test",
	} {
		if _, err := safeWebURL(rawURL); err != nil {
			t.Fatalf("safeWebURL(%q) = %v", rawURL, err)
		}
	}
}
