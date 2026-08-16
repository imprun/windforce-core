package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/gitsource"
	"github.com/imprun/windforce-core/internal/state"
)

func TestRegisterGitSourceChecksAccessWithoutReadingManifest(t *testing.T) {
	tempDir := t.TempDir()
	repoDir := createTestGitSourceRepo(t, tempDir, "repo", "apps/echo")
	if err := os.WriteFile(filepath.Join(repoDir, "apps", "echo", "windforce.json"), []byte("not-json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repoDir, "add", "apps/echo/windforce.json")
	runTestGit(t, repoDir, "commit", "-m", "break manifest")
	registry := gitsource.NewFileRegistry(filepath.Join(tempDir, "git-sources.json"))
	httpServer := httptest.NewServer(New(Config{
		GitSources: registry,
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
	var probe map[string]any
	if err := json.NewDecoder(probeResp.Body).Decode(&probe); err != nil {
		t.Fatal(err)
	}
	if probe["reachable"] != true || probe["branch_exists"] != true || probe["manifest"] != nil {
		t.Fatalf("access-only probe = %#v", probe)
	}

	registerPayload, err := json.Marshal(map[string]any{
		"name":     "source-a",
		"repo_url": filepath.ToSlash(repoDir),
		"branch":   "main",
		"subpath":  "apps/echo",
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
	registered, err := registry.Get(context.Background(), "ws-a", "source-a")
	if err != nil {
		t.Fatal(err)
	}
	if registered.AppKey != "" || registered.LastSyncedCommit != nil {
		t.Fatalf("registration must not materialize app identity: %#v", registered)
	}
}

func TestRegisterGitSourceRejectsPlacementUntilAfterSync(t *testing.T) {
	tempDir := t.TempDir()
	repoDir := createTestGitSourceRepo(t, tempDir, "repo", "apps/echo")
	registry := gitsource.NewFileRegistry(filepath.Join(tempDir, "git-sources.json"))
	httpServer := httptest.NewServer(New(Config{
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
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("register status = %d, want 422", resp.StatusCode)
	}
	var response map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response["code"] != gitSourceErrorPlacementAfterSync {
		t.Fatalf("register response = %#v", response)
	}
	snapshot, err := registry.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Sources) != 0 {
		t.Fatalf("git source survived rejected initial placement: %#v", snapshot.Sources)
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
		PlacementPolicyRevision int64 `json:"placement_policy_revision"`
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
	if body.PlacementPolicyRevision != 2 {
		t.Fatalf("placement policy revision = %d, want 2", body.PlacementPolicyRevision)
	}
	snapshot, err := fileCatalog.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	foundScopes := map[string]bool{}
	placementAuditCount := 0
	for _, record := range snapshot.Audit {
		if record.Kind != "execution_placement_updated" {
			continue
		}
		placementAuditCount++
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
	if placementAuditCount != 2 {
		t.Fatalf("placement audit count = %d, want 2", placementAuditCount)
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

func TestCanonicalPlacementPreconditionSuccessReplayAndFailure(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	actionTag := "manifest-action"
	actionLabels := []string{"browser"}
	if _, err := store.PublishRelease(ctx, contract.Deployment{
		Workspace: "ws-a", GitSourceID: "source-a", App: "echo", Commit: "commit-a", Tag: "manifest-app",
		RequiredLabels: []string{"linux"}, Actions: map[string]contract.Action{
			"run": {Action: "run", Tag: &actionTag, RunsOn: &actionLabels},
		},
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterWorker(ctx, state.WorkerRecord{
		ID: "worker-ready", Tags: []string{"ready"}, Labels: []string{"gpu"}, Slots: 2, Status: state.WorkerStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(New(Config{Store: store, Catalog: store}))
	defer httpServer.Close()

	body := `{"tag_override":"ready","required_labels_override":["gpu"],"precondition":{"operation_id":"op-api-1","expected_policy_revision":0,"minimum_matching_slots":2}}`
	first := patchCanonicalRoutingPolicyResponse(t, httpServer.URL+"/api/w/ws-a/apps/echo", body, http.StatusOK)
	if first["placement_policy_revision"] != float64(1) || first["replayed"] != false {
		t.Fatalf("first placement response = %#v", first)
	}
	check, ok := first["placement_precondition"].(map[string]any)
	if !ok || check["applied_revision"] != float64(1) || check["minimum_matching_slots"] != float64(2) {
		t.Fatalf("first placement check = %#v", check)
	}
	targets, ok := check["targets"].([]any)
	if !ok || len(targets) != 2 {
		t.Fatalf("first placement targets = %#v", check["targets"])
	}
	for _, raw := range targets {
		target := raw.(map[string]any)
		if target["matching_workers"] != float64(1) || target["matching_slots"] != float64(2) {
			t.Fatalf("target = %#v", target)
		}
	}
	encodedFirst, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encodedFirst), "worker-ready") || strings.Contains(string(encodedFirst), "fingerprint") || strings.Contains(string(encodedFirst), "credential") {
		t.Fatalf("placement response leaked internal identity: %s", encodedFirst)
	}

	if err := store.DeregisterWorker(ctx, "worker-ready"); err != nil {
		t.Fatal(err)
	}
	replay := patchCanonicalRoutingPolicyResponse(t, httpServer.URL+"/api/w/ws-a/apps/echo", body, http.StatusOK)
	if replay["replayed"] != true || !reflect.DeepEqual(replay["placement_precondition"], first["placement_precondition"]) {
		t.Fatalf("replay response = %#v", replay)
	}

	failedBody := `{"tag_override":"missing","precondition":{"operation_id":"op-api-2","expected_policy_revision":1,"minimum_matching_slots":1}}`
	failed := patchCanonicalRoutingPolicyResponse(t, httpServer.URL+"/api/w/ws-a/apps/echo", failedBody, http.StatusUnprocessableEntity)
	if failed["reason"] != "insufficient_matching_capacity" || failed["placement_policy_revision"] != float64(1) {
		t.Fatalf("capacity error = %#v", failed)
	}

	resp, err := http.Get(httpServer.URL + "/api/w/ws-a/apps/echo")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var detail map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if detail["placement_policy_revision"] != float64(1) {
		t.Fatalf("app detail revision = %#v", detail)
	}
	audits, err := store.AuditTrail(ctx, "ws-a", "source-a")
	if err != nil {
		t.Fatal(err)
	}
	placementAudits := 0
	for _, audit := range audits {
		if audit.Kind == "execution_placement_updated" {
			placementAudits++
		}
	}
	if placementAudits != 1 {
		t.Fatalf("placement audits = %d, want 1", placementAudits)
	}
}

func TestControlOpenAPIExposesPlacementPreconditionContract(t *testing.T) {
	document := buildControlPlaneOpenAPI("http://127.0.0.1:18091", "ws-a")
	components := document["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	patch := schemas["ExecutionPlacementPatch"].(map[string]any)
	properties := patch["properties"].(map[string]any)
	if properties["precondition"] == nil || schemas["ExecutionPlacementPrecondition"] == nil ||
		schemas["ExecutionPlacementPreconditionResult"] == nil || schemas["ExecutionPlacementCapacityError"] == nil {
		t.Fatalf("placement schemas are incomplete: %#v", schemas)
	}
	paths := document["paths"].(map[string]any)
	for _, path := range []string{"/api/w/{workspace}/apps/{app}", "/api/w/{workspace}/apps/{app}/actions/{action}"} {
		patchOperation := paths[path].(map[string]any)["patch"].(map[string]any)
		responses := patchOperation["responses"].(map[string]any)
		if responses["409"] == nil || responses["422"] == nil {
			t.Fatalf("placement responses for %s = %#v", path, responses)
		}
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

func patchCanonicalRoutingPolicyResponse(t *testing.T, url string, body string, wantStatus int) map[string]any {
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
	var responseBody map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&responseBody); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("PATCH %s = %d, want %d: %#v", url, resp.StatusCode, wantStatus, responseBody)
	}
	return responseBody
}
