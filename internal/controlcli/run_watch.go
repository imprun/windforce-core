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
		state := strings.ToLower(strings.TrimSpace(run.State))
		if state != previous {
			r.publishProgress(quiet, "Run %s: %s", run.RunID, state)
			previous = state
		}
		if terminalRunState(state) {
			if resultOnly && state == "succeeded" {
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
			if state != "succeeded" {
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
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "succeeded", "failed", "canceled", "expired":
		return true
	default:
		return false
	}
}
