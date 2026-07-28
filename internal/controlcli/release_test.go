package controlcli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestWFReleaseViewSelectsExactHistoryItem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/w/team/apps/echo/history" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":"release-old","commit_sha":"111","active":false},
			{"id":"release-active","commit_sha":"222","active":true}
		]`))
	}))
	defer server.Close()

	stdout, stderr, exit := runWFReleaseTest(
		t,
		server.URL,
		"release", "view", "echo", "release-active",
	)
	if exit != ExitOK {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if result["id"] != "release-active" || result["active"] != true {
		t.Fatalf("result = %#v", result)
	}
}

func TestWFReleaseActivateRequiresAuditConfirmationAndUsesCanonicalEndpoint(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.Method != http.MethodPost ||
			request.URL.Path != "/api/w/team/apps/echo/releases/release-old/rollback" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["confirm"] != true || body["reason"] != "restore known good version" {
			t.Fatalf("body = %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"app":"echo","active_release_id":"release-old"}`))
	}))
	defer server.Close()

	_, stderr, exit := runWFReleaseTest(
		t,
		server.URL,
		"release", "activate", "echo", "release-old",
		"--reason", "restore known good version",
	)
	if exit == ExitOK || !strings.Contains(stderr, "--yes is required") {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	if requests.Load() != 0 {
		t.Fatalf("request sent before confirmation: %d", requests.Load())
	}

	stdout, stderr, exit := runWFReleaseTest(
		t,
		server.URL,
		"release", "activate", "echo", "release-old",
		"--reason", "restore known good version",
		"--yes",
	)
	if exit != ExitOK {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
	if !strings.Contains(stdout, `"active_release_id":"release-old"`) {
		t.Fatalf("stdout = %s", stdout)
	}
	if requests.Load() != 1 {
		t.Fatalf("request count = %d", requests.Load())
	}
}

func runWFReleaseTest(t *testing.T, serverURL string, args ...string) (string, string, int) {
	t.Helper()
	t.Setenv("WF_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	fullArgs := []string{"--api-url", serverURL, "--workspace", "team", "--actor", "test-user"}
	fullArgs = append(fullArgs, args...)
	var stdout, stderr bytes.Buffer
	exit := RunWF(fullArgs, strings.NewReader(""), &stdout, &stderr)
	return stdout.String(), stderr.String(), exit
}
