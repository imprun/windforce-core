package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
)

func TestCanonicalActionDescriptionPreservesOpaquePublicInterfaces(t *testing.T) {
	fileCatalog := catalog.NewFileCatalog(filepath.Join(t.TempDir(), "catalog.json"))
	declaration := json.RawMessage(`{"contract":"example.interface/v1","metadata":{"priority":1}}`)
	if _, err := fileCatalog.PublishRelease(context.Background(), contract.Deployment{
		Workspace: "ws-a", GitSourceID: "source-a", APIVersion: contract.AppManifestV2,
		App: "echo", Commit: "commit-a", Entrypoint: "main.ts",
		Actions: map[string]contract.Action{
			"run": {
				Action:           "run",
				InputSchemaBody:  json.RawMessage(`{"type":"object"}`),
				OutputSchemaBody: json.RawMessage(`{"type":"object"}`),
				PublicInterfaces: []json.RawMessage{declaration},
			},
		},
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(New(Config{Catalog: fileCatalog}))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/w/ws-a/apps/echo/actions/run")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.StatusCode, body)
	}
	var action canonicalActionModel
	if err := json.Unmarshal(body, &action); err != nil {
		t.Fatal(err)
	}
	if len(action.PublicInterfaces) != 1 || string(action.PublicInterfaces[0]) != string(declaration) {
		t.Fatalf("public interfaces = %#v", action.PublicInterfaces)
	}

	appResponse, err := http.Get(server.URL + "/api/w/ws-a/apps/echo")
	if err != nil {
		t.Fatal(err)
	}
	defer appResponse.Body.Close()
	appBody, err := io.ReadAll(appResponse.Body)
	if err != nil {
		t.Fatal(err)
	}
	if appResponse.StatusCode != http.StatusOK ||
		!strings.Contains(string(appBody), `"api_version":"windforce.app-manifest/v2"`) ||
		!strings.Contains(string(appBody), `"public_interfaces"`) {
		t.Fatalf("app description status = %d body = %s", appResponse.StatusCode, appBody)
	}
}
