import { useState } from "react";
import { Field, Modal } from "../components/ui";
import { type Client, errorMessage } from "../lib/api";
import { useApp } from "../lib/app-context";
import { translate } from "../shared/i18n";

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
  const [pendingRefresh, setPendingRefresh] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const normalizedName = name.trim();
  const dirty = !client || normalizedName !== client.name;

  function finish() {
    if (pendingRefresh) onSaved();
    else onClose();
  }

  function close() {
    if (issuedToken && !window.confirm(translate("clients.closeWithoutCopying"))) {
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
    if (hasToken && !window.confirm(translate("clients.rotateConfirm"))) {
      return;
    }
    setBusy(true);
    setError("");
    try {
      const result = await api.rotateClientToken(client.id);
      setIssuedToken(result.api_token);
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
    if (!window.confirm(translate("clients.revokeConfirm"))) {
      return;
    }
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
      notify("ok", translate("clients.tokenCopied"));
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
    if (!window.confirm(translate("clients.deleteConfirm", { name: client.name }))) return;
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

  if (issuedToken) {
    return (
      <Modal title={translate("clients.saveToken")} onClose={close}>
        <div className="inlineNotice">{translate("clients.saveTokenHint")}</div>
        <Field label={translate("settings.apiToken")}>
          <input
            className="mono"
            readOnly
            value={issuedToken}
            onFocus={(event) => event.currentTarget.select()}
          />
        </Field>
        {error ? <div className="inlineNotice error">{error}</div> : null}
        <footer className="dialogFooter">
          <span />
          <div className="dialogFooterActions">
            <button className="button" type="button" onClick={() => void copyToken()}>
              {translate("clients.copyToken")}
            </button>
            <button className="button primary" type="button" onClick={finish}>
              {translate("common.done")}
            </button>
          </div>
        </footer>
      </Modal>
    );
  }

  return (
    <Modal
      title={client ? translate("clients.edit") : translate("clients.register")}
      onClose={close}
    >
      <div className="formGrid">
        <Field label={translate("common.name")}>
          <input maxLength={200} value={name} onChange={(event) => setName(event.target.value)} />
        </Field>
        {client ? (
          <Field label={translate("clients.invocationToken")}>
            <div>
              <p>
                {hasToken ? translate("workspace.status.active") : translate("clients.notIssued")}
              </p>
              <div className="dialogFooterActions">
                <button className="button" type="button" disabled={busy} onClick={rotateToken}>
                  {hasToken ? translate("clients.rotateToken") : translate("clients.issueToken")}
                </button>
                <button
                  className="button danger"
                  type="button"
                  disabled={busy || !hasToken}
                  onClick={revokeToken}
                >
                  {translate("clients.revokeToken")}
                </button>
              </div>
              <p className="fieldHint">{translate("clients.tokenScopeHint")}</p>
            </div>
          </Field>
        ) : (
          <div className="inlineNotice">{translate("clients.registrationTokenHint")}</div>
        )}
      </div>
      {error ? <div className="inlineNotice error">{error}</div> : null}
      <footer className="dialogFooter">
        <span>
          {client ? (
            <button
              className="button danger"
              type="button"
              disabled={busy || hasToken}
              onClick={remove}
            >
              {translate("common.delete")}
            </button>
          ) : null}
        </span>
        <div className="dialogFooterActions">
          <button className="button" type="button" disabled={busy} onClick={close}>
            {translate("common.cancel")}
          </button>
          <button
            className="button primary"
            type="button"
            disabled={busy || !dirty || !normalizedName}
            onClick={save}
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
  );
}
