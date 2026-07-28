package controlcli

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
)

func (r *runner) workspace(args []string) error {
	if len(args) == 0 {
		return usageError{"workspace requires list, show, view, or use"}
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return usageError{"usage: " + r.program + " workspace list"}
		}
		return r.json(http.MethodGet, "/api/workspaces", nil)
	case "show":
		if len(args) != 1 {
			return usageError{"usage: " + r.program + " workspace show"}
		}
		return r.outputJSON(map[string]any{
			"context":   r.resolved.ProfileName,
			"workspace": r.resolved.Workspace,
			"api_url":   r.resolved.APIURL,
		})
	case "view":
		if len(args) != 2 {
			return usageError{"usage: " + r.program + " workspace view <workspace>"}
		}
		workspaceID, err := validWorkspaceArgument(args[1])
		if err != nil {
			return err
		}
		return r.json(http.MethodGet, "/api/workspaces/"+workspaceID, nil)
	case "use":
		if len(args) != 2 {
			return usageError{"usage: " + r.program + " workspace use <workspace>"}
		}
		return r.useWorkspace(args[1])
	default:
		return usageError{fmt.Sprintf("unknown workspace command %q", args[0])}
	}
}

func (r *runner) useWorkspace(value string) error {
	workspaceID, err := validWorkspaceArgument(value)
	if err != nil {
		return err
	}
	if r.resolved.ProfileName == "" {
		return fmt.Errorf("select or create a context before changing its workspace")
	}
	client := *r.client
	client.Workspace = workspaceID
	if _, err := client.DoJSON(
		context.Background(),
		http.MethodGet,
		client.WorkspacePath("apps")+"?summary=1",
		nil,
	); err != nil {
		return fmt.Errorf("verify workspace %q: %w", workspaceID, err)
	}

	profile := r.config.Profiles[r.resolved.ProfileName]
	previous := profile.Workspace
	profile.Workspace = workspaceID
	r.config.Profiles[r.resolved.ProfileName] = profile
	if err := saveConfig(r.configPath, r.config); err != nil {
		return err
	}
	return r.outputJSON(map[string]any{
		"context":            r.resolved.ProfileName,
		"workspace":          workspaceID,
		"previous_workspace": previous,
		"api_url":            r.resolved.APIURL,
	})
}

func validWorkspaceArgument(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !contract.ValidWorkspaceID(value) {
		return "", usageError{"workspace ID must be 2-48 lowercase letters, digits, hyphens, or underscores"}
	}
	return value, nil
}
