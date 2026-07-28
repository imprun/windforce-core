package controlcli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestWFAuthSwitchSelectsExistingAccountAfterVerification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet ||
			request.URL.Path != "/api/w/team/apps" ||
			request.URL.Query().Get("summary") != "1" {
			t.Fatalf("request = %s %s", request.Method, request.URL.String())
		}
		if request.Header.Get("Authorization") != "Bearer second-token" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("WF_CONFIG", configPath)
	config := ConfigFile{
		CurrentProfile: "hosted",
		Profiles: map[string]Profile{
			"hosted": {
				APIURL:    server.URL,
				Workspace: "team",
				Account:   "first",
				AuthType:  authTypeToken,
			},
		},
	}
	if err := saveConfig(configPath, config); err != nil {
		t.Fatal(err)
	}
	key, err := credentialKey(Profile{APIURL: server.URL, Account: "second"})
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryCredentialStore{values: map[string]string{key: "second-token"}}

	var stdout, stderr bytes.Buffer
	exit := RunWFWithCredentialStore(
		[]string{"auth", "switch", "second"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		store,
	)
	if exit != ExitOK {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	updated, err := loadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Profiles["hosted"].Account != "second" {
		t.Fatalf("profile = %#v", updated.Profiles["hosted"])
	}
	if !strings.Contains(stdout.String(), `"account":"second"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestWFAuthSwitchDoesNotMutateContextWhenAccountIsMissing(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("WF_CONFIG", configPath)
	config := ConfigFile{
		CurrentProfile: "hosted",
		Profiles: map[string]Profile{
			"hosted": {
				APIURL:    "https://cell.example.test",
				Workspace: "team",
				Account:   "first",
				AuthType:  authTypeToken,
			},
		},
	}
	if err := saveConfig(configPath, config); err != nil {
		t.Fatal(err)
	}
	store := &memoryCredentialStore{values: map[string]string{}}

	var stdout, stderr bytes.Buffer
	exit := RunWFWithCredentialStore(
		[]string{"auth", "switch", "missing"},
		strings.NewReader(""),
		&stdout,
		&stderr,
		store,
	)
	if exit == ExitOK || !strings.Contains(stderr.String(), "no stored credential") {
		t.Fatalf("exit=%d stderr=%q", exit, stderr.String())
	}
	updated, err := loadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Profiles["hosted"].Account != "first" {
		t.Fatalf("context mutated: %#v", updated.Profiles["hosted"])
	}
}
