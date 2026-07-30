import { Check, Copy } from "lucide-react";
import { useState } from "react";
import { ConfirmDialog, Field, Modal } from "../components/ui";
import { type Client, errorMessage } from "../lib/api";
import { useApp } from "../lib/app-context";
import { translate } from "../shared/i18n";

type ConfirmationKind = "close" | "rotate" | "revoke" | "delete";

export function ClientDialog({
  client,
  onClose,
  onSaved,
  onDeleted,
}: {
  client?: Client;
  onClose: () => void;
  onSaved: () => void;
  onDeleted: () => void;
}) {
  const { api, notify } = useApp();
  const [name, setName] = useState(client?.name || "");
  const [hasToken, setHasToken] = useState(client?.has_token || false);
  const [issuedToken, setIssuedToken] = useState("");
  const [tokenCopied, setTokenCopied] = useState(false);
  const [confirmation, setConfirmation] = useState<ConfirmationKind | null>(null);
  const [pendingRefresh, setPendingRefresh] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const normalizedName = name.trim();
  const dirty = !client || normalizedName !== client.name;

  function finish() {
    if (pendingRefresh) onSaved();
    else onClose();
  }

  function requestClose() {
    if (issuedToken && !tokenCopied) {
      setConfirmation("close");
      return;
    }
    finish();
  }

  async function save() {
    if (!normalizedName) {
      setError(translate("trigger.validation.nameRequired"));
      return;
    }
    setBusy(true);
    setError("");
    try {
      if (client) {
        await api.updateClient(client.id, { name: normalizedName });
        notify("ok", translate("clients.updated", { name: normalizedName }));
        onSaved();
      } else {
        const result = await api.createClient({ name: normalizedName });
        setIssuedToken(result.api_token);
        setTokenCopied(false);
        setHasToken(result.client.has_token);
        setPendingRefresh(true);
        notify("ok", translate("clients.created", { name: normalizedName }));
      }
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function rotateToken() {
    if (!client) return;
    setBusy(true);
    setError("");
    try {
      const result = await api.rotateClientToken(client.id);
      setIssuedToken(result.api_token);
      setTokenCopied(false);
      setHasToken(true);
      setPendingRefresh(true);
      notify("ok", translate("clients.tokenIssued", { name: client.name }));
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function revokeToken() {
    if (!client || !hasToken) return;
    setBusy(true);
    setError("");
    try {
      await api.revokeClientToken(client.id);
      setHasToken(false);
      setPendingRefresh(true);
      notify("ok", translate("clients.tokenRevoked", { name: client.name }));
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function copyToken() {
    try {
      await navigator.clipboard.writeText(issuedToken);
      setTokenCopied(true);
    } catch (cause) {
      setError(errorMessage(cause));
    }
  }

  async function remove() {
    if (!client) return;
    if (hasToken) {
      setError(translate("clients.revokeBeforeDelete"));
      return;
    }
    setBusy(true);
    setError("");
    try {
      await api.deleteClient(client.id);
      notify("ok", translate("clients.deleted", { name: client.name }));
      onDeleted();
    } catch (cause) {
      setError(errorMessage(cause));
      setBusy(false);
    }
  }

  function requestRotateToken() {
    if (hasToken) {
      setConfirmation("rotate");
      return;
    }
    void rotateToken();
  }

  function confirmAction() {
    const action = confirmation;
    setConfirmation(null);
    if (action === "close") {
      finish();
    } else if (action === "rotate") {
      void rotateToken();
    } else if (action === "revoke") {
      void revokeToken();
    } else if (action === "delete") {
      void remove();
    }
  }

  const confirmationDialog = confirmation ? (
    <ConfirmDialog
      title={translate(`clients.confirm.${confirmation}.title`)}
      description={
        confirmation === "delete" && client
          ? translate("clients.deleteConfirm", { name: client.name })
          : translate(
              confirmation === "close"
                ? "clients.closeWithoutCopying"
                : confirmation === "rotate"
                  ? "clients.rotateConfirm"
                  : "clients.revokeConfirm",
            )
      }
      confirmLabel={translate(`clients.confirm.${confirmation}.action`)}
      onCancel={() => setConfirmation(null)}
      onConfirm={confirmAction}
    />
  ) : null;

  if (issuedToken) {
    return (
      <>
        <Modal
          id="client-token-dialog"
          title={translate("clients.saveToken")}
          onClose={requestClose}
        >
          <div className="inlineNotice">{translate("clients.saveTokenHint")}</div>
          <Field label={translate("settings.apiToken")}>
            <div className="clientTokenCopyRow">
              <input
                className="mono"
                readOnly
                value={issuedToken}
                onFocus={(event) => event.currentTarget.select()}
              />
              <button className="button" type="button" onClick={() => void copyToken()}>
                {tokenCopied ? (
                  <Check size={16} aria-hidden="true" />
                ) : (
                  <Copy size={16} aria-hidden="true" />
                )}
                <span aria-live="polite">
                  {tokenCopied ? translate("common.copied") : translate("clients.copyToken")}
                </span>
              </button>
            </div>
          </Field>
          {error ? <div className="inlineNotice error">{error}</div> : null}
          <footer className="dialogFooter dialogFooterEnd">
            <button className="button primary" type="button" onClick={requestClose}>
              {translate("common.done")}
            </button>
          </footer>
        </Modal>
        {confirmationDialog}
      </>
    );
  }

  return (
    <>
      <Modal
        id={client ? "client-edit-dialog" : "client-register-dialog"}
        title={client ? translate("clients.edit") : translate("clients.register")}
        subtitle={client ? translate("clients.editHint") : translate("clients.registrationHint")}
        onClose={requestClose}
      >
        <div className="clientDialogForm">
          <Field label={translate("common.name")}>
            <input
              id="clientName"
              maxLength={200}
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </Field>
          {client ? (
            <>
              <section className="clientCredentialSection" aria-labelledby="client-token-heading">
                <div className="clientSectionHeading">
                  <div>
                    <h3 id="client-token-heading">{translate("clients.invocationToken")}</h3>
                    <p>{translate("clients.tokenScopeHint")}</p>
                  </div>
                  <span className={hasToken ? "badge badge-good" : "badge badge-neutral"}>
                    <span className="badgeIcon" aria-hidden="true">
                      {hasToken ? "✓" : "○"}
                    </span>
                    {hasToken
                      ? translate("workspace.status.active")
                      : translate("clients.notIssued")}
                  </span>
                </div>
                <div className="clientCredentialActions">
                  <button
                    className="button"
                    type="button"
                    disabled={busy}
                    onClick={requestRotateToken}
                  >
                    {hasToken ? translate("clients.rotateToken") : translate("clients.issueToken")}
                  </button>
                  <button
                    className="button danger"
                    type="button"
                    disabled={busy || !hasToken}
                    onClick={() => setConfirmation("revoke")}
                  >
                    {translate("clients.revokeToken")}
                  </button>
                </div>
              </section>
              <section className="dangerZone compact clientDangerZone">
                <div>
                  <strong>{translate("clients.deleteClient")}</strong>
                  <p>
                    {hasToken
                      ? translate("clients.revokeBeforeDelete")
                      : translate("clients.deleteHint")}
                  </p>
                </div>
                <button
                  className="button danger"
                  type="button"
                  disabled={busy || hasToken}
                  onClick={() => setConfirmation("delete")}
                >
                  {translate("common.delete")}
                </button>
              </section>
            </>
          ) : (
            <div className="inlineNotice">{translate("clients.registrationTokenHint")}</div>
          )}
        </div>
        {error ? <div className="inlineNotice error">{error}</div> : null}
        <footer className="dialogFooter dialogFooterEnd">
          <div className="dialogFooterActions">
            <button className="button" type="button" disabled={busy} onClick={requestClose}>
              {translate("common.cancel")}
            </button>
            <button
              className="button primary"
              type="button"
              disabled={busy || !dirty || !normalizedName}
              onClick={() => void save()}
            >
              {busy
                ? translate("common.saving")
                : client
                  ? translate("trigger.saveChanges")
                  : translate("clients.create")}
            </button>
          </div>
        </footer>
      </Modal>
      {confirmationDialog}
    </>
  );
}
