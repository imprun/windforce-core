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
	"github.com/imprun/windforce-core/internal/state"
)

func TestCanonicalClientLifecycle(t *testing.T) {
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
		Client   clientView `json:"client"`
		APIToken string     `json:"api_token"`
	}
	createdBody := do(http.MethodPost, "/api/w/ws-a/clients", "alice@example.test", `{"name":"Acme Operations"}`, http.StatusCreated, &issued)
	created := issued.Client
	if created.ID == "" || created.WorkspaceID != "ws-a" || created.Name != "Acme Operations" || !created.HasToken || !strings.HasPrefix(issued.APIToken, contract.ClientTokenPrefix) {
		t.Fatalf("created = %#v", created)
	}
	if created.InvocationPolicy.Mode != state.TargetPolicyModeAll || created.InvocationPolicy.Revision != 0 || len(created.InvocationPolicy.AllowedTargets) != 0 {
		t.Fatalf("default invocation policy = %#v", created.InvocationPolicy)
	}
	if created.CreatedBy != "alice@example.test" || created.UpdatedBy != "alice@example.test" {
		t.Fatalf("created actors = %#v", created)
	}
	if bytes.Contains(createdBody, []byte("token_hash")) || bytes.Contains(createdBody, []byte("external_key")) {
		t.Fatalf("created response exposes stored credential data: %s", createdBody)
	}

	do(http.MethodPost, "/api/w/ws-a/clients", "alice@example.test", `{"name":"   "}`, http.StatusBadRequest, nil)

	var clients []clientView
	do(http.MethodGet, "/api/w/ws-a/clients", "", "", http.StatusOK, &clients)
	if len(clients) != 1 || clients[0].ID != created.ID {
		t.Fatalf("clients = %#v", clients)
	}

	var updated clientView
	clientPath := "/api/w/ws-a/clients/" + created.ID
	policyPath := clientPath + "/invocation-policy"
	do(http.MethodPut, policyPath, "policy-admin@example.test", `{"operation_id":"policy-missing-revision","mode":"restricted","allowed_targets":[]}`, http.StatusBadRequest, nil)
	do(http.MethodPut, policyPath, "policy-admin@example.test", `{"operation_id":"policy-missing-targets","expected_revision":0,"mode":"restricted"}`, http.StatusBadRequest, nil)
	do(http.MethodPut, policyPath, "policy-admin@example.test", `{"operation_id":"policy-invalid","expected_revision":0,"mode":"all","allowed_targets":["echo"]}`, http.StatusBadRequest, nil)
	policyBody := `{"operation_id":"policy-1","expected_revision":0,"mode":"restricted","allowed_targets":["echo/run","echo","echo/run"]}`
	var policyResult struct {
		InvocationPolicy clientInvocationPolicyView `json:"invocation_policy"`
		Replayed         bool                       `json:"replayed"`
	}
	do(http.MethodPut, policyPath, "policy-admin@example.test", policyBody, http.StatusOK, &policyResult)
	if policyResult.Replayed || policyResult.InvocationPolicy.Mode != state.TargetPolicyModeRestricted ||
		policyResult.InvocationPolicy.Revision != 1 || strings.Join(policyResult.InvocationPolicy.AllowedTargets, ",") != "echo,echo/run" {
		t.Fatalf("updated invocation policy = %#v", policyResult)
	}
	do(http.MethodPut, policyPath, "policy-admin@example.test", policyBody, http.StatusOK, &policyResult)
	if !policyResult.Replayed || policyResult.InvocationPolicy.Revision != 1 {
		t.Fatalf("replayed invocation policy = %#v", policyResult)
	}
	do(http.MethodPut, policyPath, "policy-admin@example.test", `{"operation_id":"policy-1","expected_revision":0,"mode":"restricted","allowed_targets":["other"]}`, http.StatusConflict, nil)
	do(http.MethodPut, policyPath, "policy-admin@example.test", `{"operation_id":"policy-stale","expected_revision":0,"mode":"restricted","allowed_targets":[]}`, http.StatusConflict, nil)

	do(http.MethodPatch, clientPath, "bob@example.test", `{"name":"Acme Korea"}`, http.StatusOK, &updated)
	if updated.ID != created.ID || updated.Name != "Acme Korea" || !updated.HasToken || updated.UpdatedBy != "bob@example.test" ||
		updated.InvocationPolicy.Revision != 1 || strings.Join(updated.InvocationPolicy.AllowedTargets, ",") != "echo,echo/run" {
		t.Fatalf("updated = %#v", updated)
	}
	var rotated struct {
		Client   clientView `json:"client"`
		APIToken string     `json:"api_token"`
	}
	do(http.MethodPost, clientPath+"/token", "bob@example.test", "", http.StatusOK, &rotated)
	if rotated.APIToken == issued.APIToken || !strings.HasPrefix(rotated.APIToken, contract.ClientTokenPrefix) {
		t.Fatalf("rotated token was not replaced")
	}

	var audit []state.ClientAudit
	auditBody := do(http.MethodGet, clientPath+"/audit", "", "", http.StatusOK, &audit)
	if len(audit) != 4 || audit[0].Kind != "token_rotated" || audit[1].Kind != "updated" || audit[2].Kind != "invocation_policy_updated" || audit[3].Kind != "created" {
		t.Fatalf("audit = %#v", audit)
	}
	if bytes.Contains(auditBody, []byte(issued.APIToken)) || bytes.Contains(auditBody, []byte(rotated.APIToken)) {
		t.Fatalf("audit exposes client key: %s", auditBody)
	}

	do(http.MethodDelete, clientPath, "carol@example.test", "", http.StatusConflict, nil)
	do(http.MethodDelete, clientPath+"/token", "carol@example.test", "", http.StatusOK, &updated)
	if updated.HasToken {
		t.Fatalf("revoked client still has a token: %#v", updated)
	}
	do(http.MethodDelete, clientPath, "carol@example.test", "", http.StatusNoContent, nil)
	do(http.MethodGet, clientPath, "", "", http.StatusNotFound, nil)
	auditBody = do(http.MethodGet, clientPath+"/audit", "", "", http.StatusOK, &audit)
	if len(audit) != 6 || audit[0].Kind != "deleted" || audit[1].Kind != "token_revoked" || audit[0].Actor != "carol@example.test" {
		t.Fatalf("audit after delete = %#v", audit)
	}
	if bytes.Contains(auditBody, []byte(issued.APIToken)) || bytes.Contains(auditBody, []byte(rotated.APIToken)) {
		t.Fatalf("audit exposes deleted client key: %s", auditBody)
	}
}

func TestControlPlaneOpenAPIIncludesClients(t *testing.T) {
	schemas := controlPlaneSchemas()
	for _, name := range []string{"Client", "ClientInvocationPolicy", "UpdateClientInvocationPolicyRequest", "ClientInvocationPolicyResult", "ClientTokenResult", "CreateClientRequest", "UpdateClientRequest", "ClientAudit", "AuditChanges", "AuditEvent", "InputConfig", "SetInputConfigRequest", "InputConfigAudit", "ProvisioningInvocationPolicy", "ProvisioningResource", "ProvisioningImportRequest", "ProvisioningResult"} {
		if schemas[name] == nil {
			t.Fatalf("missing schema %s", name)
		}
	}
	provisioningResource := schemas["ProvisioningResource"].(map[string]any)
	provisioningProperties := provisioningResource["properties"].(map[string]any)
	provisioningSpec := provisioningProperties["spec"].(map[string]any)
	if provisioningSpec["properties"].(map[string]any)["invocationPolicy"] == nil {
		t.Fatal("provisioning Client spec does not expose invocationPolicy")
	}
	paths := buildControlPlaneOpenAPI("http://example.test", "default")["paths"].(map[string]any)
	for _, path := range []string{
		"/api/w/{workspace}/audit-events",
		"/api/w/{workspace}/provisioning/import",
		"/api/w/{workspace}/provisioning/export",
		"/api/w/{workspace}/clients",
		"/api/w/{workspace}/clients/{client_id}",
		"/api/w/{workspace}/clients/{client_id}/token",
		"/api/w/{workspace}/clients/{client_id}/invocation-policy",
		"/api/w/{workspace}/clients/{client_id}/audit",
		"/api/w/{workspace}/clients/{client_id}/input-configs",
		"/api/w/{workspace}/clients/{client_id}/input-config-audit",
		"/api/w/{workspace}/apps/{app}/input-configs",
		"/api/w/{workspace}/apps/{app}/input-config-audit",
	} {
		if paths[path] == nil {
			t.Fatalf("missing path %s", path)
		}
	}
}
