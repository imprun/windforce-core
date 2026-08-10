package server

import (
	"net/http"

	"github.com/imprun/windforce-core/internal/contract"
)

type systemInfoResponse struct {
	Service       string                 `json:"service"`
	Workspace     string                 `json:"workspace"`
	Ready         bool                   `json:"ready"`
	Planes        map[string]bool        `json:"planes"`
	Backends      map[string]bool        `json:"backends"`
	Auth          map[string]bool        `json:"auth"`
	RuntimeConfig map[string]interface{} `json:"runtime_config"`
}

func (h *Handler) handleSystemInfo(w http.ResponseWriter, r *http.Request, workspaceID string) {
	waitMilliseconds := int64(0)
	if h.wait > 0 {
		waitMilliseconds = h.wait.Milliseconds()
	}
	triggerCount := 0
	scheduleCount := 0
	httpRouteCount := 0
	httpRouteReadyCount := 0
	httpRouteErrorCount := 0
	if h.store != nil {
		if definitions, err := h.store.ListTriggers(r.Context(), workspaceID); err == nil {
			triggerCount = len(definitions)
			for _, definition := range definitions {
				if definition.Kind == "schedule" {
					scheduleCount++
				}
			}
		}
		if bindings, err := h.store.ListHTTPRouteBindings(r.Context(), workspaceID, "", false); err == nil {
			httpRouteCount = len(bindings)
			for _, binding := range bindings {
				switch binding.State {
				case "ready":
					httpRouteReadyCount++
				case "error":
					httpRouteErrorCount++
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, systemInfoResponse{
		Service:   "windforce-lite",
		Workspace: contract.NormalizeWorkspace(workspaceID),
		Ready:     h.store != nil,
		Planes: map[string]bool{
			"invocation_api": true,
			"control_api":    true,
			"worker_api":     true,
			"trigger_api":    h.triggerManager != nil,
			"http_routes":    true,
			"web_ui":         h.uiMode == UIModeEmbedded,
			"metrics":        h.metricsHandler != nil,
		},
		Backends: map[string]bool{
			"state_store":         h.store != nil,
			"catalog":             h.catalog != nil,
			"syncer":              h.syncer != nil,
			"execution_bundles":   h.executionBundles != nil,
			"git_sources":         h.gitSources != nil,
			"artifact_store":      h.artifactStore != nil,
			"http_route_provider": h.httpRouteProvider != "",
		},
		Auth: map[string]bool{
			"admin_token_configured":  h.adminToken != "",
			"worker_token_configured": h.workerToken != "",
			"job_token_configured":    h.jobTokenSecret != "",
			"secret_key_configured":   h.secretKey != "" && h.secretKey != DefaultSecretKey,
			"previous_secret_key":     h.secretKeyPrevious != "",
		},
		RuntimeConfig: map[string]interface{}{
			"wait_ms":               waitMilliseconds,
			"sample_root":           h.sampleRoot != "",
			"managed_workspaces":    h.managedWorkspaces,
			"triggers_count":        triggerCount,
			"schedules_count":       scheduleCount,
			"http_route_provider":   h.httpRouteProvider,
			"http_routes_count":     httpRouteCount,
			"http_routes_ready":     httpRouteReadyCount,
			"http_routes_error":     httpRouteErrorCount,
			"ui_mode":               h.uiMode,
			"worker_group_operator": h.workerGroupOperator,
		},
	})
}
