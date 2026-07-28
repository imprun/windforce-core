package controlcli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/imprun/windforce-core/internal/bundle"
	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/controlcli"
	"github.com/imprun/windforce-core/internal/gitsource"
	"github.com/imprun/windforce-core/internal/server"
	"github.com/imprun/windforce-core/internal/state"
	"github.com/imprun/windforce-core/internal/syncer"
)

type integrationBundleManager struct{}

func (integrationBundleManager) BuildExecutionBundle(_ context.Context, deployment contract.Deployment) (contract.Deployment, error) {
	deployment.BundleDigest = "sha256:" + strings.Repeat("a", 64)
	deployment.BundleURI = "execution-bundle://sha256/" + strings.Repeat("a", 64)
	return deployment, nil
}

func (integrationBundleManager) ValidateExecutionBundle(context.Context, contract.Deployment) error {
	return nil
}

func TestWFDirectCellPublishReleaseRunWatchAndResult(t *testing.T) {
	tempDir := t.TempDir()
	remoteDir := filepath.Join(tempDir, "remote.git")
	workDir := filepath.Join(tempDir, "work")
	runIntegrationGit(t, tempDir, "init", "--bare", remoteDir)
	runIntegrationGit(t, tempDir, "clone", remoteDir, workDir)
	runIntegrationGit(t, workDir, "checkout", "-b", "main")
	runIntegrationGit(t, workDir, "config", "user.email", "wf-integration@example.com")
	runIntegrationGit(t, workDir, "config", "user.name", "WF Integration")

	appDir := filepath.Join(workDir, "apps", "echo")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(appDir, "windforce.json"),
		[]byte(`{"app":"echo","entrypoint":"main.ts","actions":{"run":{}}}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(appDir, "main.ts"),
		[]byte("export async function main(ctx) { return ctx.input }\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	runIntegrationGit(t, workDir, "add", "-A")
	runIntegrationGit(t, workDir, "commit", "-m", "publish fixture")
	runIntegrationGit(t, workDir, "push", "-u", "origin", "main")
	commit := strings.TrimSpace(runIntegrationGit(t, workDir, "rev-parse", "HEAD"))

	releaseCatalog := catalog.NewFileCatalog(filepath.Join(tempDir, "catalog.json"))
	stateStore := state.NewLocalStore(filepath.Join(tempDir, "state.json"))
	if _, err := stateStore.CreateWorkspace(context.Background(), "team", "Team", "integration"); err != nil {
		t.Fatal(err)
	}
	cell := httptest.NewServer(server.New(server.Config{
		Store:            stateStore,
		Catalog:          releaseCatalog,
		Syncer:           &syncer.Syncer{Store: bundle.NewLocalStore(filepath.Join(tempDir, "source-store")), CloneRoot: filepath.Join(tempDir, "clones")},
		ExecutionBundles: integrationBundleManager{},
		GitSources:       gitsource.NewFileRegistry(filepath.Join(tempDir, "git-sources.json")),
		AdminToken:       "integration-token",
	}))
	defer cell.Close()

	t.Setenv("WF_CONFIG", filepath.Join(tempDir, "wf-config.json"))
	t.Setenv("WF_TOKEN", "integration-token")
	var stdout, stderr bytes.Buffer
	exit := controlcli.RunWF(
		[]string{
			"--api-url", cell.URL,
			"--workspace", "team",
			"--actor", "integration@example.com",
			"app", "publish", appDir,
			"--quiet",
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != controlcli.ExitOK {
		t.Fatalf("wf exit=%d stderr=%s", exit, stderr.String())
	}
	var published map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	releaseID, _ := published["release_id"].(string)
	if published["app"] != "echo" ||
		published["commit"] != commit ||
		strings.TrimSpace(releaseID) == "" ||
		published["bundle_status"] != "ready" {
		t.Fatalf("published = %#v", published)
	}

	historyRequest, err := http.NewRequest(http.MethodGet, cell.URL+"/api/w/team/apps/echo/history", nil)
	if err != nil {
		t.Fatal(err)
	}
	historyRequest.Header.Set("Authorization", "Bearer integration-token")
	response, err := http.DefaultClient.Do(historyRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("history status = %d", response.StatusCode)
	}
	var history []struct {
		ID     string `json:"id"`
		Commit string `json:"commit_sha"`
		Active bool   `json:"active"`
	}
	if err := json.NewDecoder(response.Body).Decode(&history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].ID != releaseID || history[0].Commit != commit || !history[0].Active {
		t.Fatalf("history = %#v", history)
	}

	releaseRaw := runWFDirectCellIntegration(t, cell.URL, "release", "view", "echo", releaseID)
	var release struct {
		ID        string `json:"id"`
		CommitSHA string `json:"commit_sha"`
		Active    bool   `json:"active"`
	}
	if err := json.Unmarshal(releaseRaw, &release); err != nil {
		t.Fatal(err)
	}
	if release.ID != releaseID || release.CommitSHA != commit || !release.Active {
		t.Fatalf("release view = %#v", release)
	}

	status, workerBody := postWorkerIntegration(
		t,
		cell.URL+"/worker/v1/workers",
		[]byte(`{"id":"wf-integration-worker","group":"default","labels":[],"slots":1}`),
	)
	if status != http.StatusCreated {
		t.Fatalf("worker registration = %d: %s", status, workerBody)
	}

	runRaw := runWFDirectCellIntegration(
		t,
		cell.URL,
		"run",
		"create",
		"echo",
		"run",
		"--input",
		`{"message":"hi"}`,
	)
	var createdRun struct {
		ID string `json:"run_id"`
	}
	if err := json.Unmarshal(runRaw, &createdRun); err != nil {
		t.Fatal(err)
	}
	if createdRun.ID == "" {
		t.Fatalf("created Run = %s", runRaw)
	}

	status, claimBody := postWorkerIntegration(
		t,
		cell.URL+"/worker/v1/claims",
		[]byte(`{"worker_id":"wf-integration-worker","labels":[]}`),
	)
	if status != http.StatusOK {
		t.Fatalf("worker claim = %d: %s", status, claimBody)
	}
	var claim struct {
		Job struct {
			ID      string `json:"id"`
			Payload struct {
				Input json.RawMessage `json:"input"`
			} `json:"payload"`
		} `json:"job"`
		Lease json.RawMessage `json:"lease"`
	}
	if err := json.Unmarshal(claimBody, &claim); err != nil {
		t.Fatal(err)
	}
	if claim.Job.ID == "" || !bytes.Contains(claim.Job.Payload.Input, []byte(`"hi"`)) {
		t.Fatalf("worker claim = %s", claimBody)
	}
	completion, err := json.Marshal(map[string]any{
		"lease":   claim.Lease,
		"outcome": "succeeded",
		"result": map[string]any{
			"app":      "echo",
			"action":   "run",
			"output":   map[string]any{"message": "hi"},
			"exitCode": 0,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	status, completionBody := postWorkerIntegration(
		t,
		cell.URL+"/worker/v1/jobs/"+claim.Job.ID+"/complete",
		completion,
	)
	if status != http.StatusNoContent {
		t.Fatalf("worker completion = %d: %s", status, completionBody)
	}

	watchedResult := runWFDirectCellIntegration(
		t,
		cell.URL,
		"run",
		"watch",
		createdRun.ID,
		"--interval",
		"100ms",
		"--timeout",
		"5s",
		"--result",
		"--quiet",
	)
	fetchedResult := runWFDirectCellIntegration(t, cell.URL, "run", "result", createdRun.ID)
	var watched, fetched map[string]any
	if err := json.Unmarshal(watchedResult, &watched); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fetchedResult, &fetched); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(watched, fetched) {
		t.Fatalf("watch result = %#v, fetched result = %#v", watched, fetched)
	}
	if watched["message"] != "hi" {
		t.Fatalf("Run result = %#v", watched)
	}
}

func runWFDirectCellIntegration(t *testing.T, cellURL string, args ...string) []byte {
	t.Helper()
	fullArgs := []string{
		"--api-url", cellURL,
		"--workspace", "team",
		"--actor", "integration@example.com",
	}
	fullArgs = append(fullArgs, args...)
	var stdout, stderr bytes.Buffer
	exit := controlcli.RunWF(
		fullArgs,
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != controlcli.ExitOK {
		t.Fatalf("wf %s exit=%d stderr=%s", strings.Join(args, " "), exit, stderr.String())
	}
	return append([]byte(nil), stdout.Bytes()...)
}

func postWorkerIntegration(t *testing.T, endpoint string, body []byte) (int, []byte) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer integration-token")
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, responseBody
}

func runIntegrationGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", dir}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}
