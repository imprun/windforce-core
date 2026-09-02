package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/state"
)

func TestRunRotateSecretKeyUsesEnvironmentOnlyAndDefaultsToDryRun(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	snapshot := state.Snapshot{
		Workspaces: map[string]state.Workspace{
			contract.DefaultWorkspace: {ID: contract.DefaultWorkspace, Name: "Default", Status: state.WorkspaceActive},
		},
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	currentSecret := "cli-current-secret-value"
	previousSecret := "cli-previous-secret-value"
	t.Setenv("ROTATION_CURRENT_TEST", currentSecret)
	t.Setenv("ROTATION_PREVIOUS_TEST", previousSecret)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runRotateSecretKey([]string{
		"--state", statePath,
		"--current-secret-env", "ROTATION_CURRENT_TEST",
		"--previous-secret-env", "ROTATION_PREVIOUS_TEST",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("dry-run exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	var report state.SecretKeyRotationReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Mode != "dry-run" || !report.Changed || report.Applied || report.LegacyWorkspacesMigrated != 1 {
		t.Fatalf("unexpected dry-run report: %+v", report)
	}
	if strings.Contains(stdout.String(), currentSecret) || strings.Contains(stdout.String(), previousSecret) ||
		strings.Contains(stderr.String(), currentSecret) || strings.Contains(stderr.String(), previousSecret) {
		t.Fatal("rotation output contains key material")
	}
	afterDryRun, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterDryRun) != string(before) {
		t.Fatal("CLI dry-run changed local state")
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = runRotateSecretKey([]string{
		"--state", statePath,
		"--current-secret-env", "ROTATION_CURRENT_TEST",
		"--previous-secret-env", "ROTATION_PREVIOUS_TEST",
		"--apply",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("apply exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if strings.Contains(stdout.String(), currentSecret) || strings.Contains(stdout.String(), previousSecret) ||
		strings.Contains(stderr.String(), currentSecret) || strings.Contains(stderr.String(), previousSecret) {
		t.Fatal("rotation output contains key material")
	}
	if afterApply, err := os.ReadFile(statePath); err != nil || string(afterApply) == string(before) {
		t.Fatal("CLI apply did not change local state")
	}
}

func TestRunRotateSecretKeyRejectsDirectSecretFlagsAndNonLocalBackend(t *testing.T) {
	for _, args := range [][]string{
		{"--current-secret", "must-not-be-accepted"},
		{"--state-backend", "postgres"},
	} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if exitCode := runRotateSecretKey(args, &stdout, &stderr); exitCode != 2 {
			t.Fatalf("exit code = %d for args %v", exitCode, args[:1])
		}
		if strings.Contains(stdout.String(), "must-not-be-accepted") || strings.Contains(stderr.String(), "must-not-be-accepted") {
			t.Fatal("rejected CLI input echoed key material")
		}
	}
}

func TestRunRotateSecretKeySanitizesStateErrors(t *testing.T) {
	const sentinel = "sensitive-record-identifier"
	statePath := filepath.Join(t.TempDir(), sentinel)
	if err := os.Mkdir(statePath, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ROTATE_CURRENT_SECRET", "current-secret-for-sanitized-error")
	t.Setenv("ROTATE_PREVIOUS_SECRET", "previous-secret-for-sanitized-error")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runRotateSecretKey([]string{
		"--state", statePath,
		"--current-secret-env", "ROTATE_CURRENT_SECRET",
		"--previous-secret-env", "ROTATE_PREVIOUS_SECRET",
	}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if strings.Contains(stdout.String(), sentinel) || strings.Contains(stderr.String(), sentinel) {
		t.Fatal("rotation error output exposed a state identifier")
	}
}
