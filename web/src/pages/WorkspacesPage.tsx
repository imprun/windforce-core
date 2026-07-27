import { Plus, RefreshCw } from "lucide-react";
import { useState } from "react";
import { Layout } from "../components/Layout";
import { EmptyState, ErrorNotice, Field, Loading, Modal, Panel } from "../components/ui";
import { WorkspaceActivation, WorkspaceStatus } from "../features/WorkspaceAdmin";
import { errorMessage } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { notifyWorkspaceRegistryChanged } from "../lib/workspaces";

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
      title="Workspaces"
      subtitle="Create a workspace or switch the active execution context."
      actions={
        <>
          <button
            className="button"
            type="button"
            onClick={state.reload}
            title="Refresh workspaces"
          >
            <RefreshCw size={16} aria-hidden="true" /> Refresh
          </button>
          <button className="button primary" type="button" onClick={() => setCreating(true)}>
            <Plus size={16} aria-hidden="true" /> Create workspace
          </button>
        </>
      }
    >
      {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
      {state.loading && !state.data ? <Loading label="Loading workspaces…" /> : null}
      {state.data ? (
        <>
          <dl className="workspaceRegistrySummary">
            <div>
              <dt>Total</dt>
              <dd>{state.data.items.length}</dd>
            </div>
            <div>
              <dt>Active</dt>
              <dd>{activeCount}</dd>
            </div>
            <div>
              <dt>Archived</dt>
              <dd>{archivedCount}</dd>
            </div>
          </dl>
          <Panel
            title="All workspaces"
            subtitle="Workspace settings are available after switching into that workspace."
          >
            {state.data.items.length === 0 ? (
              <EmptyState title="No workspaces registered." />
            ) : (
              <div className="tableWrap">
                <table className="table workspaceTable" id="workspaceRegistry">
                  <thead>
                    <tr>
                      <th>Workspace</th>
                      <th>Status</th>
                      <th>Switch</th>
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
      notify("ok", `Workspace ${result.id} created. Switch to it to configure access.`);
      onClose();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      title="Create workspace"
      subtitle="Workspace IDs are permanent routing identifiers. Access tokens are created separately."
      onClose={onClose}
    >
      {error ? <ErrorNotice message={error} /> : null}
      <div className="dialogForm">
        <Field
          label="Workspace ID"
          hint="Lowercase letters, digits, and hyphens. Cannot be changed later."
        >
          <input value={id} onChange={(event) => setID(event.target.value)} placeholder="team-a" />
        </Field>
        <Field label="Display name">
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
            {saving ? "Creating…" : "Create workspace"}
          </button>
        </div>
      </div>
    </Modal>
  );
}
