package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/executionlimit"
	"github.com/imprun/windforce-core/internal/state"
)

func TestSystemInfoExposesSafeServiceConfiguration(t *testing.T) {
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	server := httptest.NewServer(New(Config{
		Store:               store,
		AdminToken:          "secret-admin-token",
		WorkerToken:         "secret-worker-token",
		JobTokenSecret:      "secret-job-token",
		SecretKey:           "secret-key-value",
		Wait:                250 * time.Millisecond,
		ManagedWorkspaces:   true,
		HTTPRouteProvider:   "kubernetes-gateway-api",
		UIMode:              UIModeDisabled,
		WorkerGroupOperator: WorkerGroupOperatorExternal,
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/w/default/system/info", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret-admin-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Service       string            `json:"service"`
		Workspace     string            `json:"workspace"`
		Ready         bool              `json:"ready"`
		Planes        map[string]bool   `json:"planes"`
		Backends      map[string]bool   `json:"backends"`
		Auth          map[string]bool   `json:"auth"`
		Capabilities  map[string]string `json:"capabilities"`
		RuntimeConfig struct {
			WaitMS              float64 `json:"wait_ms"`
			ManagedWorkspaces   bool    `json:"managed_workspaces"`
			HTTPRouteProvider   string  `json:"http_route_provider"`
			UIMode              string  `json:"ui_mode"`
			WorkerGroupOperator string  `json:"worker_group_operator"`
		} `json:"runtime_config"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Service != "windforce-lite" || body.Workspace != "default" || !body.Ready {
		t.Fatalf("body identity = %#v", body)
	}
	if !body.Planes["control_api"] || !body.Planes["invocation_api"] || !body.Planes["worker_api"] || body.Planes["web_ui"] ||
		!body.Planes["http_routes"] || body.Planes["execution_api"] || body.Planes["public_api"] {
		t.Fatalf("planes = %#v", body.Planes)
	}
	if !body.Backends["state_store"] || !body.Backends["http_route_provider"] {
		t.Fatalf("backends = %#v", body.Backends)
	}
	if !body.Auth["admin_token_configured"] || !body.Auth["worker_token_configured"] || !body.Auth["job_token_configured"] {
		t.Fatalf("auth = %#v", body.Auth)
	}
	if body.Capabilities["execution_limit_policy"] != "v1" || body.Capabilities["execution_limit_shape"] != executionlimit.FingerprintVersion {
		t.Fatalf("capabilities = %#v", body.Capabilities)
	}
	if body.RuntimeConfig.WaitMS != 250 {
		t.Fatalf("wait_ms = %#v", body.RuntimeConfig.WaitMS)
	}
	if !body.RuntimeConfig.ManagedWorkspaces {
		t.Fatal("managed_workspaces = false, want true")
	}
	if body.RuntimeConfig.HTTPRouteProvider != "kubernetes-gateway-api" {
		t.Fatalf("http_route_provider = %q", body.RuntimeConfig.HTTPRouteProvider)
	}
	if body.RuntimeConfig.UIMode != UIModeDisabled || body.RuntimeConfig.WorkerGroupOperator != WorkerGroupOperatorExternal {
		t.Fatalf("presentation runtime config = UI %q, operator %q", body.RuntimeConfig.UIMode, body.RuntimeConfig.WorkerGroupOperator)
	}
}
