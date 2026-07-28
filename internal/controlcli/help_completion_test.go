package controlcli

import (
	"bytes"
	"strings"
	"testing"
)

func TestWFCommandHelpDoesNotRequireConfigurationOrAuthentication(t *testing.T) {
	t.Setenv("WF_CONFIG", t.TempDir())
	var stdout, stderr bytes.Buffer
	exit := RunWF(
		[]string{"app", "publish", "--help"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitOK {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	if !strings.Contains(stdout.String(), "wf app publish [path]") ||
		!strings.Contains(stdout.String(), "--allow-dirty") ||
		!strings.Contains(stdout.String(), "exact-commit") {
		t.Fatalf("help = %s", stdout.String())
	}
}

func TestWFVersionDoesNotRequireConfigurationOrAuthentication(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("WF_CONFIG", t.TempDir())
			var stdout, stderr bytes.Buffer
			exit := RunWF(args, strings.NewReader(""), &stdout, &stderr)
			if exit != ExitOK || strings.TrimSpace(stdout.String()) != Version {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
			}
		})
	}
}

func TestWFCompletionDoesNotRequireConfigurationOrAuthentication(t *testing.T) {
	t.Setenv("WF_CONFIG", t.TempDir())
	for _, test := range []struct {
		shell string
		want  string
	}{
		{shell: "bash", want: "complete -F _wf_complete wf"},
		{shell: "zsh", want: "#compdef wf"},
		{shell: "fish", want: "complete -c wf"},
		{shell: "powershell", want: "Register-ArgumentCompleter"},
	} {
		t.Run(test.shell, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exit := RunWF(
				[]string{"completion", test.shell},
				strings.NewReader(""),
				&stdout,
				&stderr,
			)
			if exit != ExitOK || !strings.Contains(stdout.String(), test.want) {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
			}
		})
	}
}

func TestWFUnknownHelpTopicReturnsUsageFailureWithoutConfiguration(t *testing.T) {
	t.Setenv("WF_CONFIG", t.TempDir())
	var stdout, stderr bytes.Buffer
	exit := RunWF(
		[]string{"help", "does-not-exist"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if exit != ExitUsage || !strings.Contains(stderr.String(), "unknown help topic") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}
