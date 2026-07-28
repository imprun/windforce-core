package controlcli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type watchedRun struct {
	RunID  string `json:"run_id"`
	State  string `json:"state"`
	App    string `json:"app"`
	Action string `json:"action"`
}

func (r *runner) watchRun(runID string, interval time.Duration, timeout time.Duration, resultOnly bool, quiet bool) error {
	if interval < 100*time.Millisecond {
		return usageError{"--interval must be at least 100ms"}
	}
	if timeout <= 0 {
		return usageError{"--timeout must be greater than zero"}
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return usageError{"run ID is required"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var previous string
	for {
		raw, err := r.client.DoJSON(
			ctx,
			http.MethodGet,
			r.client.InvocationPath("runs", runID),
			nil,
		)
		if err != nil {
			return err
		}
		var run watchedRun
		if err := json.Unmarshal(raw, &run); err != nil {
			return fmt.Errorf("decode Run status: %w", err)
		}
		if run.RunID == "" || run.State == "" {
			return fmt.Errorf("Cell returned an incomplete Run status")
		}
		if run.State != previous {
			r.publishProgress(quiet, "Run %s: %s", run.RunID, strings.ToLower(string(run.State)))
			previous = run.State
		}
		if terminalRunState(run.State) {
			if resultOnly && run.State == "SUCCEEDED" {
				result, err := r.client.DoJSON(
					ctx,
					http.MethodGet,
					r.client.InvocationPath("runs", runID, "result"),
					nil,
				)
				if err != nil {
					return err
				}
				return r.outputJSON(result)
			}
			if err := r.outputJSON(raw); err != nil {
				return err
			}
			if run.State != "SUCCEEDED" {
				return commandFailure{message: fmt.Sprintf("Run %s finished with state %s", run.RunID, run.State)}
			}
			return nil
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("timed out waiting for Run %s after %s", runID, timeout)
		case <-timer.C:
		}
	}
}

func terminalRunState(value string) bool {
	switch value {
	case "SUCCEEDED", "FAILED", "CANCELED", "EXPIRED":
		return true
	default:
		return false
	}
}
