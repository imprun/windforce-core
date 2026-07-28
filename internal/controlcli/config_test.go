package controlcli

import (
	"path/filepath"
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
