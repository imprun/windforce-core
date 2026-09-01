package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imprun/windforce-core/internal/contract"
	executionpkg "github.com/imprun/windforce-core/internal/execution"
	"github.com/imprun/windforce-core/internal/state"
)

func TestCanonicalServicePrincipalLifecycle(t *testing.T) {
	server := httptest.NewServer(New(Config{
		Store: state.NewLocalStore(filepath.Join(t.TempDir(), "state.json")),
	}))
	defer server.Close()

	do := func(method string, path string, actor string, body string, wantStatus int, target any) []byte {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if actor != "" {
			req.Header.Set("X-Windforce-Actor", actor)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var payload bytes.Buffer
		if _, err := payload.ReadFrom(resp.Body); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != wantStatus {
			t.Fatalf("%s %s status = %d, want %d: %s", method, path, resp.StatusCode, wantStatus, payload.String())
		}
		if target != nil && payload.Len() > 0 {
			if err := json.Unmarshal(payload.Bytes(), target); err != nil {
				t.Fatalf("decode %s %s: %v: %s", method, path, err, payload.String())
			}
		}
		return payload.Bytes()
	}

	var issued struct {
		ServicePrincipal servicePrincipalView `json:"service_principal"`
		APIToken         string               `json:"api_token"`
	}
	body := `{"name":"Order intake","scopes":["runs:read:own","runs:create","runs:create"],"allowed_targets":["echo/run","echo/run"]}`
	createdBody := do(http.MethodPost, "/api/w/ws-a/service-principals", "alice@example.test", body, http.StatusCreated, &issued)
	created := issued.ServicePrincipal
	if created.ID == "" || created.WorkspaceID != "ws-a" || created.Name != "Order intake" ||
		!created.HasToken || !strings.HasPrefix(issued.APIToken, contract.ServiceTokenPrefix) {
		t.Fatalf("created = %#v", created)
	}
	if len(created.Scopes) != 2 || created.Scopes[0] != string(executionpkg.ScopeRunsCreate) || created.Scopes[1] != string(executionpkg.ScopeRunsReadOwn) {
		t.Fatalf("scopes = %#v", created.Scopes)
	}
	if len(created.AllowedTargets) != 1 || created.AllowedTargets[0] != "echo/run" {
		t.Fatalf("allowed targets = %#v", created.AllowedTargets)
	}
	if bytes.Contains(createdBody, []byte("token_hash")) {
		t.Fatalf("created response exposes a token hash: %s", createdBody)
	}

	do(http.MethodPost, "/api/w/ws-a/service-principals", "", `{"name":"bad","scopes":["root"]}`, http.StatusBadRequest, nil)
	do(http.MethodPost, "/api/w/ws-a/service-principals", "", `{"name":"bad","scopes":[],"allowed_targets":["echo/*"]}`, http.StatusBadRequest, nil)

	path := "/api/w/ws-a/service-principals/" + created.ID
	var updated servicePrincipalView
	do(http.MethodPatch, path, "bob@example.test", `{"scopes":["apps:read"],"allowed_targets":[]}`, http.StatusOK, &updated)
	if len(updated.Scopes) != 1 || updated.Scopes[0] != string(executionpkg.ScopeAppsRead) || len(updated.AllowedTargets) != 0 {
		t.Fatalf("updated = %#v", updated)
	}

	var rotated struct {
		ServicePrincipal servicePrincipalView `json:"service_principal"`
		APIToken         string               `json:"api_token"`
	}
	do(http.MethodPost, path+"/token", "bob@example.test", "", http.StatusOK, &rotated)
	if rotated.APIToken == issued.APIToken || !strings.HasPrefix(rotated.APIToken, contract.ServiceTokenPrefix) {
		t.Fatal("rotated token was not replaced")
	}

	var audit []state.ServicePrincipalAudit
	auditBody := do(http.MethodGet, path+"/audit", "", "", http.StatusOK, &audit)
	if len(audit) != 3 || audit[0].Kind != "token_rotated" || audit[1].Kind != "updated" || audit[2].Kind != "created" {
		t.Fatalf("audit = %#v", audit)
	}
	if bytes.Contains(auditBody, []byte(issued.APIToken)) || bytes.Contains(auditBody, []byte(rotated.APIToken)) {
		t.Fatalf("audit exposes a service principal token: %s", auditBody)
	}

	do(http.MethodDelete, path, "carol@example.test", "", http.StatusConflict, nil)
	do(http.MethodDelete, path+"/token", "carol@example.test", "", http.StatusOK, &updated)
	if updated.HasToken {
		t.Fatalf("revoked service principal still has a token: %#v", updated)
	}
	do(http.MethodDelete, path, "carol@example.test", "", http.StatusNoContent, nil)
	do(http.MethodGet, path, "", "", http.StatusNotFound, nil)
}

func TestControlPlaneOpenAPIIncludesServicePrincipals(t *testing.T) {
	document := buildControlPlaneOpenAPI("http://example.test", "default")
	paths := document["paths"].(map[string]any)
	for _, path := range []string{
		"/api/w/{workspace}/service-principals",
		"/api/w/{workspace}/service-principals/{service_principal_id}",
		"/api/w/{workspace}/service-principals/{service_principal_id}/token",
		"/api/w/{workspace}/service-principals/{service_principal_id}/audit",
	} {
		if paths[path] == nil {
			t.Errorf("OpenAPI path %q is missing", path)
		}
	}
	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	for _, name := range []string{
		"ServicePrincipal",
		"CreateServicePrincipalRequest",
		"UpdateServicePrincipalRequest",
		"ServicePrincipalTokenResult",
		"ServicePrincipalAudit",
	} {
		if schemas[name] == nil {
			t.Errorf("OpenAPI schema %q is missing", name)
		}
	}
	createRequest := schemas["CreateServicePrincipalRequest"].(map[string]any)
	properties := createRequest["properties"].(map[string]any)
	scopeItems := properties["scopes"].(map[string]any)["items"].(map[string]any)
	scopeValues := scopeItems["enum"].([]any)
	wantedScopes := map[string]bool{
		string(executionpkg.ScopeOpaqueHTTPProjectionsRead):  false,
		string(executionpkg.ScopeOpaqueHTTPProjectionsWrite): false,
	}
	for _, value := range scopeValues {
		if scope, ok := value.(string); ok {
			if _, wanted := wantedScopes[scope]; wanted {
				wantedScopes[scope] = true
			}
		}
	}
	for scope, found := range wantedScopes {
		if !found {
			t.Errorf("OpenAPI service principal scope %q is missing", scope)
		}
	}
}
