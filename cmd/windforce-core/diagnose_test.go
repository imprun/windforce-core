package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imprun/windforce-core/internal/diagnose"
)

func TestRunDiagnoseJSONIsReadOnlyAndRedacted(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "missing", "state.json")
	storePath := filepath.Join(root, "missing-store")
	sensitive := "redaction-sentinel"
	t.Setenv("DIAGNOSE_ADMIN_TOKEN", sensitive)
	t.Setenv("DIAGNOSE_SECRET_KEY", sensitive)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := runDiagnose([]string{
		"--mode", "server", "--json", "--state", statePath, "--store", storePath,
		"--admin-token-env", "DIAGNOSE_ADMIN_TOKEN", "--secret-key-env", "DIAGNOSE_SECRET_KEY",
	}, &stdout, &stderr)
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; stderr=%q; stdout=%q", exit, stderr.String(), stdout.String())
	}
	var report diagnose.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v; output=%q", err, stdout.String())
	}
	if report.SchemaVersion != diagnose.ReportSchemaVersion || report.Mode != diagnose.ModeServer {
		t.Fatalf("report = %#v", report)
	}
	for _, forbidden := range []string{sensitive, statePath, storePath} {
		if strings.Contains(stdout.String(), forbidden) || strings.Contains(stderr.String(), forbidden) {
			t.Fatalf("diagnose output leaked %q", forbidden)
		}
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("diagnose created local state file, stat err=%v", err)
	}
	if _, err := os.Stat(storePath); !os.IsNotExist(err) {
		t.Fatalf("diagnose created artifact store, stat err=%v", err)
	}
}

func TestRunDiagnoseRejectsInvalidMode(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exit := runDiagnose([]string{"--mode", "cloud"}, &stdout, &stderr); exit != diagnoseUsageExitCode {
		t.Fatalf("exit = %d, want %d", exit, diagnoseUsageExitCode)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "--mode") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
