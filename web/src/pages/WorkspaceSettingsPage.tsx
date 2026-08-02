import { Archive, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Layout } from "../components/Layout";
import { SettingsNav } from "../components/SettingsNav";
import {
  ConfirmDialog,
  DefinitionList,
  ErrorNotice,
  Field,
  Loading,
  Panel,
} from "../components/ui";
import { WorkspaceStatus } from "../features/WorkspaceAdmin";
import type { Workspace } from "../lib/api";
import { errorMessage } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatTime } from "../lib/format";
import { useRouter } from "../lib/router";
import { notifyWorkspaceRegistryChanged } from "../lib/workspaces";
import { translate } from "../shared/i18n";
import { WorkspaceAccessSections } from "./WorkspaceAccessSettingsPage";

export function WorkspaceSettingsPage() {
  const { api, settings } = useApp();
  const state = useAsync(() => api.workspace(settings.workspace), [api, settings.workspace]);

  return (
    <Layout
      title={translate("workspaceSettings.pageTitle")}
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
  const { api, settings, updateSettings, notify, runtimeConfig } = useApp();
  const { navigate } = useRouter();
  const [name, setName] = useState(workspace.name);
  const [operation, setOperation] = useState<"save" | "archive" | "delete" | null>(null);
  const [error, setError] = useState("");
  const [confirmArchive, setConfirmArchive] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  useEffect(() => setName(workspace.name), [workspace.name]);

  async function save() {
    setOperation("save");
    setError("");
    try {
      await api.updateWorkspace(workspace.id, name.trim());
      notify("ok", translate("workspaceSettings.nameUpdated"));
      notifyWorkspaceRegistryChanged();
      onChanged();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setOperation(null);
    }
  }

  async function archive() {
    setConfirmArchive(false);
    setOperation("archive");
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
      setOperation(null);
    }
  }

  async function remove() {
    setConfirmDelete(false);
    setOperation("delete");
    setError("");
    try {
      await api.deleteWorkspace(workspace.id);
      updateSettings({ ...settings, workspace: "default" });
      notify("ok", translate("workspaceSettings.deletedSwitched"));
      notifyWorkspaceRegistryChanged();
      navigate("/");
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setOperation(null);
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
            <div className="fieldWithAction">
              <input
                value={name}
                disabled={workspace.status === "archived"}
                onChange={(event) => setName(event.target.value)}
              />
              <button
                className="button primary"
                type="button"
                disabled={
                  operation !== null ||
                  workspace.status === "archived" ||
                  !name.trim() ||
                  name.trim() === workspace.name
                }
                onClick={save}
              >
                {operation === "save"
                  ? translate("common.saving")
                  : translate("workspaceSettings.saveDisplayName")}
              </button>
            </div>
          </Field>
        </div>
      </Panel>

      <WorkspaceAccessSections workspace={workspace} />

      <Panel
        title={translate("workspaceSettings.lifecycle")}
        subtitle={translate("workspaceSettings.lifecycleHint")}
      >
        {runtimeConfig?.authMode === "host_managed" ? (
          <div className="inlineNotice">{translate("workspaceSettings.hostManagedLifecycle")}</div>
        ) : workspace.id === "default" ? (
          <div className="inlineNotice">{translate("workspaceSettings.defaultPermanent")}</div>
        ) : (
          <>
            {workspace.status === "archived" ? (
              <div className="inlineNotice">{translate("workspaceSettings.archivedNotice")}</div>
            ) : (
              <div className="dangerZone">
                <div>
                  <strong>{translate("workspaceSettings.archive")}</strong>
                  <p>{translate("workspaceSettings.archiveWarning")}</p>
                </div>
                <button
                  className="button secondary"
                  type="button"
                  disabled={operation !== null}
                  onClick={() => setConfirmArchive(true)}
                >
                  <Archive size={16} aria-hidden="true" />{" "}
                  {operation === "archive"
                    ? translate("workspaceSettings.archiving")
                    : translate("workspaceSettings.archive")}
                </button>
              </div>
            )}
            <div className="dangerZone">
              <div>
                <strong>{translate("workspaceSettings.delete")}</strong>
                <p>{translate("workspaceSettings.deleteWarning")}</p>
              </div>
              <button
                className="button danger"
                type="button"
                disabled={operation !== null}
                onClick={() => setConfirmDelete(true)}
              >
                <Trash2 size={16} aria-hidden="true" />{" "}
                {operation === "delete"
                  ? translate("workspaceSettings.deleting")
                  : translate("workspaceSettings.delete")}
              </button>
            </div>
          </>
        )}
      </Panel>
      {confirmArchive ? (
        <ConfirmDialog
          title={translate("workspaceSettings.archiveTitle")}
          description={translate("workspaceSettings.archiveConfirm", { name: workspace.name })}
          confirmLabel={translate("workspaceSettings.archive")}
          onConfirm={() => void archive()}
          onCancel={() => setConfirmArchive(false)}
        />
      ) : null}
      {confirmDelete ? (
        <ConfirmDialog
          title={translate("workspaceSettings.deleteTitle")}
          description={translate("workspaceSettings.deleteConfirm", { name: workspace.name })}
          confirmLabel={translate("workspaceSettings.delete")}
          tone="danger"
          confirmation={{
            label: translate("workspaceSettings.deleteConfirmationLabel", {
              name: workspace.name,
            }),
            expected: workspace.name,
          }}
          onConfirm={() => void remove()}
          onCancel={() => setConfirmDelete(false)}
        />
      ) : null}
    </>
  );
}
