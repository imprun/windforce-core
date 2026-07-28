package controlcli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestWFRunWatchPrintsSuccessfulResult(t *testing.T) {
	var statusRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1/workspaces/team/runs/run-1":
			state := "QUEUED"
			if statusRequests.Add(1) > 1 {
				state = "SUCCEEDED"
			}
			_, _ = w.Write([]byte(`{"run_id":"run-1","state":"` + state + `","app":"echo","action":"run"}`))
		case "/api/v1/workspaces/team/runs/run-1/result":
			_, _ = w.Write([]byte(`{"message":"done"}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Setenv("WF_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var stdout, stderr bytes.Buffer
	exit := RunWF(
		[]string{
			"--api-url", server.URL,
			"--workspace", "team",
			"run", "watch", "run-1",
			"--interval", "100ms",
			"--timeout", "2s",
			"--result",
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitOK {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != `{"message":"done"}` {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "queued") || !strings.Contains(stderr.String(), "succeeded") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestWFRunWatchRejectsUnsafePollingIntervalBeforeRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	t.Setenv("WF_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var stdout, stderr bytes.Buffer
	exit := RunWF(
		[]string{
			"--api-url", server.URL,
			"--workspace", "team",
			"run", "watch", "run-1",
			"--interval", "1ms",
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitUsage || !strings.Contains(stderr.String(), "at least 100ms") {
		t.Fatalf("exit=%d stderr=%q", exit, stderr.String())
	}
	if requests.Load() != 0 {
		t.Fatalf("requests = %d", requests.Load())
	}
}

func TestWFRunWatchReturnsCommandFailureForFailedRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/workspaces/team/runs/run-failed" {
			t.Fatalf("path = %s", request.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"run-failed","state":"FAILED","app":"echo","action":"run"}`))
	}))
	defer server.Close()

	t.Setenv("WF_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var stdout, stderr bytes.Buffer
	exit := RunWF(
		[]string{
			"--api-url", server.URL,
			"--workspace", "team",
			"run", "watch", "run-failed",
			"--timeout", "2s",
			"--quiet",
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitFailure ||
		!strings.Contains(stdout.String(), `"state":"FAILED"`) ||
		!strings.Contains(stderr.String(), "finished with state FAILED") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}
