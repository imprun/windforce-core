import { Archive } from "lucide-react";
import { useEffect, useState } from "react";
import { Layout } from "../components/Layout";
import { SettingsNav } from "../components/SettingsNav";
import { DefinitionList, ErrorNotice, Field, Loading, Panel } from "../components/ui";
import { WorkspaceStatus } from "../features/WorkspaceAdmin";
import type { Workspace } from "../lib/api";
import { errorMessage } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatTime } from "../lib/format";
import { useRouter } from "../lib/router";
import { notifyWorkspaceRegistryChanged } from "../lib/workspaces";

export function WorkspaceSettingsPage() {
  const { api, settings } = useApp();
  const state = useAsync(() => api.workspace(settings.workspace), [api, settings.workspace]);

  return (
    <Layout title="Settings" subtitle="Configure the active workspace identity and lifecycle.">
      <SettingsNav />
      {state.loading && !state.data ? <Loading label="Loading workspace settings…" /> : null}
      {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
      {state.data ? <WorkspaceSettings workspace={state.data} onChanged={state.reload} /> : null}
    </Layout>
  );
}

function WorkspaceSettings({
  workspace,
  onChanged,
}: {
  workspace: Workspace;
  onChanged: () => void;
}) {
  const { api, settings, updateSettings, notify } = useApp();
  const { navigate } = useRouter();
  const [name, setName] = useState(workspace.name);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => setName(workspace.name), [workspace.name]);

  async function save() {
    setSaving(true);
    setError("");
    try {
      await api.updateWorkspace(workspace.id, name.trim());
      notify("ok", "Workspace name updated.");
      notifyWorkspaceRegistryChanged();
      onChanged();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSaving(false);
    }
  }

  async function archive() {
    if (
      !window.confirm(
        `Archive ${workspace.name}? Reads remain available, but releases, settings changes, and new runs will be blocked.`,
      )
    )
      return;
    setSaving(true);
    setError("");
    try {
      await api.archiveWorkspace(workspace.id);
      updateSettings({ ...settings, workspace: "default" });
      notify("info", "Workspace archived. Switched to default.");
      notifyWorkspaceRegistryChanged();
      navigate("/");
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSaving(false);
    }
  }

  return (
    <>
      <Panel
        title="Workspace identity"
        subtitle="The immutable routing ID and operator-facing display name."
      >
        {error ? <ErrorNotice message={error} /> : null}
        <DefinitionList
          className="workspaceIdentityFacts"
          items={[
            ["Workspace ID", <span className="mono">{workspace.id}</span>],
            ["Status", <WorkspaceStatus workspace={workspace} />],
            ["Created", formatTime(workspace.created_at)],
            ["Created by", workspace.created_by],
          ]}
        />
        <div className="workspaceSingleSetting">
          <Field label="Display name" hint="Shown in the workspace switcher and settings.">
            <input
              value={name}
              disabled={workspace.status === "archived"}
              onChange={(event) => setName(event.target.value)}
            />
          </Field>
          <button
            className="button primary"
            type="button"
            disabled={
              saving ||
              workspace.status === "archived" ||
              !name.trim() ||
              name.trim() === workspace.name
            }
            onClick={save}
          >
            {saving ? "Saving…" : "Save display name"}
          </button>
        </div>
      </Panel>

      <Panel
        title="Workspace lifecycle"
        subtitle="Archive preserves records while preventing future changes and executions."
      >
        {workspace.id === "default" ? (
          <div className="inlineNotice">
            The default workspace is permanent and cannot be archived.
          </div>
        ) : workspace.status === "archived" ? (
          <div className="inlineNotice">
            This workspace is archived. Reads and audit records remain available.
          </div>
        ) : (
          <div className="dangerZone">
            <div>
              <strong>Archive workspace</strong>
              <p>
                Blocks releases, configuration changes, webhook changes, and new runs. This action
                cannot be reversed.
              </p>
            </div>
            <button className="button danger" type="button" disabled={saving} onClick={archive}>
              <Archive size={16} aria-hidden="true" /> {saving ? "Archiving…" : "Archive workspace"}
            </button>
          </div>
        )}
      </Panel>
    </>
  );
}
