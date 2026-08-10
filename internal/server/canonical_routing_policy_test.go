package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/gitsource"
	"github.com/imprun/windforce-core/internal/syncer"
)

type failingInitialPlacementCatalog struct {
	*catalog.FileCatalog
}

func (c *failingInitialPlacementCatalog) SetInitialAppRoutingPolicy(context.Context, string, string, catalog.RoutingPolicyPatch) (catalog.RoutingPolicy, error) {
	return catalog.RoutingPolicy{}, errors.New("injected placement failure")
}

func TestRegisterGitSourcePreviewsManifestAndStoresInitialRoutingPolicy(t *testing.T) {
	tempDir := t.TempDir()
	repoDir := createTestGitSourceRepo(t, tempDir, "repo", "apps/echo")
	fileCatalog := catalog.NewFileCatalog(filepath.Join(tempDir, "catalog.json"))
	httpServer := httptest.NewServer(New(Config{
		Catalog:    fileCatalog,
		Syncer:     &syncer.Syncer{CloneRoot: filepath.Join(tempDir, "clone")},
		GitSources: gitsource.NewFileRegistry(filepath.Join(tempDir, "git-sources.json")),
	}))
	defer httpServer.Close()

	probePayload, err := json.Marshal(map[string]any{
		"repo_url": filepath.ToSlash(repoDir),
		"branch":   "main",
		"subpath":  "apps/echo",
	})
	if err != nil {
		t.Fatal(err)
	}
	probeResp, err := http.Post(httpServer.URL+"/api/w/ws-a/git_sources/probe", "application/json", bytes.NewReader(probePayload))
	if err != nil {
		t.Fatal(err)
	}
	defer probeResp.Body.Close()
	if probeResp.StatusCode != http.StatusOK {
		t.Fatalf("probe status = %d", probeResp.StatusCode)
	}
	var probe struct {
		Manifest struct {
			AppKey         string          `json:"app_key"`
			WorkerTag      string          `json:"worker_tag"`
			LegacyRouteTag json.RawMessage `json:"route_tag"`
			Actions        []struct {
				WorkerTag string `json:"worker_tag"`
			} `json:"actions"`
		} `json:"manifest"`
	}
	if err := json.NewDecoder(probeResp.Body).Decode(&probe); err != nil {
		t.Fatal(err)
	}
	if probe.Manifest.AppKey != "echo" || probe.Manifest.WorkerTag != "default" || probe.Manifest.LegacyRouteTag != nil ||
		len(probe.Manifest.Actions) != 1 || probe.Manifest.Actions[0].WorkerTag != "default" {
		t.Fatalf("manifest preview = %#v", probe.Manifest)
	}

	registerPayload, err := json.Marshal(map[string]any{
		"name":     "source-a",
		"repo_url": filepath.ToSlash(repoDir),
		"branch":   "main",
		"subpath":  "apps/echo",
		"placement_policy": map[string]any{
			"tag_override":             "browser",
			"required_labels_override": []string{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	registerResp, err := http.Post(httpServer.URL+"/api/w/ws-a/git_sources", "application/json", bytes.NewReader(registerPayload))
	if err != nil {
		t.Fatal(err)
	}
	defer registerResp.Body.Close()
	if registerResp.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d", registerResp.StatusCode)
	}
	policy, err := fileCatalog.GetRoutingPolicy(context.Background(), "ws-a", "echo")
	if err != nil {
		t.Fatal(err)
	}
	if policy.RouteTagOverride == nil || *policy.RouteTagOverride != "browser" ||
		policy.RequiredLabelsOverride == nil || len(*policy.RequiredLabelsOverride) != 0 {
		t.Fatalf("initial routing policy = %#v", policy)
	}
}

func TestRegisterGitSourceRollsBackSourceWhenInitialPlacementFails(t *testing.T) {
	tempDir := t.TempDir()
	repoDir := createTestGitSourceRepo(t, tempDir, "repo", "apps/echo")
	registry := gitsource.NewFileRegistry(filepath.Join(tempDir, "git-sources.json"))
	fileCatalog := catalog.NewFileCatalog(filepath.Join(tempDir, "catalog.json"))
	httpServer := httptest.NewServer(New(Config{
		Catalog:    &failingInitialPlacementCatalog{FileCatalog: fileCatalog},
		Syncer:     &syncer.Syncer{CloneRoot: filepath.Join(tempDir, "clone")},
		GitSources: registry,
	}))
	defer httpServer.Close()

	payload, err := json.Marshal(map[string]any{
		"name":     "source-a",
		"repo_url": filepath.ToSlash(repoDir),
		"branch":   "main",
		"subpath":  "apps/echo",
		"placement_policy": map[string]any{
			"tag_override": "browser",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(httpServer.URL+"/api/w/ws-a/git_sources", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("register status = %d, want 500", resp.StatusCode)
	}
	snapshot, err := registry.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Sources) != 0 {
		t.Fatalf("git source survived failed initial placement: %#v", snapshot.Sources)
	}
}

func TestCanonicalRoutingPolicyLabelsAPI(t *testing.T) {
	fileCatalog := catalog.NewFileCatalog(filepath.Join(t.TempDir(), "catalog.json"))
	actionLabels := []string{"browser"}
	if err := fileCatalog.UpsertDeployment(context.Background(), contract.Deployment{
		Workspace: "ws-a", GitSourceID: "source-a", App: "echo", Commit: "commit-a",
		Tag: "manifest-app", RequiredLabels: []string{"linux"},
		Actions: map[string]contract.Action{
			"run": {Action: "run", RunsOn: &actionLabels},
		},
	}); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(New(Config{Catalog: fileCatalog}))
	defer httpServer.Close()

	patchCanonicalRoutingPolicy(t, httpServer.URL+"/api/w/ws-a/apps/echo", `{"tag_override":"operator-app","required_labels_override":["gpu"]}`)
	patchCanonicalRoutingPolicy(t, httpServer.URL+"/api/w/ws-a/apps/echo/actions/run", `{"required_labels_override":[]}`)

	resp, err := http.Get(httpServer.URL + "/api/w/ws-a/apps/echo")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		App struct {
			Tag                     string    `json:"tag"`
			TagOverride             *string   `json:"tag_override"`
			RequiredLabels          []string  `json:"required_labels"`
			RequiredLabelsOverride  *[]string `json:"required_labels_override"`
			EffectiveRouteTag       string    `json:"effective_route_tag"`
			EffectiveRequiredLabels []string  `json:"effective_required_labels"`
		} `json:"app"`
		Actions []struct {
			ActionKey               string    `json:"action_key"`
			RequiredLabels          []string  `json:"required_labels"`
			RequiredLabelsOverride  *[]string `json:"required_labels_override"`
			EffectiveRouteTag       string    `json:"effective_route_tag"`
			EffectiveRequiredLabels []string  `json:"effective_required_labels"`
		} `json:"actions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.App.Tag != "manifest-app" || body.App.TagOverride == nil || *body.App.TagOverride != "operator-app" || body.App.EffectiveRouteTag != "operator-app" {
		t.Fatalf("app route view = %#v", body.App)
	}
	if !reflect.DeepEqual(body.App.RequiredLabels, []string{"linux"}) || body.App.RequiredLabelsOverride == nil ||
		!reflect.DeepEqual(*body.App.RequiredLabelsOverride, []string{"gpu"}) ||
		!reflect.DeepEqual(body.App.EffectiveRequiredLabels, []string{"gpu"}) {
		t.Fatalf("app labels view = %#v", body.App)
	}
	if len(body.Actions) != 1 || !reflect.DeepEqual(body.Actions[0].RequiredLabels, []string{"browser"}) ||
		body.Actions[0].RequiredLabelsOverride == nil || len(*body.Actions[0].RequiredLabelsOverride) != 0 ||
		len(body.Actions[0].EffectiveRequiredLabels) != 0 {
		t.Fatalf("action labels view = %#v", body.Actions)
	}
	snapshot, err := fileCatalog.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	foundScopes := map[string]bool{}
	for _, record := range snapshot.Audit {
		if record.Kind != "execution_placement_updated" {
			continue
		}
		if record.Actor != "operator@example.test" {
			t.Fatalf("placement audit actor = %q", record.Actor)
		}
		var detail map[string]any
		if err := json.Unmarshal([]byte(record.Detail), &detail); err != nil {
			t.Fatalf("placement audit detail is not JSON: %q", record.Detail)
		}
		if detail["previous"] == nil || detail["new"] == nil {
			t.Fatalf("placement audit detail = %#v", detail)
		}
		scope, ok := detail["scope"].(string)
		if !ok || (scope != "app" && scope != "action") {
			t.Fatalf("placement audit scope = %#v", detail)
		}
		if scope == "action" && detail["action_key"] != "run" {
			t.Fatalf("action placement audit = %#v", detail)
		}
		foundScopes[scope] = true
	}
	if !foundScopes["app"] || !foundScopes["action"] {
		t.Fatalf("placement mutation audit scopes = %#v", foundScopes)
	}
}

func TestCanonicalRoutingPolicyRejectsInvalidLabels(t *testing.T) {
	fileCatalog := catalog.NewFileCatalog(filepath.Join(t.TempDir(), "catalog.json"))
	if err := fileCatalog.UpsertDeployment(context.Background(), contract.Deployment{
		Workspace: "ws-a", App: "echo", Commit: "commit-a",
		Actions: map[string]contract.Action{"run": {Action: "run"}},
	}); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(New(Config{Catalog: fileCatalog}))
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodPatch, httpServer.URL+"/api/w/ws-a/apps/echo", bytes.NewBufferString(`{"required_labels_override":["Has Space"]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestCanonicalPlacementViewsUseEmptyArraysWhenReleaseHasNoLabels(t *testing.T) {
	fileCatalog := catalog.NewFileCatalog(filepath.Join(t.TempDir(), "catalog.json"))
	if err := fileCatalog.UpsertDeployment(context.Background(), contract.Deployment{
		Workspace: "ws-a", App: "echo", Commit: "commit-a",
		Actions: map[string]contract.Action{"run": {Action: "run"}},
	}); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(New(Config{Catalog: fileCatalog}))
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/api/w/ws-a/apps/echo")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		App struct {
			RequiredLabels          json.RawMessage `json:"required_labels"`
			EffectiveRequiredLabels json.RawMessage `json:"effective_required_labels"`
		} `json:"app"`
		Actions []struct {
			RequiredLabels          json.RawMessage `json:"required_labels"`
			EffectiveRequiredLabels json.RawMessage `json:"effective_required_labels"`
		} `json:"actions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if string(body.App.RequiredLabels) != "[]" || string(body.App.EffectiveRequiredLabels) != "[]" || len(body.Actions) != 1 ||
		string(body.Actions[0].RequiredLabels) != "[]" || string(body.Actions[0].EffectiveRequiredLabels) != "[]" {
		t.Fatalf("empty label arrays = %#v", body)
	}
}

func patchCanonicalRoutingPolicy(t *testing.T, url string, body string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Windforce-Actor", "operator@example.test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var responseBody map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&responseBody)
		t.Fatalf("PATCH %s = %d %#v", url, resp.StatusCode, responseBody)
	}
}
