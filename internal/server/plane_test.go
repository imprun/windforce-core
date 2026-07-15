package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/imprun/windforce-lite/internal/state"
)

func TestControlPlaneOnlyServesControlRoutesAndWebUI(t *testing.T) {
	handler := New(Config{
		Store:            state.NewLocalStore(filepath.Join(t.TempDir(), "state.json")),
		EnableControlAPI: true,
		EnableWebUI:      true,
	})

	assertHandlerStatus(t, handler, "/api/w/default/openapi.json", http.StatusOK)
	assertHandlerStatus(t, handler, "/ui/", http.StatusOK)
	assertHandlerStatus(t, handler, "/execution/v1/openapi.json", http.StatusNotFound)
	assertHandlerStatus(t, handler, "/api/w/default/state?path=runtime/value", http.StatusNotFound)
}

func TestExecutionPlaneOnlyServesExecutionAndRuntimeRoutes(t *testing.T) {
	handler := New(Config{
		Store:              state.NewLocalStore(filepath.Join(t.TempDir(), "state.json")),
		EnableExecutionAPI: true,
	})

	assertHandlerStatus(t, handler, "/execution/v1/openapi.json", http.StatusOK)
	assertHandlerStatus(t, handler, "/api/w/default/state?path=runtime/value", http.StatusOK)
	assertHandlerStatus(t, handler, "/api/w/default/openapi.json", http.StatusNotFound)
	assertHandlerStatus(t, handler, "/ui/", http.StatusNotFound)
}

func assertHandlerStatus(t *testing.T, handler http.Handler, path string, want int) {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != want {
		t.Fatalf("GET %s status = %d, want %d; body=%s", path, response.Code, want, response.Body.String())
	}
}
