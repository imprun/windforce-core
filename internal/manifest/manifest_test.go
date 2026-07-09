package manifest

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseFillsActionName(t *testing.T) {
	app, err := Parse([]byte(`{
		"app": "echo",
		"entrypoint": "action.go",
		"scriptLang": "go",
		"actions": {
			"run": {
				"command": ["go", "run", "./action.go"]
			}
		}
	}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if app.Actions["run"].Action != "run" {
		t.Fatalf("action name = %q", app.Actions["run"].Action)
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

func TestParseRejectsCapabilityTagConflicts(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "app tag",
			body: `{"app":"echo","entrypoint":"main.ts","tag":"default","capabilities":["browser"],"actions":{"run":{}}}`,
			want: "declares both tag and capabilities",
		},
		{
			name: "action tag",
			body: `{"app":"echo","entrypoint":"main.ts","capabilities":["browser"],"actions":{"run":{"tag":"fast"}}}`,
			want: "declares both tag and capabilities",
		},
		{
			name: "unsupported",
			body: `{"app":"echo","entrypoint":"main.ts","capabilities":["gpu"],"actions":{"run":{}}}`,
			want: "unsupported capability",
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

func TestParseRejectsMismatchedActionName(t *testing.T) {
	_, err := Parse([]byte(`{
		"app": "echo",
		"entrypoint": "main.ts",
		"actions": {
			"run": { "action": "other" }
		}
	}`))
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestParseRejectsInvalidCanonicalKeys(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "app",
			body: `{"app":"Echo","entrypoint":"main.ts","actions":{"run":{}}}`,
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

func TestParseRejectsNonCanonicalManifestFields(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing app entrypoint",
			body: `{"app":"echo","actions":{"run":{}}}`,
			want: "has no entrypoint",
		},
		{
			name: "app runtime alias",
			body: `{"app":"echo","entrypoint":"main.ts","runtime":"typescript","actions":{"run":{}}}`,
			want: "use scriptLang",
		},
		{
			name: "action entrypoint",
			body: `{"app":"echo","entrypoint":"main.ts","actions":{"run":{"entrypoint":"run.ts"}}}`,
			want: "use app entrypoint",
		},
		{
			name: "action runtime",
			body: `{"app":"echo","entrypoint":"main.ts","actions":{"run":{"runtime":"go"}}}`,
			want: "use app scriptLang",
		},
		{
			name: "millisecond timeout",
			body: `{"app":"echo","entrypoint":"main.ts","actions":{"run":{"timeoutMs":30000}}}`,
			want: "use timeout seconds",
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

func TestParseRejectsEscapingActionPaths(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "app entrypoint",
			body: `{"app":"echo","entrypoint":"../main.ts","actions":{"run":{}}}`,
			want: "app echo entrypoint path",
		},
		{
			name: "input schema",
			body: `{"app":"echo","entrypoint":"main.ts","actions":{"run":{"inputSchema":"schemas/../input.json"}}}`,
			want: "input schema path",
		},
		{
			name: "output schema",
			body: `{"app":"echo","entrypoint":"main.ts","actions":{"run":{"outputSchema":"../output.json"}}}`,
			want: "output schema path",
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
