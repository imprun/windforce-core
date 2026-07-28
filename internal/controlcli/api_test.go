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

func TestWFAPIUsesSelectedWorkspaceAndTypedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/w/team/apps" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["limit"] != float64(10) ||
			body["enabled"] != true ||
			body["label"] != "10" {
			t.Fatalf("body = %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	t.Setenv("WF_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var stdout, stderr bytes.Buffer
	exit := RunWF(
		[]string{
			"--api-url", server.URL,
			"--workspace", "team",
			"api", "apps",
			"--field", "limit=10",
			"--field", "enabled=true",
			"--raw-field", "label=10",
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitOK || strings.TrimSpace(stdout.String()) != `{"ok":true}` {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func TestWFAPIAllowsOnlySelectedHost(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	t.Setenv("WF_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	for _, endpoint := range []string{
		"https://attacker.example/api",
		"//attacker.example/api",
		"apps/../secrets",
	} {
		var stdout, stderr bytes.Buffer
		exit := RunWF(
			[]string{
				"--api-url", server.URL,
				"--workspace", "team",
				"api", endpoint,
			},
			strings.NewReader(""),
			&stdout,
			&stderr,
		)
		if exit != ExitUsage {
			t.Fatalf("endpoint %q exit=%d stderr=%q", endpoint, exit, stderr.String())
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("unsafe endpoint sent %d requests", requests.Load())
	}
}

func TestWFAPIAbsolutePathRemainsOnContextHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/healthz" || request.URL.Query().Get("full") != "1" {
			t.Fatalf("URL = %s", request.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ready":true}`))
	}))
	defer server.Close()

	t.Setenv("WF_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var stdout, stderr bytes.Buffer
	exit := RunWF(
		[]string{
			"--api-url", server.URL,
			"--workspace", "team",
			"api", "/healthz?full=1",
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitOK {
		t.Fatalf("exit=%d stderr=%q", exit, stderr.String())
	}
}

func TestWFAPIRejectsRedirectWithoutForwardingCredentials(t *testing.T) {
	var redirectedRequests atomic.Int32
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		redirectedRequests.Add(1)
	}))
	defer attacker.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, attacker.URL+"/capture", http.StatusFound)
	}))
	defer server.Close()

	t.Setenv("WF_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("WF_TOKEN", "sensitive-token")
	var stdout, stderr bytes.Buffer
	exit := RunWF(
		[]string{
			"--api-url", server.URL,
			"--workspace", "team",
			"--actor", "person@example.com",
			"api", "apps",
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitAPIClient || !strings.Contains(stderr.String(), "HTTP 302") {
		t.Fatalf("exit=%d stderr=%q", exit, stderr.String())
	}
	if redirectedRequests.Load() != 0 {
		t.Fatalf("redirect target received %d requests", redirectedRequests.Load())
	}
}
