package controlcli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

type terminalBuffer struct {
	bytes.Buffer
}

func (*terminalBuffer) IsTerminal() bool {
	return true
}

func TestWFJSONFieldsAndJQProvideStableMachineOutput(t *testing.T) {
	server := outputTestServer(t)
	defer server.Close()
	t.Setenv("WF_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	exit := RunWF(
		[]string{
			"--api-url", server.URL,
			"--workspace", "team",
			"--json", "app,state",
			"--jq", ".app",
			"run", "show", "run-1",
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitOK || strings.TrimSpace(stdout.String()) != "echo" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func TestWFTemplateFormatsSelectedOutput(t *testing.T) {
	server := outputTestServer(t)
	defer server.Close()
	t.Setenv("WF_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	exit := RunWF(
		[]string{
			"--api-url", server.URL,
			"--workspace", "team",
			"--template", "{{.app}}:{{.state}}",
			"run", "show", "run-1",
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitOK || stdout.String() != "echo:RUNNING\n" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func TestWFJSONRejectsUnknownFieldsWithAvailableNames(t *testing.T) {
	server := outputTestServer(t)
	defer server.Close()
	t.Setenv("WF_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	exit := RunWF(
		[]string{
			"--api-url", server.URL,
			"--workspace", "team",
			"--json", "missing",
			"run", "show", "run-1",
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitUsage ||
		!strings.Contains(stderr.String(), `unknown JSON field "missing"`) ||
		!strings.Contains(stderr.String(), "app") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func TestWFTerminalOutputUsesReadableLabels(t *testing.T) {
	server := outputTestServer(t)
	defer server.Close()
	t.Setenv("WF_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout terminalBuffer
	var stderr bytes.Buffer
	exit := RunWF(
		[]string{
			"--api-url", server.URL,
			"--workspace", "team",
			"run", "show", "run-1",
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitOK {
		t.Fatalf("exit=%d stderr=%q", exit, stderr.String())
	}
	if !strings.Contains(stdout.String(), "RUN ID") ||
		!strings.Contains(stdout.String(), "run-1") ||
		!strings.Contains(stdout.String(), "STATE") {
		t.Fatalf("terminal output = %q", stdout.String())
	}
	if strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") {
		t.Fatalf("terminal output remained raw JSON: %q", stdout.String())
	}
}

func outputTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/workspaces/team/runs/run-1" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"run_id":"run-1",
			"state":"RUNNING",
			"app":"echo",
			"action":"run"
		}`))
	}))
}
