package controlcli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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

func TestWFAppPublishAgainstRealCellHandler(t *testing.T) {
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
	cell := httptest.NewServer(server.New(server.Config{
		Store:            state.NewLocalStore(filepath.Join(tempDir, "state.json")),
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
	if published["app"] != "echo" ||
		published["commit"] != commit ||
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
		Commit string `json:"commit_sha"`
		Active bool   `json:"active"`
	}
	if err := json.NewDecoder(response.Body).Decode(&history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Commit != commit || !history[0].Active {
		t.Fatalf("history = %#v", history)
	}
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
