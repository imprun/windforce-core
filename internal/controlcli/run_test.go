package controlcli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRunJobWaitUsesControlPlaneContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/w/team/jobs/run/demo/health/wait" || r.URL.Query().Get("timeout_ms") != "5000" {
			t.Fatalf("request URL = %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"succeeded"}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	exit := Run([]string{"--api-url", server.URL, "--workspace", "team", "job", "run", "demo", "health", "--wait", "--timeout-ms", "5000", "--input", `{"ping":true}`}, strings.NewReader(""), &stdout, &stderr)
	if exit != ExitOK {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != `{"status":"succeeded"}` {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestRunSourceDeployConfirmsRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/w/team/git_sources/12/deploy" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var body sourceDeployRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if !body.Confirm || body.Message != "Publish validated revision" {
			t.Fatalf("request body = %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":true}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	exit := Run(
		[]string{
			"--api-url", server.URL,
			"--workspace", "team",
			"source", "deploy", "12",
			"--message", "Publish validated revision",
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitOK {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != `{"active":true}` {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestRunRejectsUnexpectedArgumentsBeforeRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	t.Setenv("WINDFORCE_CONFIG", t.TempDir()+"/config.json")

	tests := map[string][]string{
		"profile list":        {"profile", "list", "unexpected"},
		"profile show":        {"profile", "show", "local", "unexpected"},
		"profile set":         {"profile", "set", "local", "--api-url", server.URL, "unexpected"},
		"source list":         {"--api-url", server.URL, "source", "list", "unexpected"},
		"source register":     {"--api-url", server.URL, "source", "register", "--name", "demo", "--repo-url", "https://example.test/repo.git", "unexpected"},
		"source probe":        {"--api-url", server.URL, "source", "probe", "--repo-url", "https://example.test/repo.git", "unexpected"},
		"source deploy":       {"--api-url", server.URL, "source", "deploy", "12", "unexpected"},
		"job run":             {"--api-url", server.URL, "job", "run", "demo", "health", "unexpected"},
		"job list":            {"--api-url", server.URL, "job", "list", "unexpected"},
		"job show":            {"--api-url", server.URL, "job", "show", "job-1", "unexpected"},
		"job result":          {"--api-url", server.URL, "job", "result", "job-1", "unexpected"},
		"job logs":            {"--api-url", server.URL, "job", "logs", "job-1", "unexpected"},
		"job cancel":          {"--api-url", server.URL, "job", "cancel", "job-1", "unexpected"},
		"provisioning export": {"--api-url", server.URL, "provisioning", "export", "unexpected"},
		"provisioning apply":  {"--api-url", server.URL, "provisioning", "apply", "--file", "missing.yaml", "unexpected"},
		"openapi":             {"--api-url", server.URL, "openapi", "unexpected"},
		"version":             {"version", "unexpected"},
		"help":                {"help", "unexpected"},
	}

	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exit := Run(args, strings.NewReader(""), &stdout, &stderr)
			if exit != ExitUsage {
				t.Fatalf("exit=%d stdout=%s stderr=%s", exit, stdout.String(), stderr.String())
			}
		})
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("unexpected API requests = %d", got)
	}
}

func TestRunMapsAPIStatusToStableExitCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	exit := Run([]string{"--api-url", server.URL, "app", "list"}, strings.NewReader(""), &stdout, &stderr)
	if exit != ExitAPIClient || !strings.Contains(stderr.String(), "unauthorized") {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
}

func TestProfileSetDoesNotPersistTokenValue(t *testing.T) {
	path := t.TempDir() + "/config.json"
	t.Setenv("WINDFORCE_CONFIG", path)
	t.Setenv("PRIVATE_WF_TOKEN", "must-not-be-written")
	var stdout, stderr bytes.Buffer
	exit := Run([]string{"profile", "set", "local", "--api-url", "http://127.0.0.1:18091", "--token-env", "PRIVATE_WF_TOKEN", "--use"}, strings.NewReader(""), &stdout, &stderr)
	if exit != ExitOK {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	data, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if data.Profiles["local"].TokenEnv != "PRIVATE_WF_TOKEN" || strings.Contains(stdout.String(), "must-not-be-written") {
		t.Fatalf("profile output leaked or lost token env: %s", stdout.String())
	}
}

func TestGlobalHelpIsSuccessful(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := Run([]string{"--help"}, strings.NewReader(""), &stdout, &stderr)
	if exit != ExitOK || !strings.Contains(stdout.String(), "source list") || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", exit, stdout.String(), stderr.String())
	}
}
