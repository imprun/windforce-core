import { useEffect, useMemo } from "react";
import { useApp, useAsync } from "../lib/app-context";
import { useRouter } from "../lib/router";
import { visibleWorkspaces, WORKSPACE_REGISTRY_CHANGED } from "../lib/workspaces";

export function WorkspaceSwitcher() {
  const { api, settings, updateSettings } = useApp();
  const { navigate } = useRouter();
  const state = useAsync(() => api.workspaces(), [api]);
  const workspaces = useMemo(
    () => visibleWorkspaces(state.data?.items || [], settings.workspace),
    [settings.workspace, state.data],
  );

  useEffect(() => {
    window.addEventListener(WORKSPACE_REGISTRY_CHANGED, state.reload);
    return () => window.removeEventListener(WORKSPACE_REGISTRY_CHANGED, state.reload);
  }, [state.reload]);

  if (state.error || workspaces.length === 0) {
    return <span className="workspacePill" title="Active workspace">workspace / {settings.workspace}</span>;
  }

  return (
    <label className="workspaceSwitcher" title="Active workspace">
      <span>Workspace</span>
      <select
        aria-label="Active workspace"
        value={settings.workspace}
        onChange={(event) => {
          updateSettings({ ...settings, workspace: event.target.value });
          navigate("/");
        }}
      >
        {workspaces.map((workspace) => (
          <option key={workspace.id} value={workspace.id}>
            {workspace.name} ({workspace.id}){workspace.status === "archived" ? " — archived" : ""}
          </option>
        ))}
      </select>
    </label>
  );
}
