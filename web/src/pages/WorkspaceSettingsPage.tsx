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
import { translate } from "../shared/i18n";

export function WorkspaceSettingsPage() {
  const { api, settings } = useApp();
  const state = useAsync(() => api.workspace(settings.workspace), [api, settings.workspace]);

  return (
    <Layout
      title={translate("navigation.settings")}
      subtitle={translate("workspaceSettings.subtitle")}
    >
      <SettingsNav />
      {state.loading && !state.data ? (
        <Loading label={translate("workspaceSettings.loading")} />
      ) : null}
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
      notify("ok", translate("workspaceSettings.nameUpdated"));
      notifyWorkspaceRegistryChanged();
      onChanged();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSaving(false);
    }
  }

  async function archive() {
    if (!window.confirm(translate("workspaceSettings.archiveConfirm", { name: workspace.name })))
      return;
    setSaving(true);
    setError("");
    try {
      await api.archiveWorkspace(workspace.id);
      updateSettings({ ...settings, workspace: "default" });
      notify("info", translate("workspaceSettings.archivedSwitched"));
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
        title={translate("workspaceSettings.identity")}
        subtitle={translate("workspaceSettings.identityHint")}
      >
        {error ? <ErrorNotice message={error} /> : null}
        <DefinitionList
          className="workspaceIdentityFacts"
          items={[
            [translate("settings.workspaceID"), <span className="mono">{workspace.id}</span>],
            [translate("common.status"), <WorkspaceStatus workspace={workspace} />],
            [translate("workspaceSettings.created"), formatTime(workspace.created_at)],
            [translate("workspaceSettings.createdBy"), workspace.created_by],
          ]}
        />
        <div className="workspaceSingleSetting">
          <Field
            label={translate("workspaces.displayName")}
            hint={translate("workspaceSettings.displayNameHint")}
          >
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
            {saving ? translate("common.saving") : translate("workspaceSettings.saveDisplayName")}
          </button>
        </div>
      </Panel>

      <Panel
        title={translate("workspaceSettings.lifecycle")}
        subtitle={translate("workspaceSettings.lifecycleHint")}
      >
        {workspace.id === "default" ? (
          <div className="inlineNotice">{translate("workspaceSettings.defaultPermanent")}</div>
        ) : workspace.status === "archived" ? (
          <div className="inlineNotice">{translate("workspaceSettings.archivedNotice")}</div>
        ) : (
          <div className="dangerZone">
            <div>
              <strong>{translate("workspaceSettings.archive")}</strong>
              <p>{translate("workspaceSettings.archiveWarning")}</p>
            </div>
            <button className="button danger" type="button" disabled={saving} onClick={archive}>
              <Archive size={16} aria-hidden="true" />{" "}
              {saving
                ? translate("workspaceSettings.archiving")
                : translate("workspaceSettings.archive")}
            </button>
          </div>
        )}
      </Panel>
    </>
  );
}
