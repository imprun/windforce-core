package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/execution"
	"github.com/imprun/windforce-core/internal/state"
	triggerpkg "github.com/imprun/windforce-core/internal/trigger"
)

type serverTriggerAdmission struct {
	request execution.CreateRunRequest
	store   state.Store
	output  json.RawMessage
}

func (a *serverTriggerAdmission) CreateRun(ctx context.Context, request execution.CreateRunRequest) (execution.Admission, error) {
	a.request = request
	run := state.Run{
		ID:             "run-trigger-1",
		Adapter:        request.Adapter,
		App:            request.App,
		Action:         request.Action,
		State:          state.RunSucceeded,
		Deployment:     contract.Deployment{Workspace: request.Workspace},
		Output:         a.output,
		PrincipalKind:  string(request.Principal.Kind),
		PrincipalID:    request.Principal.ID,
		CreatedBy:      request.CreatedBy,
		PermissionedAs: request.PermissionedAs,
	}
	if a.store != nil {
		err := a.store.CreateRunAndEnqueue(ctx, run, state.Job{
			ID:    "job-trigger-1",
			RunID: run.ID,
			State: state.JobSucceeded,
			Payload: state.JobPayload{
				Workspace: request.Workspace,
				App:       request.App,
				Action:    request.Action,
			},
		})
		if err != nil {
			return execution.Admission{}, err
		}
	}
	return execution.Admission{Run: run}, nil
}

func TestCanonicalTriggerCRUDDoesNotReturnSecrets(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	store.SecretKey = "test-trigger-secret-key"
	if _, err := store.CreateWorkspace(ctx, "ws", "Workspace", "tester"); err != nil {
		t.Fatal(err)
	}
	handler := New(Config{
		Store:             store,
		ManagedWorkspaces: true,
		AdminToken:        "admin",
	})
	body := []byte(`{
		"name":"incoming",
		"kind":"webhook",
		"app":"demo",
		"action":"run",
		"config":{},
		"completion":{"mode":"poll"},
		"secret_config":{"secret":"must-not-be-returned"}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/api/w/ws/triggers", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer admin")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "must-not-be-returned") {
		t.Fatalf("secret leaked in response: %s", response.Body.String())
	}
	var created canonicalTrigger
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !created.HasSecret || created.Enabled {
		t.Fatalf("created = %#v", created)
	}
	if created.Completion.Mode != state.TriggerCompletionModePoll ||
		created.Response.Mode != state.TriggerResponseAsync {
		t.Fatalf("created policies = completion:%#v response:%#v", created.Completion, created.Response)
	}

	enable := httptest.NewRequest(http.MethodPost, "/api/w/ws/triggers/"+created.ID+"/enable", nil)
	enable.Header.Set("Authorization", "Bearer admin")
	enableResponse := httptest.NewRecorder()
	handler.ServeHTTP(enableResponse, enable)
	if enableResponse.Code != http.StatusOK {
		t.Fatalf("enable status = %d body = %s", enableResponse.Code, enableResponse.Body.String())
	}
	audit, err := store.ListTriggerAudit(ctx, "ws", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 2 || audit[1].Kind != "enabled" {
		t.Fatalf("audit = %#v", audit)
	}

	callbackBody := []byte(`{
		"name":"incoming",
		"kind":"webhook",
		"enabled":true,
		"app":"demo",
		"action":"run",
		"config":{},
		"completion":{"mode":"callback","callback":{"endpoint":"https://partner.example.test/completed"}},
		"response":{"mode":"async"},
		"secret_config":{"completion":{"signing_secret":"callback-secret"}}
	}`)
	callbackUpdate := httptest.NewRequest(http.MethodPut, "/api/w/ws/triggers/"+created.ID, bytes.NewReader(callbackBody))
	callbackUpdate.Header.Set("Authorization", "Bearer admin")
	callbackResponse := httptest.NewRecorder()
	handler.ServeHTTP(callbackResponse, callbackUpdate)
	if callbackResponse.Code != http.StatusOK {
		t.Fatalf("callback update status = %d body = %s", callbackResponse.Code, callbackResponse.Body.String())
	}
	merged, err := store.GetTrigger(ctx, "ws", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(merged.SecretConfig, []byte("must-not-be-returned")) ||
		!bytes.Contains(merged.SecretConfig, []byte("callback-secret")) {
		t.Fatalf("secret patch did not preserve source and completion secrets: %s", merged.SecretConfig)
	}

	scheduleBody := []byte(`{
		"name":"daily",
		"kind":"schedule",
		"enabled":false,
		"app":"demo",
		"action":"run",
		"completion":{"mode":"none"},
		"config":{"cron":"0 9 * * *","timezone":"Asia/Seoul"}
	}`)
	update := httptest.NewRequest(http.MethodPut, "/api/w/ws/triggers/"+created.ID, bytes.NewReader(scheduleBody))
	update.Header.Set("Authorization", "Bearer admin")
	updateResponse := httptest.NewRecorder()
	handler.ServeHTTP(updateResponse, update)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update status = %d body = %s", updateResponse.Code, updateResponse.Body.String())
	}
	var scheduled canonicalTrigger
	if err := json.Unmarshal(updateResponse.Body.Bytes(), &scheduled); err != nil {
		t.Fatal(err)
	}
	if scheduled.HasSecret {
		t.Fatalf("stale webhook secret remains represented: %#v", scheduled)
	}
	stored, err := store.GetTrigger(ctx, "ws", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored.SecretConfig) != "null" {
		t.Fatalf("stale webhook secret was not cleared: %s", stored.SecretConfig)
	}
}

func TestWebhookTriggerIngressUsesSignatureWithoutAPIBearer(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	store.SecretKey = "test-trigger-secret-key"
	if _, err := store.CreateWorkspace(ctx, "ws", "Workspace", "tester"); err != nil {
		t.Fatal(err)
	}
	definition, err := store.CreateTrigger(ctx, state.TriggerDefinition{
		WorkspaceID:  "ws",
		Name:         "incoming",
		Kind:         triggerpkg.KindWebhook,
		Enabled:      true,
		AppKey:       "demo",
		ActionKey:    "run",
		Config:       json.RawMessage(`{}`),
		SecretConfig: json.RawMessage(`{"secret":"top-secret"}`),
	}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	admission := &serverTriggerAdmission{}
	manager := &triggerpkg.Manager{
		Store:     store,
		Admission: admission,
		Factories: triggerpkg.DefaultFactories(),
	}
	if err := manager.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}

	handler := New(Config{
		Store:          store,
		AdminToken:     "admin",
		TriggerManager: manager,
	})
	body := []byte(`{"hello":"world"}`)
	mac := hmac.New(sha256.New, []byte("top-secret"))
	_, _ = mac.Write(body)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workspaces/ws/triggers/"+definition.ID+"/events",
		bytes.NewReader(body),
	)
	request.Header.Set("X-WF-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	if admission.request.Workspace != "ws" ||
		admission.request.IdempotencyKey == "" ||
		admission.request.Adapter != "trigger:webhook" {
		t.Fatalf("admission = %#v", admission.request)
	}
}

func TestWebhookTriggerIngressWaitsForRawResultWhenConfigured(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	store.SecretKey = "test-trigger-secret-key"
	if _, err := store.CreateWorkspace(ctx, "ws", "Workspace", "tester"); err != nil {
		t.Fatal(err)
	}
	definition, err := store.CreateTrigger(ctx, state.TriggerDefinition{
		WorkspaceID:  "ws",
		Name:         "synchronous incoming",
		Kind:         triggerpkg.KindWebhook,
		Enabled:      true,
		AppKey:       "demo",
		ActionKey:    "run",
		Config:       json.RawMessage(`{}`),
		Completion:   state.TriggerCompletionPolicy{Mode: state.TriggerCompletionModePoll},
		Response:     state.TriggerResponsePolicy{Mode: state.TriggerResponseWait, TimeoutSeconds: 5},
		SecretConfig: json.RawMessage(`{"secret":"top-secret"}`),
	}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	admission := &serverTriggerAdmission{store: store, output: json.RawMessage(`{"ok":true}`)}
	manager := &triggerpkg.Manager{
		Store:     store,
		Admission: admission,
		Factories: triggerpkg.DefaultFactories(),
	}
	if err := manager.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	handler := New(Config{Store: store, AdminToken: "admin", TriggerManager: manager})
	body := []byte(`{"hello":"world"}`)
	mac := hmac.New(sha256.New, []byte("top-secret"))
	_, _ = mac.Write(body)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workspaces/ws/triggers/"+definition.ID+"/events",
		bytes.NewReader(body),
	)
	request.Header.Set("X-WF-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK ||
		result["ok"] != true ||
		response.Header().Get("X-WF-Run-Id") != "run-trigger-1" {
		t.Fatalf("status = %d headers = %#v body = %s", response.Code, response.Header(), response.Body.String())
	}
}

func TestControlPlaneOpenAPIIncludesWriteOnlyTriggerSecrets(t *testing.T) {
	document := buildControlPlaneOpenAPI("http://core.example.test", "ws")
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, expected := range []string{
		`/api/w/{workspace}/triggers`,
		`/api/w/{workspace}/triggers/{triggerId}/routes`,
		`/api/w/{workspace}/http-route-bindings/{bindingId}/status`,
		`/api/v1/workspaces/{workspace}/triggers/{triggerId}/events`,
		`"secret_config"`,
		`"writeOnly":true`,
		`"TriggerCompletionPolicy"`,
		`"TriggerResponsePolicy"`,
		`"status_url"`,
		`"result_url"`,
		`"HTTPRouteBindingStatusRequest"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("OpenAPI missing %q", expected)
		}
	}
	if strings.Contains(text, "must-not-be-returned") || strings.Contains(text, "top-secret") {
		t.Fatal("OpenAPI contains a trigger secret value")
	}
}
