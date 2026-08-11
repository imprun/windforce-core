package manifest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseFillsActionName(t *testing.T) {
	app, err := Parse([]byte(`{
		"app": "echo",
		"entrypoint": "main.ts",
		"scriptLang": "typescript",
		"actions": {
			"run": {}
		}
	}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if app.Actions["run"].Action != "run" {
		t.Fatalf("action name = %q", app.Actions["run"].Action)
	}
}

func TestParseAcceptsFCodeAppAndModuleKeys(t *testing.T) {
	app, err := Parse([]byte(`{
		"app": "4MDCPCM",
		"entrypoint": "main.py",
		"scriptLang": "python",
		"actions": {
			"1000": {}
		}
	}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if app.App != "4MDCPCM" {
		t.Fatalf("app key = %q, want 4MDCPCM", app.App)
	}
	if app.Actions["1000"].Action != "1000" {
		t.Fatalf("action key = %q, want 1000", app.Actions["1000"].Action)
	}
}

func TestParseRejectsCommandAdapterManifest(t *testing.T) {
	_, err := Parse([]byte(`{
		"app": "4MDCPCM",
		"name": "Coupang Eats",
		"entrypoint": "src/coupang_eats/app.py",
		"actions": {
			"1000": {
				"runtime": "python",
				"inputSchema": "src/coupang_eats/modules/m1000/input.schema.json",
				"outputSchema": "src/coupang_eats/modules/m1000/output.schema.json",
				"timeoutMs": 120000,
				"adapter": {
					"type": "command",
					"command": ["scraping-windforce-adapter"],
					"options": {"module": "1000"}
				}
			}
		}
	}`))
	if err == nil || !strings.Contains(err.Error(), "adapter is not supported") {
		t.Fatalf("Parse error = %v, want adapter rejection", err)
	}
}

func TestLoadMissingManifestUsesCanonicalMessage(t *testing.T) {
	_, err := Load(t.TempDir(), "")
	if err == nil || err.Error() != "no windforce.json manifest at source root (subpath)" {
		t.Fatalf("Load error = %v", err)
	}
}

func TestLoadWrapsParseErrorWithManifestName(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, FileName), []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(root, "")
	if err == nil || !strings.Contains(err.Error(), "parse windforce.json:") {
		t.Fatalf("Load error = %v, want parse windforce.json prefix", err)
	}
}

func TestLoadValidationErrorsNameOperatorSelectedManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "scraping.json"), []byte(`{
		"app": "echo",
		"entrypoint": "main.ts",
		"actions": {"Not A Key": {}}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(root, "scraping.json")
	if err == nil || !strings.Contains(err.Error(), "in scraping.json") {
		t.Fatalf("Load error = %v, want the operator-selected manifest name", err)
	}
	if strings.Contains(err.Error(), FileName) {
		t.Fatalf("Load error = %v, leaks the default manifest name", err)
	}
}

func TestLoadReadsOperatorSelectedManifestName(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "scraping.json"), []byte(`{
		"app": "echo",
		"entrypoint": "main.ts",
		"actions": {"run": {}}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	app, err := Load(root, "scraping.json")
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	if app.App != "echo" {
		t.Fatalf("app = %q, want echo", app.App)
	}
}

func TestLoadMissingSelectedManifestNamesThatFile(t *testing.T) {
	_, err := Load(t.TempDir(), "scraping.json")
	if err == nil || err.Error() != "no scraping.json manifest at source root (subpath)" {
		t.Fatalf("Load error = %v, want the selected name in the message", err)
	}
}

func TestResolveFileNameFallsBackToDefault(t *testing.T) {
	if got := ResolveFileName("  "); got != FileName {
		t.Fatalf("ResolveFileName(blank) = %q, want %q", got, FileName)
	}
	if got := ResolveFileName(" scraping.json "); got != "scraping.json" {
		t.Fatalf("ResolveFileName trimmed = %q", got)
	}
}

func TestParseAppliesCanonicalAppDefaults(t *testing.T) {
	app, err := Parse([]byte(`{
		"app": "echo",
		"entrypoint": "main.ts",
		"scriptLang": "typescript",
		"timeout": 120,
		"maxConcurrent": 2,
		"actions": {
			"run": {},
			"fast": {"timeout": 45}
		}
	}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if app.MaxConcurrent == nil || *app.MaxConcurrent != 2 {
		t.Fatalf("maxConcurrent = %v, want 2", app.MaxConcurrent)
	}
	run := app.Actions["run"]
	if run.Entrypoint != "main.ts" || run.Runtime != "typescript" || run.TimeoutMs != 120000 {
		t.Fatalf("run defaults = %#v", run)
	}
	fast := app.Actions["fast"]
	if fast.Entrypoint != "main.ts" || fast.Runtime != "typescript" || fast.TimeoutMs != 45000 {
		t.Fatalf("fast overrides = %#v", fast)
	}
}

func TestParseAppliesCanonicalDefaultTimeout(t *testing.T) {
	app, err := Parse([]byte(`{
		"app": "echo",
		"entrypoint": "main.ts",
		"actions": {
			"run": {}
		}
	}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if app.TimeoutS != 300 {
		t.Fatalf("app timeout = %d, want 300", app.TimeoutS)
	}
	if app.Tag != "default" {
		t.Fatalf("app tag = %q, want default", app.Tag)
	}
	run := app.Actions["run"]
	if run.TimeoutMs != 300000 {
		t.Fatalf("run timeout ms = %d, want 300000", run.TimeoutMs)
	}
}

func TestParsePreservesScriptLangForRuntimeDispatch(t *testing.T) {
	for _, test := range []struct {
		name       string
		scriptLang string
	}{
		{name: "typescript", scriptLang: "typescript"},
		{name: "python", scriptLang: "python"},
		{name: "go", scriptLang: "go"},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, err := Parse([]byte(`{
				"app": "echo",
				"entrypoint": "main.go",
				"scriptLang": "` + test.scriptLang + `",
				"actions": {
					"run": {}
				}
			}`))
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			if app.ScriptLang != test.scriptLang || app.Actions["run"].Runtime != test.scriptLang {
				t.Fatalf("scriptLang/runtime = %q/%q, want %q", app.ScriptLang, app.Actions["run"].Runtime, test.scriptLang)
			}
		})
	}
}

func TestParseRejectsUnsupportedScriptLang(t *testing.T) {
	_, err := Parse([]byte(`{
		"app": "echo",
		"entrypoint": "main.rb",
		"scriptLang": "ruby",
		"actions": {"run": {}}
	}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported scriptLang") {
		t.Fatalf("Parse error = %v, want unsupported scriptLang", err)
	}
}

func TestParseDefaultTagDoesNotConflictWithActionCapabilities(t *testing.T) {
	app, err := Parse([]byte(`{
		"app": "echo",
		"entrypoint": "main.ts",
		"actions": {
			"run": {"capabilities": ["browser"]}
		}
	}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if app.Tag != "default" {
		t.Fatalf("app tag = %q, want default", app.Tag)
	}
	caps := app.Actions["run"].Capabilities
	if caps == nil || len(*caps) != 1 || (*caps)[0] != "browser" {
		t.Fatalf("action capabilities = %#v", caps)
	}
}

func TestParsePreservesCapabilities(t *testing.T) {
	app, err := Parse([]byte(`{
		"app": "echo",
		"entrypoint": "main.ts",
		"capabilities": ["browser", "browser"],
		"actions": {
			"run": {},
			"plain": {"capabilities": []}
		}
	}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if !reflect.DeepEqual(app.Capabilities, []string{"browser"}) {
		t.Fatalf("app capabilities = %#v", app.Capabilities)
	}
	if app.Actions["run"].Capabilities != nil {
		t.Fatalf("run capabilities = %#v, want nil inheritance", app.Actions["run"].Capabilities)
	}
	plain := app.Actions["plain"].Capabilities
	if plain == nil || len(*plain) != 0 {
		t.Fatalf("plain capabilities = %#v, want explicit empty override", plain)
	}
}

func TestParseAllowsTagAndLabelsTogether(t *testing.T) {
	data := []byte(`{
		"app": "demo",
		"entrypoint": "main.ts",
		"tag": "blue",
		"capabilities": ["browser"],
		"runsOn": ["kr"],
		"actions": {"run": {}}
	}`)
	app, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if app.Tag != "blue" {
		t.Fatalf("tag = %q, want blue (tags and labels coexist)", app.Tag)
	}
	if want := []string{"browser", "kr"}; !reflect.DeepEqual(app.RunsOn, want) {
		t.Fatalf("runsOn = %#v, want %#v (capabilities merge as alias)", app.RunsOn, want)
	}
}

func TestParseRejectsInvalidRunsOn(t *testing.T) {
	for name, field := range map[string]string{
		"reserved sys prefix": `"runsOn": ["sys/pool.dedicated"]`,
		"invalid label":       `"runsOn": ["Not Valid"]`,
	} {
		data := []byte(`{"app": "demo", "entrypoint": "main.ts", ` + field + `, "actions": {"run": {}}}`)
		if _, err := Parse(data); err == nil {
			t.Fatalf("%s: Parse must reject", name)
		}
	}
}

func TestParseRejectsInvalidMaxConcurrent(t *testing.T) {
	_, err := Parse([]byte(`{
		"app": "echo",
		"entrypoint": "main.ts",
		"maxConcurrent": 0,
		"actions": {"run": {}}
	}`))
	if err == nil || !strings.Contains(err.Error(), "maxConcurrent must be positive") {
		t.Fatalf("Parse error = %v, want maxConcurrent validation", err)
	}
}

func TestParseNormalizesAppAndActionExecutionLimits(t *testing.T) {
	app, err := Parse([]byte(`{
		"app": "echo",
		"entrypoint": "main.ts",
		"executionLimits": {
			"concurrency": [{
				"id": "account",
				"maxConcurrent": 2,
				"inputPointers": ["/account/id"]
			}]
		},
		"actions": {
			"run": {
				"executionLimits": {
					"concurrency": [{
						"id": "egress",
						"maxConcurrent": 1,
						"inputPointers": ["/egress~1name"]
					}]
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got := app.ExecutionLimits.Concurrency; len(got) != 1 || got[0].ID != "account" || got[0].InputPointers[0] != "/account/id" {
		t.Fatalf("app execution limits = %#v", got)
	}
	if got := app.Actions["run"].ExecutionLimits.Concurrency; len(got) != 1 || got[0].ID != "egress" || got[0].InputPointers[0] != "/egress~1name" {
		t.Fatalf("action execution limits = %#v", got)
	}
}

func TestParseRejectsInvalidExecutionLimits(t *testing.T) {
	for name, limit := range map[string]string{
		"missing pointers": `{"id":"account","maxConcurrent":1,"inputPointers":[]}`,
		"invalid pointer":  `{"id":"account","maxConcurrent":1,"inputPointers":["account"]}`,
		"invalid escape":   `{"id":"account","maxConcurrent":1,"inputPointers":["/account~2id"]}`,
		"invalid max":      `{"id":"account","maxConcurrent":0,"inputPointers":["/account"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(`{"app":"echo","entrypoint":"main.ts","executionLimits":{"concurrency":[` + limit + `]},"actions":{"run":{}}}`))
			if err == nil || !strings.Contains(err.Error(), "executionLimits") {
				t.Fatalf("Parse error = %v, want executionLimits validation", err)
			}
		})
	}
}

func TestParseIgnoresActionNameFieldAndUsesMapKey(t *testing.T) {
	app, err := Parse([]byte(`{
		"app": "echo",
		"entrypoint": "main.ts",
		"actions": {
			"run": { "action": "other" }
		}
	}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if app.Actions["run"].Action != "run" {
		t.Fatalf("action name = %q, want map key", app.Actions["run"].Action)
	}
}

func TestParseRejectsUnsafeKeys(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "app",
			body: `{"app":"bad-app","entrypoint":"main.ts","actions":{"run":{}}}`,
			want: "invalid app key",
		},
		{
			name: "action",
			body: `{"app":"echo","entrypoint":"main.ts","actions":{"bad-action":{}}}`,
			want: "invalid action key",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse([]byte(test.body))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Parse error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestParseRejectsMissingEntrypoint(t *testing.T) {
	_, err := Parse([]byte(`{"app":"echo","actions":{"run":{}}}`))
	if err == nil || !strings.Contains(err.Error(), "has no entrypoint") {
		t.Fatalf("Parse error = %v, want missing entrypoint validation", err)
	}
}

func TestParseRejectsActionCommand(t *testing.T) {
	_, err := Parse([]byte(`{
		"app": "echo",
		"entrypoint": "main.py",
		"actions": {
			"run": {
				"command": ["python", "main.py"]
			}
		}
	}`))
	if err == nil || !strings.Contains(err.Error(), "command is not supported") {
		t.Fatalf("Parse error = %v, want command rejection", err)
	}
}

func TestParseRejectsUnsupportedFlows(t *testing.T) {
	_, err := Parse([]byte(`{
		"app": "echo",
		"entrypoint": "main.ts",
		"actions": {
			"run": {}
		},
		"flows": {
			"main": {
				"steps": [
					{"key": "run", "action": "run"}
				]
			}
		}
	}`))
	if err == nil || !strings.Contains(err.Error(), "does not support flows") {
		t.Fatalf("Parse error = %v, want unsupported flows validation", err)
	}
}

func TestParsePreservesExecutionFieldsAndIgnoresRuntimeOwnedFields(t *testing.T) {
	app, err := Parse([]byte(`{
		"app": "echo",
		"entrypoint": "main.ts",
		"runtime": "legacy",
		"scriptLang": "typescript",
		"timeout": 120,
		"actions": {
			"run": {
				"action": "other",
				"entrypoint": "run.ts",
				"timeoutMs": 30000,
				"tagOverride": "operator-owned",
				"inputSchemaBody": {"type": "string"},
				"outputSchemaBody": {"type": "string"},
				"operatorSettingsSchemaBody": {"type": "string"},
				"updatedAt": "2025-01-01T00:00:00Z"
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if app.Runtime != "" {
		t.Fatalf("app runtime = %q, want ignored", app.Runtime)
	}
	run := app.Actions["run"]
	if run.Action != "run" || run.Entrypoint != "run.ts" || run.Runtime != "typescript" || run.TimeoutMs != 30000 {
		t.Fatalf("run execution fields = %#v", run)
	}
	if run.TagOverride != nil || len(run.InputSchemaBody) != 0 || len(run.OutputSchemaBody) != 0 || len(run.OperatorSettingsSchemaBody) != 0 || run.UpdatedAt != nil {
		t.Fatalf("runtime-owned fields leaked = %#v", run)
	}
}

func TestParseRejectsActionRuntime(t *testing.T) {
	_, err := Parse([]byte(`{
		"app":"echo","entrypoint":"main.ts","scriptLang":"typescript",
		"actions":{"run":{"runtime":"python"}}
	}`))
	if err == nil || !strings.Contains(err.Error(), "runtime is not supported") {
		t.Fatalf("Parse error = %v, want action runtime rejection", err)
	}
}

func TestParseRejectsEscapingEntrypoint(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "app entrypoint",
			body: `{"app":"echo","entrypoint":"../main.ts","actions":{"run":{}}}`,
			want: `app echo entrypoint "../main.ts" must be a relative path inside the app`,
		},
		{
			name: "absolute app entrypoint",
			body: `{"app":"echo","entrypoint":"/main.ts","actions":{"run":{}}}`,
			want: `app echo entrypoint "/main.ts" must be a relative path inside the app`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse([]byte(test.body))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Parse error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestParsePreservesSchemaPathsForMaterialization(t *testing.T) {
	app, err := Parse([]byte(`{
		"app": "echo",
		"entrypoint": "main.ts",
		"actions": {
			"run": {
				"inputSchema": "../input.json",
				"outputSchema": "/output.json"
			}
		}
	}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	run := app.Actions["run"]
	if run.InputSchema != "../input.json" || run.OutputSchema != "/output.json" {
		t.Fatalf("schema paths = input:%q output:%q", run.InputSchema, run.OutputSchema)
	}
}

func TestParseNormalizesRuntimeAccess(t *testing.T) {
	app, err := Parse([]byte(`{
  "app": "echo",
  "entrypoint": "main.ts",
  "actions": {
    "run": {
      "runtimeAccess": {
        "variables": [" credentials/token ", "credentials/token"],
        "resources": ["database/main"]
      }
    }
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	action := app.Actions["run"]
	if !reflect.DeepEqual(action.RuntimeAccess.Variables, []string{"credentials/token"}) ||
		!reflect.DeepEqual(action.RuntimeAccess.Resources, []string{"database/main"}) {
		t.Fatalf("runtime access = %#v", action.RuntimeAccess)
	}
}

func TestParseRejectsInvalidRuntimeAccessPath(t *testing.T) {
	_, err := Parse([]byte(`{
  "app": "echo",
  "entrypoint": "main.ts",
  "actions": {
    "run": {"runtimeAccess": {"variables": ["../secret"]}}
  }
}`))
	if err == nil || !strings.Contains(err.Error(), "runtimeAccess") {
		t.Fatalf("Parse error = %v, want runtimeAccess validation", err)
	}
}
