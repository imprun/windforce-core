package controlcli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestWFAppPublishRegistersSynchronizesAndPublishesExactCommit(t *testing.T) {
	repoRoot, appDir, commit := createPublishGitFixture(t)

	var (
		mu       sync.Mutex
		requests []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		mu.Lock()
		requests = append(requests, request.Method+" "+request.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")

		switch request.Method + " " + request.URL.Path {
		case "GET /api/w/team/git_sources":
			_, _ = w.Write([]byte(`[]`))
		case "POST /api/w/team/git_sources":
			var body map[string]string
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			want := map[string]string{
				"name":      "echo",
				"repo_url":  "https://git.example.test/team/apps.git",
				"branch":    "main",
				"subpath":   "apps/echo",
				"creds_ref": "",
			}
			if !reflect.DeepEqual(body, want) {
				t.Fatalf("register body = %#v, want %#v", body, want)
			}
			_, _ = w.Write([]byte(`{
				"id": 12,
				"workspace_id": "team",
				"name": "echo",
				"repo_url": "git@git.example.test:team/apps.git",
				"branch": "main",
				"subpath": "apps/echo",
				"creds_ref": ""
			}`))
		case "POST /api/w/team/git_sources/12/sync":
			var body map[string]string
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["expected_commit"] != commit {
				t.Fatalf("sync expected_commit = %q, want %q", body["expected_commit"], commit)
			}
			_, _ = w.Write([]byte(`{"app":"echo","commit":"` + commit + `"}`))
		case "POST /api/w/team/git_sources/12/deploy":
			var body sourceDeployRequest
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if !body.Confirm || body.ExpectedCommit != commit {
				t.Fatalf("deploy body = %#v", body)
			}
			_, _ = w.Write([]byte(`{
				"app": "echo",
				"commit": "` + commit + `",
				"deployment_id": "release-1",
				"bundle_status": "ready",
				"bundle_digest": "sha256:abc"
			}`))
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Setenv("WF_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var stdout, stderr bytes.Buffer
	exit := RunWF(
		[]string{
			"--api-url", server.URL,
			"--workspace", "team",
			"--actor", "test-user",
			"app", "publish", appDir,
			"--quiet",
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitOK {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("quiet stderr = %q", stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if result["app"] != "echo" ||
		result["commit"] != commit ||
		result["source_id"] != float64(12) ||
		result["registered"] != true ||
		result["workspace"] != "team" ||
		result["manifest"] != "apps/echo/windforce.json" {
		t.Fatalf("publish result = %#v", result)
	}
	if strings.Contains(stdout.String(), repoRoot) {
		t.Fatalf("stdout leaks absolute checkout path: %s", stdout.String())
	}
	wantRequests := []string{
		"GET /api/w/team/git_sources",
		"POST /api/w/team/git_sources",
		"POST /api/w/team/git_sources/12/sync",
		"POST /api/w/team/git_sources/12/deploy",
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(requests, wantRequests) {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
}

func TestInspectPublishTargetRejectsDirtyWorktreeUnlessExplicitlyAllowed(t *testing.T) {
	_, appDir, commit := createPublishGitFixture(t)
	if err := os.WriteFile(filepath.Join(appDir, "main.ts"), []byte("export const changed = true;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(appDir, "windforce.json"),
		[]byte(`{"app":"uncommitted-name","entrypoint":"main.ts","actions":{"run":{}}}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := inspectPublishTarget(appDir, "", "", false); err == nil ||
		!strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("inspect error = %v", err)
	}
	target, err := inspectPublishTarget(appDir, "", "", true)
	if err != nil {
		t.Fatalf("allow dirty inspect: %v", err)
	}
	if !target.Dirty || target.Commit != commit || target.App != "echo" {
		t.Fatalf("target = %#v", target)
	}
}

func TestNormalizeRepoURLMatchesHTTPSAndSSHWithoutAcceptingSecrets(t *testing.T) {
	base := t.TempDir()
	httpsKey, err := normalizeRepoURL("https://github.com/imprun/windforce-core.git", base)
	if err != nil {
		t.Fatal(err)
	}
	sshKey, err := normalizeRepoURL("git@github.com:imprun/windforce-core.git", base)
	if err != nil {
		t.Fatal(err)
	}
	if httpsKey != sshKey {
		t.Fatalf("https key %q != ssh key %q", httpsKey, sshKey)
	}
	if _, err := normalizeRepoURL("https://token@github.com/imprun/windforce-core.git", base); err == nil ||
		!strings.Contains(err.Error(), "embedded credentials") {
		t.Fatalf("credential URL error = %v", err)
	}
	if _, err := normalizeRepoURL("https://github.com/imprun/windforce-core.git?token=secret", base); err == nil {
		t.Fatal("query-bearing repository URL was accepted")
	}
	if _, err := normalizeRepoURL("ssh://git:secret@github.com/imprun/windforce-core.git", base); err == nil ||
		!strings.Contains(err.Error(), "embedded password") {
		t.Fatalf("SSH password URL error = %v", err)
	}
	if _, err := normalizeRepoURL("https://github.com/imprun/windforce..core.git", base); err != nil {
		t.Fatalf("valid double-dot repository name rejected: %v", err)
	}
}

func TestResolvePublishGitSourceFailsClosedOnAmbiguousMatch(t *testing.T) {
	target := publishTarget{
		Branch:   "main",
		RepoRoot: t.TempDir(),
		RepoKey:  "host:github.com/imprun/apps",
		Subpath:  "apps/echo",
	}
	sources := []publishGitSource{
		{ID: 1, Name: "one", RepoURL: "https://github.com/imprun/apps.git", Branch: "main", Subpath: "apps/echo"},
		{ID: 2, Name: "two", RepoURL: "git@github.com:imprun/apps.git", Branch: "main", Subpath: "apps/echo"},
	}
	r := &runner{resolved: resolvedConfig{ProfileName: "default", Profile: Profile{Workspace: "team"}}}
	if _, _, err := r.resolvePublishGitSource(sources, target, 0, "", "", true); err == nil ||
		!strings.Contains(err.Error(), "--source-id") {
		t.Fatalf("ambiguous source error = %v", err)
	}
}

func createPublishGitFixture(t *testing.T) (string, string, string) {
	t.Helper()
	repoRoot := t.TempDir()
	runPublishTestGit(t, repoRoot, "init", "-b", "main")
	runPublishTestGit(t, repoRoot, "config", "user.email", "wf-test@example.com")
	runPublishTestGit(t, repoRoot, "config", "user.name", "WF Test")

	appDir := filepath.Join(repoRoot, "apps", "echo")
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
	if err := os.WriteFile(filepath.Join(appDir, "main.ts"), []byte("export async function main(ctx) { return ctx.input }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runPublishTestGit(t, repoRoot, "add", "-A")
	runPublishTestGit(t, repoRoot, "commit", "-m", "initial app")
	runPublishTestGit(t, repoRoot, "remote", "add", "origin", "https://git.example.test/team/apps.git")
	commit := strings.TrimSpace(runPublishTestGit(t, repoRoot, "rev-parse", "HEAD"))
	return repoRoot, appDir, commit
}

func runPublishTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", dir}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}
