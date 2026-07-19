import { Archive, Copy, KeyRound, Plus, RefreshCw } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Layout } from "../components/Layout";
import { SettingsNav } from "../components/SettingsNav";
import { DefinitionList, EmptyState, ErrorNotice, Field, Loading, Modal, Panel, Sheet } from "../components/ui";
import type { Workspace } from "../lib/api";
import { errorMessage } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatRelative, formatTime } from "../lib/format";
import { notifyWorkspaceRegistryChanged } from "../lib/workspaces";

export function WorkspacesPage() {
  const { api, settings, updateSettings, notify } = useApp();
  const state = useAsync(() => api.workspaces(), [api]);
  const [selectedID, setSelectedID] = useState("");
  const [creating, setCreating] = useState(false);
  const selected = useMemo(
    () => state.data?.items.find((workspace) => workspace.id === selectedID) || null,
    [selectedID, state.data],
  );

  return (
    <Layout
      title="Workspaces"
      subtitle="Create managed control-plane namespaces and manage their access boundary."
      actions={
        <>
          <button className="button" type="button" onClick={state.reload} title="Refresh workspaces">
            <RefreshCw size={16} aria-hidden="true" /> Refresh
          </button>
          <button className="button primary" type="button" onClick={() => setCreating(true)}>
            <Plus size={16} aria-hidden="true" /> Create workspace
          </button>
        </>
      }
    >
      <SettingsNav />
      {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
      {state.loading && !state.data ? <Loading label="Loading workspaces…" /> : null}
      {state.data ? (
        <Panel title="Workspace registry" subtitle={`${state.data.items.length} managed workspace${state.data.items.length === 1 ? "" : "s"}`}>
          {state.data.items.length === 0 ? (
            <EmptyState title="No workspaces registered." />
          ) : (
            <div className="tableWrap">
              <table className="table workspaceTable" id="workspaceRegistry">
                <thead>
                  <tr>
                    <th>Workspace</th>
                    <th>Status</th>
                    <th>Access token</th>
                    <th>Updated</th>
                    <th>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {state.data.items.map((workspace) => (
                    <tr key={workspace.id}>
                      <td>
                        <span className="cellTitle">{workspace.name}</span>
                        <span className="cellSub mono">{workspace.id}{workspace.id === settings.workspace ? " · current" : ""}</span>
                      </td>
                      <td><WorkspaceStatus workspace={workspace} /></td>
                      <td>{workspace.has_token ? "Configured" : "Not configured"}</td>
                      <td title={formatTime(workspace.updated_at)}>
                        <span className="cellTitle">{formatRelative(workspace.updated_at)}</span>
                        <span className="cellSub">{workspace.updated_by}</span>
                      </td>
                      <td className="tableActions">
                        <button className="button small" type="button" onClick={() => setSelectedID(workspace.id)}>Manage</button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Panel>
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
      {selected ? (
        <WorkspaceSheet
          workspace={selected}
          onClose={() => setSelectedID("")}
          onChanged={(workspace) => {
            if (workspace.status === "archived" && workspace.id === settings.workspace) {
              updateSettings({ ...settings, workspace: "default" });
              notify("info", "Archived workspace. Switched to default.");
            }
            state.reload();
            notifyWorkspaceRegistryChanged();
          }}
        />
      ) : null}
    </Layout>
  );
}

function WorkspaceStatus({ workspace }: { workspace: Workspace }) {
  return workspace.status === "active" ? (
    <span className="badge badge-good">Active</span>
  ) : (
    <span className="badge badge-neutral">Archived</span>
  );
}

function CreateWorkspaceDialog({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const { api, notify } = useApp();
  const [id, setID] = useState("");
  const [name, setName] = useState("");
  const [token, setToken] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  async function create() {
    setSaving(true);
    setError("");
    try {
      const result = await api.createWorkspace(id.trim(), name.trim());
      setToken(result.api_token);
      onCreated();
      notify("ok", `Workspace ${result.workspace.id} created.`);
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal title={token ? "Workspace created" : "Create workspace"} subtitle={token ? "Store the workspace token now; it will not be shown again." : "Workspace IDs are permanent routing identifiers."} onClose={onClose}>
      {error ? <ErrorNotice message={error} /> : null}
      {token ? (
        <OneTimeToken token={token} />
      ) : (
        <div className="dialogForm">
          <Field label="Workspace ID" hint="Lowercase letters, digits, and hyphens. Cannot be changed later.">
            <input value={id} onChange={(event) => setID(event.target.value)} placeholder="team-a" autoFocus />
          </Field>
          <Field label="Display name">
            <input value={name} onChange={(event) => setName(event.target.value)} placeholder="Team A" />
          </Field>
          <div className="dialogFooter">
            <button className="button primary" type="button" disabled={saving || !id.trim() || !name.trim()} onClick={create}>
              {saving ? "Creating…" : "Create workspace"}
            </button>
          </div>
        </div>
      )}
    </Modal>
  );
}

function WorkspaceSheet({ workspace, onClose, onChanged }: { workspace: Workspace; onClose: () => void; onChanged: (workspace: Workspace) => void }) {
  const { api, notify } = useApp();
  const [name, setName] = useState(workspace.name);
  const [token, setToken] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const audit = useAsync(() => api.workspaceAudit(workspace.id), [api, workspace.id]);

  useEffect(() => {
    setName(workspace.name);
  }, [workspace.name]);

  async function save() {
    setSaving(true);
    setError("");
    try {
      const updated = await api.updateWorkspace(workspace.id, name.trim());
      onChanged(updated);
      notify("ok", "Workspace name updated.");
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSaving(false);
    }
  }

  async function rotateToken() {
    if (!window.confirm(`Rotate the access token for ${workspace.name}? The current token will stop working immediately.`)) return;
    setSaving(true);
    setError("");
    try {
      const result = await api.rotateWorkspaceToken(workspace.id);
      setToken(result.api_token);
      onChanged(result.workspace);
      audit.reload();
      notify("ok", "Workspace token rotated.");
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSaving(false);
    }
  }

  async function archive() {
    if (!window.confirm(`Archive ${workspace.name}? Reads remain available, but releases, settings changes, and new runs will be blocked.`)) return;
    setSaving(true);
    setError("");
    try {
      const updated = await api.archiveWorkspace(workspace.id);
      onChanged(updated);
      notify("ok", "Workspace archived.");
      onClose();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Sheet
      title={workspace.name}
      subtitle={workspace.id}
      onClose={onClose}
      actions={
        <button className="button primary" type="button" disabled={saving || !name.trim() || name.trim() === workspace.name} onClick={save}>
          {saving ? "Saving…" : "Save changes"}
        </button>
      }
    >
      {error ? <ErrorNotice message={error} /> : null}
      <DefinitionList items={[["Status", <WorkspaceStatus workspace={workspace} />], ["Access token", workspace.has_token ? "Configured" : "Not configured"], ["Created", formatTime(workspace.created_at)], ["Updated by", workspace.updated_by]]} />
      <Field label="Display name">
        <input value={name} disabled={workspace.status === "archived"} onChange={(event) => setName(event.target.value)} />
      </Field>
      <section className="workspaceAccessSection">
        <div>
          <h3>Workspace access token</h3>
          <p>Grants API access only to this workspace. It cannot list or manage other workspaces.</p>
        </div>
        <button className="button" type="button" disabled={saving || workspace.status === "archived"} onClick={rotateToken}>
          <KeyRound size={16} aria-hidden="true" /> Rotate token
        </button>
      </section>
      {token ? <OneTimeToken token={token} /> : null}
      <section className="workspaceAuditSection">
        <h3>Lifecycle audit</h3>
        {audit.error ? <ErrorNotice message={audit.error} onRetry={audit.reload} /> : null}
        {audit.loading && !audit.data ? <Loading label="Loading audit…" /> : null}
        {audit.data?.items.length === 0 ? <p className="fieldHint">No lifecycle events.</p> : null}
        {audit.data?.items.map((record) => (
          <div className="workspaceAuditRow" key={record.id}>
            <span className="cellTitle">{record.kind.replaceAll("_", " ")}</span>
            <span className="cellSub">{record.actor} · {formatTime(record.created_at)}</span>
          </div>
        ))}
      </section>
      {workspace.status === "active" && workspace.id !== "default" ? (
        <section className="dangerZone">
          <div>
            <h3>Archive workspace</h3>
            <p>Preserves data for audit and export while blocking future changes and executions.</p>
          </div>
          <button className="button danger" type="button" disabled={saving} onClick={archive}>
            <Archive size={16} aria-hidden="true" /> Archive
          </button>
        </section>
      ) : null}
    </Sheet>
  );
}

function OneTimeToken({ token }: { token: string }) {
  const { notify } = useApp();
  return (
    <div className="oneTimeToken">
      <p className="fieldLabel">One-time workspace token</p>
      <div className="copyField">
        <code>{token}</code>
        <button className="button iconButton" type="button" title="Copy token" aria-label="Copy workspace token" onClick={async () => {
          await navigator.clipboard.writeText(token);
          notify("ok", "Workspace token copied.");
        }}>
          <Copy size={16} aria-hidden="true" />
        </button>
      </div>
      <p className="fieldHint">This value is shown once. Rotating it immediately invalidates the previous token.</p>
    </div>
  );
}
