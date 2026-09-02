package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/imprun/windforce-core/internal/state"
)

func TestInvocationAdmissionProbeAuthenticatesAndDoesNotCreateRun(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	if _, err := store.CreateWorkspace(ctx, "ws", "Workspace", "tester"); err != nil {
		t.Fatal(err)
	}
	handler := New(Config{Store: store, AdminToken: "admin"})
	for _, route := range []string{
		"/api/v1/workspaces/ws/runs",
		"/api/v1/workspaces/ws/runs/wait?timeout=1s",
	} {
		t.Run(route, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, route, bytes.NewBufferString(`{"app":"demo","action":"run","input":{}}`))
			request.Header.Set("Authorization", "Bearer admin")
			request.Header.Set("Idempotency-Key", "probe-1")
			request.Header.Set(invocationAdmissionProbeHeader, "true")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var body invocationAdmissionProbeView
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.AdmissionID == "" || body.RequestFingerprint == "" || body.State != "ready" || body.RunID != "" {
				t.Fatalf("probe body = %#v", body)
			}
			if response.Header().Get(invocationAdmissionIDHeader) != body.AdmissionID ||
				response.Header().Get(invocationAdmissionFingerprintHeader) != body.RequestFingerprint ||
				response.Header().Get(invocationAdmissionProbedHeader) != "true" {
				t.Fatalf("probe headers = %v", response.Header())
			}
			if _, err := store.GetRun(ctx, body.AdmissionID); !errors.Is(err, state.ErrNotFound) {
				t.Fatalf("probe created Run: %v", err)
			}
		})
	}
}

func TestInvocationAdmissionProbeRejectsInvalidCredentialBeforeIdentity(t *testing.T) {
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	handler := New(Config{Store: store, AdminToken: "admin"})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws/runs", bytes.NewBufferString(`{"app":"demo","action":"run","input":{}}`))
	request.Header.Set("Authorization", "Bearer invalid")
	request.Header.Set("Idempotency-Key", "probe-1")
	request.Header.Set(invocationAdmissionProbeHeader, "true")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || response.Header().Get(invocationAdmissionIDHeader) != "" {
		t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
}

func TestAdmissionOutcomeResolveIsAdminOnlyIdempotentAndAllowedWhenArchived(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	if _, err := store.CreateWorkspace(ctx, "ws", "Workspace", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ArchiveWorkspace(ctx, "ws", "tester"); err != nil {
		t.Fatal(err)
	}
	handler := New(Config{Store: store, AdminToken: "admin", ManagedWorkspaces: true})
	body := `{"request_fingerprint":"fingerprint-1"}`

	unauthorized := httptest.NewRequest(http.MethodPost, "/api/w/ws/admission-outcomes/run_probe/resolve", bytes.NewBufferString(body))
	unauthorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorizedResponse.Code, unauthorizedResponse.Body.String())
	}

	for attempt := 0; attempt < 2; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/api/w/ws/admission-outcomes/run_probe/resolve", bytes.NewBufferString(body))
		request.Header.Set("Authorization", "Bearer admin")
		request.Header.Set("X-Windforce-Actor", "operator@example.test")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("attempt=%d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
		var outcome admissionOutcomeView
		if err := json.Unmarshal(response.Body.Bytes(), &outcome); err != nil {
			t.Fatal(err)
		}
		if outcome.State != "aborted" || outcome.AdmissionID != "run_probe" || outcome.RequestFingerprint != "fingerprint-1" {
			t.Fatalf("outcome = %#v", outcome)
		}
		if bytes.Contains(response.Body.Bytes(), []byte("operator@example.test")) || bytes.Contains(response.Body.Bytes(), []byte("resolved_by")) {
			t.Fatalf("outcome response leaked the resolving principal: %s", response.Body.String())
		}
	}

	conflict := httptest.NewRequest(http.MethodPost, "/api/w/ws/admission-outcomes/run_probe/resolve", bytes.NewBufferString(`{"request_fingerprint":"different"}`))
	conflict.Header.Set("Authorization", "Bearer admin")
	conflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(conflictResponse, conflict)
	if conflictResponse.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d body=%s", conflictResponse.Code, conflictResponse.Body.String())
	}
}

func TestAdmissionOutcomeContractAppearsInControlPlaneOpenAPI(t *testing.T) {
	document := buildControlPlaneOpenAPI("http://core.example.test", "ws")
	paths := document["paths"].(map[string]any)
	getPath := "/api/w/{workspace}/admission-outcomes/{admission_id}"
	resolvePath := getPath + "/resolve"
	get := paths[getPath].(map[string]any)["get"].(map[string]any)
	resolve := paths[resolvePath].(map[string]any)["post"].(map[string]any)
	if get["operationId"] != "getAdmissionOutcome" || resolve["operationId"] != "resolveAdmissionOutcome" {
		t.Fatalf("admission outcome operation ids are incomplete: get=%v resolve=%v", get["operationId"], resolve["operationId"])
	}
	for _, status := range []string{"200", "401", "403", "409", "500"} {
		if get["responses"].(map[string]any)[status] == nil {
			t.Errorf("admission outcome GET is missing response %s", status)
		}
	}
	for _, status := range []string{"200", "400", "401", "403", "409", "500"} {
		if resolve["responses"].(map[string]any)[status] == nil {
			t.Errorf("admission outcome resolve is missing response %s", status)
		}
	}
	requestSchema := resolve["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if requestSchema["$ref"] != "#/components/schemas/ResolveAdmissionOutcomeRequest" {
		t.Fatalf("resolve request schema = %#v", requestSchema)
	}
	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	resolveSchema := schemas["ResolveAdmissionOutcomeRequest"].(map[string]any)
	if resolveSchema["additionalProperties"] != false || resolveSchema["properties"].(map[string]any)["request_fingerprint"] == nil {
		t.Fatalf("resolve schema is not strict: %#v", resolveSchema)
	}
	outcome := schemas["AdmissionOutcome"].(map[string]any)
	properties := outcome["properties"].(map[string]any)
	for _, name := range []string{"workspace_id", "admission_id", "run_id", "state", "request_fingerprint", "created_at", "updated_at"} {
		if properties[name] == nil {
			t.Errorf("AdmissionOutcome is missing %s", name)
		}
	}
	if properties["resolved_by"] != nil {
		t.Fatal("AdmissionOutcome exposes the resolving principal")
	}
}
