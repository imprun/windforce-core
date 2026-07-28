package controlcli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestWFWorkspaceUseVerifiesBeforePersisting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet ||
			request.URL.Path != "/api/w/new-team/apps" ||
			request.URL.Query().Get("summary") != "1" {
			t.Fatalf("request = %s %s", request.Method, request.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	configPath := writeWorkspaceTestConfig(t, server.URL)
	var stdout, stderr bytes.Buffer
	exit := RunWF(
		[]string{"workspace", "use", "new-team"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitOK {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	updated, err := loadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Profiles["local"].Workspace != "new-team" {
		t.Fatalf("profile = %#v", updated.Profiles["local"])
	}
	if !strings.Contains(stdout.String(), `"previous_workspace":"old-team"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestWFWorkspaceUsePreservesContextWhenVerificationFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"workspace denied"}`))
	}))
	defer server.Close()

	configPath := writeWorkspaceTestConfig(t, server.URL)
	var stdout, stderr bytes.Buffer
	exit := RunWF(
		[]string{"workspace", "use", "new-team"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitForbidden || !strings.Contains(stderr.String(), "workspace denied") {
		t.Fatalf("exit=%d stderr=%q", exit, stderr.String())
	}
	updated, err := loadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Profiles["local"].Workspace != "old-team" {
		t.Fatalf("context mutated: %#v", updated.Profiles["local"])
	}
}

func TestWFWorkspaceListUsesGlobalRegistryEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/workspaces" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"old-team"}]`))
	}))
	defer server.Close()

	writeWorkspaceTestConfig(t, server.URL)
	var stdout, stderr bytes.Buffer
	exit := RunWF(
		[]string{"workspace", "list"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitOK || !strings.Contains(stdout.String(), `"id":"old-team"`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func writeWorkspaceTestConfig(t *testing.T, serverURL string) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("WF_CONFIG", configPath)
	if err := saveConfig(configPath, ConfigFile{
		CurrentProfile: "local",
		Profiles: map[string]Profile{
			"local": {
				APIURL:    serverURL,
				Workspace: "old-team",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return configPath
}
