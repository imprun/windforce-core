import { KeyRound, RefreshCw, ShieldX } from "lucide-react";
import { useState } from "react";
import { Layout } from "../components/Layout";
import { SettingsNav } from "../components/SettingsNav";
import { ConfirmDialog, EmptyState, ErrorNotice, Loading, Panel } from "../components/ui";
import {
  CLIConnectionPanel,
  HostedAccessPanels,
  LocalBrowserAccessPanel,
} from "../features/AccessSettings";
import { OneTimeWorkspaceToken } from "../features/WorkspaceAdmin";
import type { WorkspaceToken } from "../lib/api";
import { errorMessage } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatRelative, formatTime } from "../lib/format";
import { translate } from "../shared/i18n";

type PendingTokenAction = {
  kind: "rotate" | "revoke";
  token: WorkspaceToken;
};

export function WorkspaceAccessSettingsPage() {
  const { runtimeConfig } = useApp();
  const hosted = Boolean(runtimeConfig?.hostAccount);

  return (
    <Layout
      title={translate("workspaceAccess.pageTitle")}
      subtitle={
        hosted ? translate("workspaceAccess.hostedSubtitle") : translate("workspaceAccess.subtitle")
      }
    >
      <SettingsNav />
      {hosted ? (
        <HostedAccessPanels hostConsole={runtimeConfig?.hostConsole || null} />
      ) : (
        <StandaloneAccessSettings />
      )}
    </Layout>
  );
}

function StandaloneAccessSettings() {
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
  const [pendingAction, setPendingAction] = useState<PendingTokenAction | null>(null);

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

  function confirmPendingAction() {
    const action = pendingAction;
    setPendingAction(null);
    if (!action) return;
    if (action.kind === "rotate") void rotateToken(action.token);
    else void revokeToken(action.token);
  }

  const loading =
    (workspaceState.loading && !workspaceState.data) || (tokenState.loading && !tokenState.data);
  const pageError = workspaceState.error || tokenState.error;
  const archived = workspaceState.data?.status === "archived";

  return (
    <>
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
            title={translate("workspaceAccess.workspaceTokens")}
            subtitle={translate("workspaceAccess.workspaceTokensHint", {
              workspace: settings.workspace,
            })}
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
                    placeholder={translate("workspaceAccess.tokenNamePlaceholder")}
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
            <section
              className="workspaceTokenRegistry"
              aria-labelledby="workspaceTokenRegistryTitle"
            >
              <div className="workspaceTokenRegistryHeader">
                <h3 id="workspaceTokenRegistryTitle">{translate("workspaceAccess.namedTokens")}</h3>
                <p>{translate("workspaceAccess.namedTokensHint")}</p>
              </div>
              {tokenState.data.items.length === 0 ? (
                <div className="workspaceTokensEmpty">
                  <EmptyState title={translate("workspaceAccess.empty")} />
                </div>
              ) : (
                <div className="tableWrap">
                  <table className="table workspaceTokenTable">
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
                                token.status === "active"
                                  ? "badge badge-good"
                                  : "badge badge-neutral"
                              }
                            >
                              {token.status === "active"
                                ? translate("workspace.status.active")
                                : translate("workspaceAccess.revoked")}
                            </span>
                          </td>
                          <td title={formatTime(token.updated_at)}>
                            <span className="cellTitle">{formatRelative(token.updated_at)}</span>
                          </td>
                          <td className="tableActions">
                            {token.status === "active" ? (
                              <div className="workspaceTableActions">
                                <button
                                  className="button small"
                                  type="button"
                                  disabled={saving || archived}
                                  onClick={() => setPendingAction({ kind: "rotate", token })}
                                >
                                  <RefreshCw size={15} aria-hidden="true" />{" "}
                                  {translate("workspaceAccess.rotate")}
                                </button>
                                <button
                                  className="button danger small"
                                  type="button"
                                  disabled={saving}
                                  onClick={() => setPendingAction({ kind: "revoke", token })}
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
            </section>
          </Panel>

          <CLIConnectionPanel />
          <LocalBrowserAccessPanel />
        </>
      ) : null}

      {pendingAction ? (
        <ConfirmDialog
          title={
            pendingAction.kind === "rotate"
              ? translate("workspaceAccess.rotateTitle")
              : translate("workspaceAccess.revokeTitle")
          }
          description={
            pendingAction.kind === "rotate"
              ? translate("workspaceAccess.rotateConfirm", { name: pendingAction.token.name })
              : translate("workspaceAccess.revokeConfirm", { name: pendingAction.token.name })
          }
          confirmLabel={
            pendingAction.kind === "rotate"
              ? translate("workspaceAccess.rotate")
              : translate("workspaceAccess.revoke")
          }
          onConfirm={confirmPendingAction}
          onCancel={() => setPendingAction(null)}
        />
      ) : null}
    </>
  );
}
