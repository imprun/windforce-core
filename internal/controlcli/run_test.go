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

func TestRunWaitUsesCanonicalInvocationSpecification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workspaces/team/runs/wait" || r.URL.Query().Get("timeout") != "5s" {
			t.Fatalf("request URL = %s", r.URL.String())
		}
		if r.Header.Get("Idempotency-Key") != "request-1" {
			t.Fatalf("idempotency header = %q", r.Header.Get("Idempotency-Key"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["app"] != "demo" || body["action"] != "health" || body["correlation_id"] != "trace-1" {
			t.Fatalf("request body = %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	exit := Run([]string{"--api-url", server.URL, "--workspace", "team", "run", "wait", "demo", "health", "--timeout", "5s", "--idempotency-key", "request-1", "--correlation-id", "trace-1", "--input", `{"ping":true}`}, strings.NewReader(""), &stdout, &stderr)
	if exit != ExitOK {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != `{"ok":true}` {
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
		"run create":          {"--api-url", server.URL, "run", "create", "demo", "health", "unexpected"},
		"run show":            {"--api-url", server.URL, "run", "show", "run-1", "unexpected"},
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

func TestRunWFUsesContextVocabularyAndSeparateConfig(t *testing.T) {
	path := t.TempDir() + "/config.json"
	t.Setenv("WF_CONFIG", path)
	var stdout, stderr bytes.Buffer
	exit := RunWF(
		[]string{"context", "set", "local", "--api-url", "http://127.0.0.1:18091", "--workspace", "team", "--use"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitOK {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	config, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.CurrentProfile != "local" || config.Profiles["local"].Workspace != "team" {
		t.Fatalf("config = %#v", config)
	}
	if config.Profiles["local"].TokenEnv != "" {
		t.Fatalf("new context unexpectedly stores token env %q", config.Profiles["local"].TokenEnv)
	}

	stdout.Reset()
	stderr.Reset()
	exit = RunWF([]string{"--help"}, strings.NewReader(""), &stdout, &stderr)
	if exit != ExitOK || !strings.Contains(stdout.String(), "usage: wf ") ||
		!strings.Contains(stdout.String(), "context list") ||
		strings.Contains(stdout.String(), "usage: windforce ") {
		t.Fatalf("exit=%d stdout=%s stderr=%s", exit, stdout.String(), stderr.String())
	}
}

func TestRunWFUsesProcessTokenWithoutPersistingIt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer process-secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"apps":[]}`))
	}))
	defer server.Close()

	path := t.TempDir() + "/config.json"
	t.Setenv("WF_CONFIG", path)
	t.Setenv("WF_TOKEN", "process-secret")
	var stdout, stderr bytes.Buffer
	exit := RunWF(
		[]string{"--api-url", server.URL, "app", "list"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitOK {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	if _, err := loadConfig(path); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "process-secret") || strings.Contains(stderr.String(), "process-secret") {
		t.Fatalf("token leaked: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestRunWFSourcePublishUsesReleasePublicationEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/w/team/git_sources/12/deploy" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":true}`))
	}))
	defer server.Close()

	t.Setenv("WF_CONFIG", t.TempDir()+"/config.json")
	var stdout, stderr bytes.Buffer
	exit := RunWF(
		[]string{"--api-url", server.URL, "--workspace", "team", "source", "publish", "12"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitOK {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
}
