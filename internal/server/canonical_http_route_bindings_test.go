package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imprun/windforce-core/internal/state"
)

func TestCanonicalHTTPRouteBindingLifecycle(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	store.SecretKey = "test-http-route-binding-key"
	trigger, err := store.CreateTrigger(ctx, state.TriggerDefinition{
		WorkspaceID: "ws",
		Name:        "incoming",
		Kind:        "webhook",
		AppKey:      "demo",
		ActionKey:   "run",
		Config:      json.RawMessage(`{}`),
	}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	handler := New(Config{
		Store:             store,
		AdminToken:        "admin",
		HTTPRouteProvider: "kubernetes-gateway-api",
	})

	create := authenticatedRouteRequest(t, http.MethodPost,
		"/api/w/ws/triggers/"+trigger.ID+"/routes",
		`{"hostname":"hooks.example.com","path":"/gale/events","visibility":"public","provider":"auto"}`)
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, create)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", createResponse.Code, createResponse.Body.String())
	}
	var created canonicalHTTPRouteBinding
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.State != state.HTTPRouteBindingPending || created.Generation != 1 {
		t.Fatalf("created = %#v", created)
	}

	providerList := authenticatedRouteRequest(t, http.MethodGet,
		"/api/w/ws/http-route-bindings?include_deleted=true&provider=kubernetes-gateway-api", "")
	providerListResponse := httptest.NewRecorder()
	handler.ServeHTTP(providerListResponse, providerList)
	if providerListResponse.Code != http.StatusOK ||
		!strings.Contains(providerListResponse.Body.String(), `"configured_provider":"kubernetes-gateway-api"`) ||
		!strings.Contains(providerListResponse.Body.String(), created.ID) {
		t.Fatalf("provider list status = %d body = %s", providerListResponse.Code, providerListResponse.Body.String())
	}

	ready := authenticatedRouteRequest(t, http.MethodPut,
		"/api/w/ws/http-route-bindings/"+created.ID+"/status",
		`{"state":"ready","public_url":"https://hooks.example.com/gale/events","observed_generation":1}`)
	readyResponse := httptest.NewRecorder()
	handler.ServeHTTP(readyResponse, ready)
	if readyResponse.Code != http.StatusOK ||
		!strings.Contains(readyResponse.Body.String(), `"state":"ready"`) {
		t.Fatalf("ready status = %d body = %s", readyResponse.Code, readyResponse.Body.String())
	}

	deleteRequest := authenticatedRouteRequest(t, http.MethodDelete,
		"/api/w/ws/triggers/"+trigger.ID+"/routes/"+created.ID, "")
	deleteResponse := httptest.NewRecorder()
	handler.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusAccepted {
		t.Fatalf("delete status = %d body = %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	var deleting canonicalHTTPRouteBinding
	if err := json.Unmarshal(deleteResponse.Body.Bytes(), &deleting); err != nil {
		t.Fatal(err)
	}
	if deleting.State != state.HTTPRouteBindingDeleting || deleting.Generation != 2 {
		t.Fatalf("deleting = %#v", deleting)
	}

	deleted := authenticatedRouteRequest(t, http.MethodPut,
		"/api/w/ws/http-route-bindings/"+created.ID+"/status",
		`{"state":"deleted","observed_generation":2}`)
	deletedResponse := httptest.NewRecorder()
	handler.ServeHTTP(deletedResponse, deleted)
	if deletedResponse.Code != http.StatusOK ||
		!strings.Contains(deletedResponse.Body.String(), `"state":"deleted"`) ||
		!strings.Contains(deletedResponse.Body.String(), `"deleted_at"`) {
		t.Fatalf("deleted status = %d body = %s", deletedResponse.Code, deletedResponse.Body.String())
	}

	list := authenticatedRouteRequest(t, http.MethodGet,
		"/api/w/ws/triggers/"+trigger.ID+"/routes", "")
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK || listResponse.Body.String() != "{\"items\":[]}\n" {
		t.Fatalf("active list status = %d body = %s", listResponse.Code, listResponse.Body.String())
	}
	audit := authenticatedRouteRequest(t, http.MethodGet,
		"/api/w/ws/triggers/"+trigger.ID+"/routes/"+created.ID+"/audit", "")
	auditResponse := httptest.NewRecorder()
	handler.ServeHTTP(auditResponse, audit)
	if auditResponse.Code != http.StatusOK ||
		!strings.Contains(auditResponse.Body.String(), `"delete_requested"`) ||
		!strings.Contains(auditResponse.Body.String(), `"status_changed"`) {
		t.Fatalf("audit status = %d body = %s", auditResponse.Code, auditResponse.Body.String())
	}
}

func TestCanonicalHTTPRouteBindingRejectsNonWebhookTrigger(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	store.SecretKey = "test-http-route-binding-key"
	trigger, err := store.CreateTrigger(ctx, state.TriggerDefinition{
		WorkspaceID: "ws",
		Name:        "daily",
		Kind:        "schedule",
		AppKey:      "demo",
		ActionKey:   "run",
		Config:      json.RawMessage(`{"cron":"0 0 * * *"}`),
	}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	handler := New(Config{Store: store, AdminToken: "admin"})
	request := authenticatedRouteRequest(t, http.MethodPost,
		"/api/w/ws/triggers/"+trigger.ID+"/routes",
		`{"path":"/daily"}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), "require a webhook trigger") {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
}

func authenticatedRouteRequest(t *testing.T, method string, target string, body string) *http.Request {
	t.Helper()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	request := httptest.NewRequest(method, target, reader)
	request.Header.Set("Authorization", "Bearer admin")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}
