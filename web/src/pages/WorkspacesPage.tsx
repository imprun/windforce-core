import { Plus, RefreshCw } from "lucide-react";
import { useState } from "react";
import { Layout } from "../components/Layout";
import { EmptyState, ErrorNotice, Field, Loading, Modal, Panel } from "../components/ui";
import { WorkspaceActivation, WorkspaceStatus } from "../features/WorkspaceAdmin";
import { errorMessage } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { notifyWorkspaceRegistryChanged } from "../lib/workspaces";
import { translate } from "../shared/i18n";

export function WorkspacesPage() {
  const { api } = useApp();
  const state = useAsync(() => api.workspaces(), [api]);
  const [creating, setCreating] = useState(false);
  const activeCount =
    state.data?.items.filter((workspace) => workspace.status === "active").length || 0;
  const archivedCount =
    state.data?.items.filter((workspace) => workspace.status === "archived").length || 0;

  return (
    <Layout
      scope="instance"
      title={translate("navigation.workspaces")}
      subtitle={translate("workspaces.subtitle")}
      actions={
        <>
          <button
            className="button"
            type="button"
            onClick={state.reload}
            title={translate("workspaces.refresh")}
          >
            <RefreshCw size={16} aria-hidden="true" /> {translate("common.refresh")}
          </button>
          <button className="button primary" type="button" onClick={() => setCreating(true)}>
            <Plus size={16} aria-hidden="true" /> {translate("workspaces.create")}
          </button>
        </>
      }
    >
      {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
      {state.loading && !state.data ? <Loading label={translate("workspace.loading")} /> : null}
      {state.data ? (
        <>
          <dl className="workspaceRegistrySummary">
            <div>
              <dt>{translate("workspaces.total")}</dt>
              <dd>{state.data.items.length}</dd>
            </div>
            <div>
              <dt>{translate("workspace.status.active")}</dt>
              <dd>{activeCount}</dd>
            </div>
            <div>
              <dt>{translate("common.archived")}</dt>
              <dd>{archivedCount}</dd>
            </div>
          </dl>
          <Panel title={translate("workspaces.all")} subtitle={translate("workspaces.allHint")}>
            {state.data.items.length === 0 ? (
              <EmptyState title={translate("workspaces.empty")} />
            ) : (
              <div className="tableWrap">
                <table className="table workspaceTable" id="workspaceRegistry">
                  <thead>
                    <tr>
                      <th>{translate("settingsNav.workspace")}</th>
                      <th>{translate("common.status")}</th>
                      <th>{translate("workspace.switchShort")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {state.data.items.map((workspace) => (
                      <tr key={workspace.id}>
                        <td>
                          <span className="cellTitle">{workspace.name}</span>
                          <span className="cellSub mono">{workspace.id}</span>
                        </td>
                        <td>
                          <WorkspaceStatus workspace={workspace} />
                        </td>
                        <td className="tableActions">
                          <WorkspaceActivation workspace={workspace} compact />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Panel>
        </>
      ) : null}

      {creating ? (
        <CreateWorkspaceDialog
          onClose={() => setCreating(false)}
          onCreated={() => {
            state.reload();
            notifyWorkspaceRegistryChanged();
          }}
        />
      ) : null}
    </Layout>
  );
}

function CreateWorkspaceDialog({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: () => void;
}) {
  const { api, notify } = useApp();
  const [id, setID] = useState("");
  const [name, setName] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  async function create() {
    setSaving(true);
    setError("");
    try {
      const result = await api.createWorkspace(id.trim(), name.trim());
      onCreated();
      notify("ok", translate("workspaces.created", { id: result.id }));
      onClose();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      title={translate("workspaces.create")}
      subtitle={translate("workspaces.createHint")}
      onClose={onClose}
    >
      {error ? <ErrorNotice message={error} /> : null}
      <div className="dialogForm">
        <Field label={translate("settings.workspaceID")} hint={translate("workspaces.idHint")}>
          <input value={id} onChange={(event) => setID(event.target.value)} placeholder="team-a" />
        </Field>
        <Field label={translate("workspaces.displayName")}>
          <input
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="Team A"
          />
        </Field>
        <div className="dialogFooter">
          <button
            className="button primary"
            type="button"
            disabled={saving || !id.trim() || !name.trim()}
            onClick={create}
          >
            {saving ? translate("workspaces.creating") : translate("workspaces.create")}
          </button>
        </div>
      </div>
    </Modal>
  );
}
