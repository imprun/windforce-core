import { useState } from "react";
import { Field, Modal } from "../components/ui";
import { type Client, errorMessage } from "../lib/api";
import { useApp } from "../lib/app-context";

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
  const [externalKey, setExternalKey] = useState(client?.external_key || "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const normalizedName = name.trim();
  const normalizedKey = externalKey.trim();
  const keyValid = normalizedKey !== "" && !/\s/u.test(normalizedKey);
  const dirty = !client || normalizedName !== client.name || normalizedKey !== client.external_key;

  async function save() {
    if (!normalizedName) {
      setError("Name is required.");
      return;
    }
    if (!keyValid) {
      setError("External key is required and must not contain whitespace.");
      return;
    }
    setBusy(true);
    setError("");
    try {
      if (client) {
        await api.updateClient(client.id, { name: normalizedName, external_key: normalizedKey });
        notify("ok", `Updated ${normalizedName}.`);
      } else {
        await api.createClient({ name: normalizedName, external_key: normalizedKey });
        notify("ok", `Created ${normalizedName}.`);
      }
      onSaved();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    if (!client) return;
    if (!window.confirm(`Delete client ${client.name}?`)) return;
    setBusy(true);
    setError("");
    try {
      await api.deleteClient(client.id);
      notify("ok", `Deleted ${client.name}.`);
      onDeleted();
    } catch (cause) {
      setError(errorMessage(cause));
      setBusy(false);
    }
  }

  return (
    <Modal title={client ? "Edit Client" : "Register Client"} onClose={onClose}>
      <div className="formGrid">
        <Field label="Name">
          <input maxLength={200} value={name} onChange={(event) => setName(event.target.value)} />
        </Field>
        <Field label="External key">
          <div>
            <input
              className="mono"
              maxLength={512}
              autoComplete="off"
              spellCheck={false}
              value={externalKey}
              onChange={(event) => setExternalKey(event.target.value)}
            />
            <p className="fieldHint">
              Identifies the external client for app and action settings. This is not a Windforce
              API credential.
            </p>
          </div>
        </Field>
      </div>
      {error ? <div className="inlineNotice error">{error}</div> : null}
      <footer className="dialogFooter">
        <span>
          {client ? (
            <button className="button danger" type="button" disabled={busy} onClick={remove}>
              Delete
            </button>
          ) : null}
        </span>
        <div className="dialogFooterActions">
          <button className="button" type="button" disabled={busy} onClick={onClose}>
            Cancel
          </button>
          <button
            className="button primary"
            type="button"
            disabled={busy || !dirty || !normalizedName || !keyValid}
            onClick={save}
          >
            {busy ? "Saving…" : client ? "Save changes" : "Create client"}
          </button>
        </div>
      </footer>
    </Modal>
  );
}
