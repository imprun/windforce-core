package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/bundle"
	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/executionbundle"
	"github.com/imprun/windforce-core/internal/gitsource"
	"github.com/imprun/windforce-core/internal/remoteworker"
	actionruntime "github.com/imprun/windforce-core/internal/runtime"
	"github.com/imprun/windforce-core/internal/state"
	"github.com/imprun/windforce-core/internal/syncer"
	"github.com/imprun/windforce-core/internal/worker"
)

// TestTypeScriptTier1ExternalAppsE2E is an opt-in contract test against the
// real data-team demo and sample repositories. The Application build step
// creates a deployment Git containing only a bundled entrypoint and canonical
// manifest. Core never imports or identifies their Application SDK.
//
// Run with:
//
//	WINDFORCE_TYPESCRIPT_APPS_ROOT=/path/to/scraping/apps go test ./internal/server -run TestTypeScriptTier1ExternalAppsE2E -count=1 -v
func TestTypeScriptTier1ExternalAppsE2E(t *testing.T) {
	appsRoot := strings.TrimSpace(os.Getenv("WINDFORCE_TYPESCRIPT_APPS_ROOT"))
	if appsRoot == "" {
		t.Skip("set WINDFORCE_TYPESCRIPT_APPS_ROOT to run external TypeScript App E2E")
	}
	requireCommand(t, "bun")
	requireCommand(t, "git")

	for _, test := range []struct {
		app    string
		action string
		input  string
	}{
		{app: "sample", action: "echo", input: `{"TASKID":"sample-e2e"}`},
		{app: "demo", action: "parse", input: `{"HTML":"<table><tr><td>A</td><td>B</td></tr></table>"}`},
	} {
		t.Run(test.app, func(t *testing.T) {
			sourceDir := filepath.Join(appsRoot, test.app)
			if info, err := os.Stat(sourceDir); err != nil || !info.IsDir() {
				t.Fatalf("App source %s is unavailable: %v", sourceDir, err)
			}
			deploymentRepo := buildTypeScriptDeploymentRepo(t, sourceDir)
			runExternalAppThroughLocalAndRemoteWorker(t, deploymentRepo, test.app, test.action, test.input)
		})
	}
}

func buildTypeScriptDeploymentRepo(t *testing.T, sourceDir string) string {
	t.Helper()
	describe := runE2ECommand(t, sourceDir, "bun", "main.ts", "--describe")
	var manifest map[string]any
	if err := json.Unmarshal(describe, &manifest); err != nil {
		t.Fatalf("decode App description: %v: %s", err, describe)
	}
	manifest["entrypoint"] = "main.js"
	manifest["scriptLang"] = contract.ScriptLangTypeScript

	repoDir := filepath.Join(t.TempDir(), "deployment")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	externalizeE2ESchemas(t, repoDir, manifest)
	runE2ECommand(t, sourceDir, "bun", "build", "main.ts", "--target=bun", "--format=esm", "--outfile="+filepath.Join(repoDir, "main.js"))
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "windforce.json"), append(manifestBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	runE2ECommand(t, repoDir, "git", "init")
	runE2ECommand(t, repoDir, "git", "checkout", "-b", "main")
	runE2ECommand(t, repoDir, "git", "config", "user.email", "windforce-e2e@example.invalid")
	runE2ECommand(t, repoDir, "git", "config", "user.name", "Windforce E2E")
	runE2ECommand(t, repoDir, "git", "add", ".")
	runE2ECommand(t, repoDir, "git", "commit", "-m", "build TypeScript deployment")
	return repoDir
}

func externalizeE2ESchemas(t *testing.T, repoDir string, manifest map[string]any) {
	t.Helper()
	actions, ok := manifest["actions"].(map[string]any)
	if !ok {
		t.Fatalf("App description has no actions object: %#v", manifest["actions"])
	}
	for actionName, rawAction := range actions {
		action, ok := rawAction.(map[string]any)
		if !ok {
			t.Fatalf("App action %s is not an object", actionName)
		}
		for _, schemaName := range []string{"inputSchema", "outputSchema", "operatorSettingsSchema"} {
			schema, ok := action[schemaName]
			if !ok || schema == nil {
				continue
			}
			filename := actionName + "." + schemaName + ".json"
			contents, err := json.MarshalIndent(schema, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(repoDir, filename), append(contents, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
			action[schemaName] = filename
		}
	}
}

func runExternalAppThroughLocalAndRemoteWorker(t *testing.T, repoDir string, app string, action string, input string) {
	t.Helper()
	tempDir := t.TempDir()
	workspaceID := "typescript-e2e"
	workerToken := "worker-e2e-token"
	stateStore := state.NewLocalStore(filepath.Join(tempDir, "state.json"))
	stateStore.ConfigureInputCrypto("typescript-e2e-secret", "")
	if _, err := stateStore.CreateWorkspace(context.Background(), workspaceID, workspaceID, "e2e"); err != nil {
		t.Fatal(err)
	}
	bundleStore := bundle.NewLocalStore(filepath.Join(tempDir, "source-store"))
	executionStore := executionbundle.NewLocalStore(filepath.Join(tempDir, "execution-store"))
	fileCatalog := catalog.NewFileCatalog(filepath.Join(tempDir, "catalog.json"))
	runtimeRunner := &actionruntime.Runner{
		Store:         bundleStore,
		ArtifactStore: executionStore,
		CacheRoot:     filepath.Join(tempDir, "local-cache"),
	}
	handler := New(Config{
		Store:            stateStore,
		Catalog:          fileCatalog,
		Syncer:           &syncer.Syncer{Store: bundleStore, CloneRoot: filepath.Join(tempDir, "clones")},
		ExecutionBundles: runtimeRunner,
		GitSources:       gitsource.NewFileRegistry(filepath.Join(tempDir, "git-sources.json")),
		WorkerToken:      workerToken,
		ArtifactStore:    executionStore,
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	registeredID := registerE2EGitSource(t, server.URL, workspaceID, repoDir, app)
	syncE2EGitSource(t, server.URL, workspaceID, registeredID)
	publishE2EGitSource(t, server.URL, workspaceID, registeredID)

	localRun := admitTestRun(t, stateStore, server.URL, workspaceID, app, action, input)
	localProcessor := worker.Processor{
		Store:    stateStore,
		Runner:   runtimeRunner,
		WorkerID: "typescript-local",
		Group:    "e2e",
		LeaseTTL: time.Minute,
	}
	processed, err := localProcessor.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("local Worker processed=%v err=%v", processed, err)
	}
	assertOpaqueAppRunSucceeded(t, stateStore, localRun.RunID)

	remoteRun := admitTestRun(t, stateStore, server.URL, workspaceID, app, action, input)
	remoteBackend := remoteworker.New(server.URL, workerToken)
	remoteRecord := state.WorkerRecord{ID: "typescript-remote", Group: "e2e", Slots: 1, Status: state.WorkerStatusActive}
	if err := remoteBackend.RegisterWorker(context.Background(), remoteRecord); err != nil {
		t.Fatal(err)
	}
	defer remoteBackend.DeregisterWorker(context.Background(), remoteRecord.ID)
	remoteProcessor := worker.Processor{
		Store: remoteBackend,
		Runner: &actionruntime.Runner{
			ArtifactStore: remoteworker.ArtifactStore{Client: remoteBackend},
			CacheRoot:     filepath.Join(tempDir, "remote-cache"),
			BaseURL:       server.URL,
			APIToken:      workerToken,
		},
		WorkerID: remoteRecord.ID,
		Group:    remoteRecord.Group,
		LeaseTTL: time.Minute,
	}
	processed, err = remoteProcessor.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("remote Worker processed=%v err=%v", processed, err)
	}
	assertOpaqueAppRunSucceeded(t, stateStore, remoteRun.RunID)
}

func registerE2EGitSource(t *testing.T, baseURL string, workspaceID string, repoDir string, name string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"name":     name,
		"repo_url": filepath.ToSlash(repoDir),
		"branch":   "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(baseURL+"/api/w/"+workspaceID+"/git_sources", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("register %s = %d: %s", name, response.StatusCode, payload)
	}
	var registered struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&registered); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprint(registered.ID)
}

func syncE2EGitSource(t *testing.T, baseURL string, workspaceID string, sourceID string) {
	t.Helper()
	response, err := http.Post(baseURL+"/api/w/"+workspaceID+"/git_sources/"+sourceID+"/sync", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("sync source = %d: %s", response.StatusCode, payload)
	}
}

func publishE2EGitSource(t *testing.T, baseURL string, workspaceID string, sourceID string) {
	t.Helper()
	request, err := http.NewRequest(
		http.MethodPost,
		baseURL+"/api/w/"+workspaceID+"/git_sources/"+sourceID+"/deploy",
		bytes.NewBufferString(`{"confirm":true,"message":"TypeScript Tier 1 E2E"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Windforce-Actor", "typescript-e2e")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("publish source = %d: %s", response.StatusCode, payload)
	}
}

func assertOpaqueAppRunSucceeded(t *testing.T, store *state.LocalStore, runID string) {
	t.Helper()
	run, err := store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.State != state.RunSucceeded || run.Result == nil {
		t.Fatalf("Run %s = state %s result %#v", runID, run.State, run.Result)
	}
	if !bytes.Contains(run.Result.Output, []byte(`"ENVELOPE":"dh.v1"`)) ||
		!bytes.Contains(run.Result.Output, []byte(`"RESULT":"SUCCESS"`)) {
		t.Fatalf("Run %s output = %s", runID, run.Result.Output)
	}
}

func runE2ECommand(t *testing.T, dir string, name string, args ...string) []byte {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	command.Env = os.Environ()
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return output
}

func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s is not installed", name)
	}
}
