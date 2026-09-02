package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/imprun/windforce-core/internal/state"
)

func runRotateSecretKey(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("rotate-secret-key", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	stateBackend := flags.String("state-backend", "local", "state backend to rotate; only local is supported")
	statePath := flags.String("state", defaultStatePath(), "local state file path")
	currentSecretEnv := flags.String("current-secret-env", "SECRET_KEY", "environment variable containing the target current secret key")
	previousSecretEnv := flags.String("previous-secret-env", "SECRET_KEY_PREVIOUS", "environment variable containing the source previous secret key")
	apply := flags.Bool("apply", false, "apply the atomic migration; omission performs a dry-run")
	jsonOutput := flags.Bool("json", false, "write the count-only report as JSON")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintln(stderr, "rotate-secret-key: invalid flags")
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "rotate-secret-key does not accept positional arguments")
		return 2
	}
	if strings.TrimSpace(*stateBackend) != "local" {
		fmt.Fprintln(stderr, "rotate-secret-key currently supports only the local state backend")
		return 2
	}
	if strings.TrimSpace(*currentSecretEnv) == "" || strings.TrimSpace(*previousSecretEnv) == "" {
		fmt.Fprintln(stderr, "current-secret-env and previous-secret-env are required")
		return 2
	}

	store := state.NewLocalStore(strings.TrimSpace(*statePath))
	report, err := store.RotateSecretKey(
		context.Background(),
		tokenFromEnv(strings.TrimSpace(*currentSecretEnv)),
		tokenFromEnv(strings.TrimSpace(*previousSecretEnv)),
		*apply,
		"operator:secret-key-rotation",
	)
	if outputErr := writeSecretKeyRotationReport(stdout, report, *jsonOutput); outputErr != nil {
		fmt.Fprintln(stderr, "write secret-key rotation report")
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, secretKeyRotationFailure(err))
		return 1
	}
	return 0
}

func secretKeyRotationFailure(err error) string {
	switch {
	case errors.Is(err, state.ErrSecretKeyRotationBlocked):
		return "rotate-secret-key: rotation is blocked; inspect the count-only blocker report"
	case errors.Is(err, state.ErrSecretKeyRotationUnreadable):
		return "rotate-secret-key: encrypted local state is not readable by the configured rotation keys"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "rotate-secret-key: operation interrupted"
	default:
		return "rotate-secret-key: rotation failed; inspect the local state offline"
	}
}

func writeSecretKeyRotationReport(output io.Writer, report state.SecretKeyRotationReport, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	_, err := fmt.Fprintf(output,
		"mode=%s changed=%t applied=%t workspaces=%d rewrapped=%d migrated=%d already_current=%d verified_records=%d rewritten_records=%d blockers_queued=%d blockers_running=%d blockers_human_tasks=%d blockers_rate_buckets=%d blockers_legacy_secret_variables=%d blockers_webhook_deliveries=%d blockers_trigger_completions=%d\n",
		report.Mode,
		report.Changed,
		report.Applied,
		report.WorkspacesScanned,
		report.WorkspaceKeysRewrapped,
		report.LegacyWorkspacesMigrated,
		report.WorkspacesAlreadyCurrent,
		report.EncryptedRecordsVerified,
		report.EncryptedRecordsRewritten,
		report.Blockers.QueuedJobs,
		report.Blockers.RunningJobs,
		report.Blockers.PendingHumanTasks,
		report.Blockers.UnexpiredRateBuckets,
		report.Blockers.LegacySecretVariables,
		report.Blockers.ActiveWebhookDeliveries,
		report.Blockers.ActiveTriggerCompletions,
	)
	return err
}
