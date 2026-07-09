package manifest

import (
	"strings"
	"testing"
)

func TestParseFillsActionName(t *testing.T) {
	app, err := Parse([]byte(`{
		"app": "echo",
		"actions": {
			"run": {
				"runtime": "go",
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
		"actions": {
			"run": {},
			"fast": {"entrypoint": "fast.ts", "runtime": "go", "timeout": 45}
		}
	}`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	run := app.Actions["run"]
	if run.Entrypoint != "main.ts" || run.Runtime != "typescript" || run.TimeoutMs != 120000 {
		t.Fatalf("run defaults = %#v", run)
	}
	fast := app.Actions["fast"]
	if fast.Entrypoint != "fast.ts" || fast.Runtime != "go" || fast.TimeoutMs != 45000 {
		t.Fatalf("fast overrides = %#v", fast)
	}
}

func TestParseRejectsMismatchedActionName(t *testing.T) {
	_, err := Parse([]byte(`{
		"app": "echo",
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
			body: `{"app":"Echo","actions":{"run":{}}}`,
			want: "invalid app key",
		},
		{
			name: "action",
			body: `{"app":"echo","actions":{"bad-action":{}}}`,
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

func TestParseRejectsEscapingActionPaths(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "entrypoint",
			body: `{"app":"echo","actions":{"run":{"entrypoint":"../run.js"}}}`,
			want: "entrypoint path",
		},
		{
			name: "app entrypoint",
			body: `{"app":"echo","entrypoint":"../main.ts","actions":{"run":{}}}`,
			want: "app echo entrypoint path",
		},
		{
			name: "input schema",
			body: `{"app":"echo","actions":{"run":{"inputSchema":"schemas/../input.json"}}}`,
			want: "input schema path",
		},
		{
			name: "output schema",
			body: `{"app":"echo","actions":{"run":{"outputSchema":"../output.json"}}}`,
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
