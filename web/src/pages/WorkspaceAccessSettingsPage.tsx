import { KeyRound, RefreshCw, ShieldX } from "lucide-react";
import { useState } from "react";
import { Layout } from "../components/Layout";
import { SettingsNav } from "../components/SettingsNav";
import { EmptyState, ErrorNotice, Loading, Panel } from "../components/ui";
import { OneTimeWorkspaceToken } from "../features/WorkspaceAdmin";
import type { WorkspaceToken } from "../lib/api";
import { errorMessage } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatTime } from "../lib/format";
import { translate } from "../shared/i18n";

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
      notify("ok", translate("workspaceAccess.tokenCreated"));
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSaving(false);
    }
  }

  async function rotateToken(token: WorkspaceToken) {
    if (!window.confirm(translate("workspaceAccess.rotateConfirm", { name: token.name }))) return;
    setSaving(true);
    setError("");
    try {
      const result = await api.rotateWorkspaceToken(settings.workspace, token.id);
      setSecret(result.api_token);
      tokenState.reload();
      notify("ok", translate("workspaceAccess.tokenRotated"));
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSaving(false);
    }
  }

  async function revokeToken(token: WorkspaceToken) {
    if (!window.confirm(translate("workspaceAccess.revokeConfirm", { name: token.name }))) return;
    setSaving(true);
    setError("");
    try {
      await api.revokeWorkspaceToken(settings.workspace, token.id);
      tokenState.reload();
      notify("ok", translate("workspaceAccess.tokenRevoked"));
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
      title={translate("navigation.settings")}
      subtitle={translate("workspaceAccess.subtitle")}
    >
      <SettingsNav />
      {loading ? <Loading label={translate("workspaceAccess.loading")} /> : null}
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
            title={translate("workspaceAccess.create")}
            subtitle={translate("workspaceAccess.createHint", { workspace: settings.workspace })}
          >
            <form
              className="workspaceTokenCreate"
              onSubmit={(event) => {
                event.preventDefault();
                if (!saving && !archived && name.trim()) void createToken();
              }}
            >
              <div className="field">
                <label className="fieldLabel" htmlFor="workspaceTokenName">
                  {translate("workspaceAccess.tokenName")}
                </label>
                <div className="fieldWithAction">
                  <input
                    id="workspaceTokenName"
                    value={name}
                    placeholder="Developer CLI"
                    disabled={archived}
                    aria-describedby="workspaceTokenNameHint"
                    onChange={(event) => setName(event.target.value)}
                  />
                  <button
                    className="button primary"
                    type="submit"
                    disabled={saving || archived || !name.trim()}
                  >
                    <KeyRound size={16} aria-hidden="true" />{" "}
                    {saving
                      ? translate("workspaces.creating")
                      : translate("workspaceAccess.createToken")}
                  </button>
                </div>
                <span className="fieldHint" id="workspaceTokenNameHint">
                  {translate("workspaceAccess.tokenNameHint")}
                </span>
              </div>
            </form>
            {secret ? <OneTimeWorkspaceToken token={secret} /> : null}
          </Panel>

          <Panel
            title={translate("workspaceAccess.namedTokens")}
            subtitle={translate("workspaceAccess.namedTokensHint")}
          >
            {tokenState.data.items.length === 0 ? (
              <EmptyState title={translate("workspaceAccess.empty")} />
            ) : (
              <div className="tableWrap">
                <table className="table">
                  <thead>
                    <tr>
                      <th>{translate("common.name")}</th>
                      <th>{translate("common.status")}</th>
                      <th>{translate("common.updated")}</th>
                      <th>{translate("common.actions")}</th>
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
                            {token.status === "active"
                              ? translate("workspace.status.active")
                              : translate("workspaceAccess.revoked")}
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
                                <RefreshCw size={15} aria-hidden="true" />{" "}
                                {translate("workspaceAccess.rotate")}
                              </button>
                              <button
                                className="button danger small"
                                type="button"
                                disabled={saving}
                                onClick={() => revokeToken(token)}
                              >
                                <ShieldX size={15} aria-hidden="true" />{" "}
                                {translate("workspaceAccess.revoke")}
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
