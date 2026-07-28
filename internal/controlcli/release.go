package controlcli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func (r *runner) release(args []string) error {
	if len(args) == 0 {
		return usageError{"release requires list, view, activate, or rollback"}
	}
	switch args[0] {
	case "list":
		if len(args) != 2 {
			return usageError{fmt.Sprintf("usage: %s release list <app>", r.program)}
		}
		return r.json(http.MethodGet, r.client.WorkspacePath("apps", args[1], "history"), nil)
	case "view":
		if len(args) != 3 {
			return usageError{fmt.Sprintf("usage: %s release view <app> <release-id>", r.program)}
		}
		release, err := r.findRelease(args[1], args[2])
		if err != nil {
			return err
		}
		return r.outputJSON(release)
	case "activate", "rollback":
		if len(args) < 3 {
			return usageError{fmt.Sprintf("usage: %s release %s <app> <release-id> --reason <text> --yes", r.program, args[0])}
		}
		fs := r.flags("release " + args[0])
		reason := fs.String("reason", "", "audit reason for changing the active release")
		yes := fs.Bool("yes", false, "confirm the active release change")
		if err := fs.Parse(args[3:]); err != nil {
			return usageError{err.Error()}
		}
		if fs.NArg() != 0 {
			return usageError{fmt.Sprintf("usage: %s release %s <app> <release-id> --reason <text> --yes", r.program, args[0])}
		}
		if strings.TrimSpace(*reason) == "" {
			return usageError{"--reason is required when changing the active release"}
		}
		if !*yes {
			return usageError{"--yes is required to confirm the active release change"}
		}
		return r.json(
			http.MethodPost,
			r.client.WorkspacePath("apps", args[1], "releases", args[2], "rollback"),
			map[string]any{
				"confirm": true,
				"reason":  strings.TrimSpace(*reason),
			},
		)
	default:
		return usageError{fmt.Sprintf("unknown release command %q", args[0])}
	}
}

func (r *runner) findRelease(app string, releaseID string) (json.RawMessage, error) {
	raw, err := r.client.DoJSON(
		context.Background(),
		http.MethodGet,
		r.client.WorkspacePath("apps", app, "history"),
		nil,
	)
	if err != nil {
		return nil, err
	}
	var history []json.RawMessage
	if err := json.Unmarshal(raw, &history); err != nil {
		return nil, fmt.Errorf("decode release history: %w", err)
	}
	for _, item := range history {
		var identity struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(item, &identity); err != nil {
			return nil, fmt.Errorf("decode release history item: %w", err)
		}
		if identity.ID == releaseID {
			return item, nil
		}
	}
	return nil, fmt.Errorf("release %q was not found for app %q", releaseID, app)
}
