import { KeyRound, RefreshCw, ShieldX } from "lucide-react";
import { useState } from "react";
import { Layout } from "../components/Layout";
import { SettingsNav } from "../components/SettingsNav";
import { EmptyState, ErrorNotice, Field, Loading, Panel } from "../components/ui";
import { OneTimeWorkspaceToken } from "../features/WorkspaceAdmin";
import type { WorkspaceToken } from "../lib/api";
import { errorMessage } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatTime } from "../lib/format";

export function WorkspaceAccessSettingsPage() {
  const { api, settings, notify } = useApp();
  const workspaceState = useAsync(
    () => api.workspace(settings.workspace),
    [api, settings.workspace],
  );
  const tokenState = useAsync(
    () => api.workspaceTokens(settings.workspace),
    [api, settings.workspace],
  );
  const [name, setName] = useState("");
  const [secret, setSecret] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  async function createToken() {
    setSaving(true);
    setError("");
    try {
      const result = await api.createWorkspaceToken(settings.workspace, name.trim());
      setSecret(result.api_token);
      setName("");
      tokenState.reload();
      notify("ok", "Workspace token created.");
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSaving(false);
    }
  }

  async function rotateToken(token: WorkspaceToken) {
    if (!window.confirm(`Rotate ${token.name}? The current secret will stop working immediately.`))
      return;
    setSaving(true);
    setError("");
    try {
      const result = await api.rotateWorkspaceToken(settings.workspace, token.id);
      setSecret(result.api_token);
      tokenState.reload();
      notify("ok", "Workspace token rotated.");
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSaving(false);
    }
  }

  async function revokeToken(token: WorkspaceToken) {
    if (!window.confirm(`Revoke ${token.name}? This secret will stop working immediately.`)) return;
    setSaving(true);
    setError("");
    try {
      await api.revokeWorkspaceToken(settings.workspace, token.id);
      tokenState.reload();
      notify("ok", "Workspace token revoked.");
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSaving(false);
    }
  }

  const loading =
    (workspaceState.loading && !workspaceState.data) || (tokenState.loading && !tokenState.data);
  const pageError = workspaceState.error || tokenState.error;
  const archived = workspaceState.data?.status === "archived";

  return (
    <Layout
      title="Settings"
      subtitle="Issue and revoke named credentials for the active workspace."
    >
      <SettingsNav />
      {loading ? <Loading label="Loading workspace access…" /> : null}
      {pageError ? (
        <ErrorNotice
          message={pageError}
          onRetry={() => {
            workspaceState.reload();
            tokenState.reload();
          }}
        />
      ) : null}
      {error ? <ErrorNotice message={error} /> : null}
      {workspaceState.data && tokenState.data ? (
        <>
          <Panel
            title="Create workspace token"
            subtitle={`Full operator access scoped to /api/w/${settings.workspace}. Use service principals for integrations.`}
          >
            <div className="workspaceSingleSetting">
              <Field label="Token name" hint="Identify the CLI, operator, or recovery purpose.">
                <input
                  value={name}
                  placeholder="Developer CLI"
                  disabled={archived}
                  onChange={(event) => setName(event.target.value)}
                />
              </Field>
              <button
                className="button primary"
                type="button"
                disabled={saving || archived || !name.trim()}
                onClick={createToken}
              >
                <KeyRound size={16} aria-hidden="true" /> {saving ? "Creating…" : "Create token"}
              </button>
            </div>
            {secret ? <OneTimeWorkspaceToken token={secret} /> : null}
          </Panel>

          <Panel
            title="Named workspace tokens"
            subtitle="Secrets are shown once. Rotation invalidates the old secret; revocation leaves no active replacement."
          >
            {tokenState.data.items.length === 0 ? (
              <EmptyState title="No workspace tokens configured." />
            ) : (
              <div className="tableWrap">
                <table className="table">
                  <thead>
                    <tr>
                      <th>Name</th>
                      <th>Status</th>
                      <th>Updated</th>
                      <th>Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {tokenState.data.items.map((token) => (
                      <tr key={token.id}>
                        <td>
                          <span className="cellTitle">{token.name}</span>
                          <span className="cellSub mono">{token.id}</span>
                        </td>
                        <td>
                          <span
                            className={
                              token.status === "active" ? "badge badge-good" : "badge badge-neutral"
                            }
                          >
                            {token.status === "active" ? "Active" : "Revoked"}
                          </span>
                        </td>
                        <td title={formatTime(token.updated_at)}>{formatTime(token.updated_at)}</td>
                        <td className="tableActions">
                          {token.status === "active" ? (
                            <div className="workspaceTableActions">
                              <button
                                className="button small"
                                type="button"
                                disabled={saving || archived}
                                onClick={() => rotateToken(token)}
                              >
                                <RefreshCw size={15} aria-hidden="true" /> Rotate
                              </button>
                              <button
                                className="button danger small"
                                type="button"
                                disabled={saving}
                                onClick={() => revokeToken(token)}
                              >
                                <ShieldX size={15} aria-hidden="true" /> Revoke
                              </button>
                            </div>
                          ) : (
                            "—"
                          )}
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
    </Layout>
  );
}
