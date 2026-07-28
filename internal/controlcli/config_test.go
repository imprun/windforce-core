package controlcli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigStoresTokenEnvironmentNameOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	config := ConfigFile{CurrentProfile: "local", Profiles: map[string]Profile{
		"local": {APIURL: "http://127.0.0.1:18091", Workspace: "default", TokenEnv: "WF_TEST_TOKEN"},
	}}
	if err := saveConfig(path, config); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Profiles["local"].TokenEnv; got != "WF_TEST_TOKEN" {
		t.Fatalf("token env = %q", got)
	}
}

func TestResolveProfileUsesExplicitOverrides(t *testing.T) {
	t.Setenv("WF_TEST_TOKEN", "secret")
	config := ConfigFile{CurrentProfile: "local", Profiles: map[string]Profile{
		"local": {APIURL: "https://profile.example", Workspace: "profile", TokenEnv: "WF_TEST_TOKEN"},
	}}
	resolved, err := resolveProfile(config, "", Profile{Workspace: "override"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Workspace != "override" || resolved.APIURL != "https://profile.example" || resolved.Token != "secret" {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestResolveWFContextUsesWFOverridesFirst(t *testing.T) {
	t.Setenv("WF_CONTEXT", "hosted")
	t.Setenv("WF_API_URL", "https://cell.example")
	t.Setenv("WF_WORKSPACE", "team")
	t.Setenv("WF_TOKEN", "process-secret")
	t.Setenv("WINDFORCE_CORE_API_URL", "https://legacy.example")
	config := ConfigFile{CurrentProfile: "legacy", Profiles: map[string]Profile{
		"hosted": {APIURL: "https://profile.example", Workspace: "profile"},
		"legacy": {APIURL: "https://other.example", Workspace: "other"},
	}}
	resolved, err := resolveProfileFor(wfProgram, config, "", Profile{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ProfileName != "hosted" || resolved.APIURL != "https://cell.example" ||
		resolved.Workspace != "team" || resolved.Token != "process-secret" {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestNormalizeAPIBaseURLRejectsUnsafeOrAmbiguousTargets(t *testing.T) {
	for _, raw := range []string{
		"not-a-url",
		"ftp://cell.example.test",
		"https://operator:secret@cell.example.test",
		"https://cell.example.test/path?token=secret",
		"https://cell.example.test/path#fragment",
		"https://cell.example.test/%zz",
		"http://192.0.2.10:18091",
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := normalizeAPIBaseURL(raw)
			if err == nil {
				t.Fatalf("normalizeAPIBaseURL(%q) succeeded", raw)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("unsafe URL value leaked in error: %q", err)
			}
		})
	}

	normalized, err := normalizeAPIBaseURL("HTTPS://Cloud.Example.Test/t/acme/")
	if err != nil {
		t.Fatal(err)
	}
	if normalized != "https://Cloud.Example.Test/t/acme" {
		t.Fatalf("normalized URL = %q", normalized)
	}
	normalized, err = normalizeAPIBaseURL("http://127.0.0.1:18091/")
	if err != nil || normalized != "http://127.0.0.1:18091" {
		t.Fatalf("loopback URL = %q, %v", normalized, err)
	}
}
