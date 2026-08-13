import { useState } from "react";
import { DefinitionList, Field, Modal, Panel, SelectControl } from "../components/ui";
import { type Client, errorMessage } from "../lib/api";
import { useApp } from "../lib/app-context";
import { translate } from "../shared/i18n";

export function ClientInvocationPolicy({
  client,
  onUpdated,
}: {
  client: Client;
  onUpdated: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const policy = client.invocation_policy;
  const targets = policy.allowed_targets.length
    ? policy.allowed_targets.join(", ")
    : policy.mode === "all"
      ? translate("clients.policy.allTargets")
      : translate("clients.policy.noTargets");

  return (
    <>
      <Panel
        title={translate("clients.policy.title")}
        subtitle={translate("clients.policy.subtitle")}
        actions={
          <button
            className="button"
            type="button"
            data-ui-guide="edit-client-invocation-policy"
            onClick={() => setEditing(true)}
          >
            {translate("clients.policy.edit")}
          </button>
        }
      >
        <DefinitionList
          items={[
            [
              translate("clients.policy.mode"),
              translate(
                policy.mode === "all" ? "clients.policy.modeAll" : "clients.policy.modeRestricted",
              ),
            ],
            [translate("clients.policy.allowedTargets"), <span className="mono">{targets}</span>],
            [translate("clients.policy.revision"), <span className="mono">{policy.revision}</span>],
          ]}
        />
      </Panel>
      {editing ? (
        <ClientInvocationPolicyDialog
          client={client}
          onClose={() => setEditing(false)}
          onSaved={() => {
            setEditing(false);
            onUpdated();
          }}
        />
      ) : null}
    </>
  );
}

function ClientInvocationPolicyDialog({
  client,
  onClose,
  onSaved,
}: {
  client: Client;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { api, notify } = useApp();
  const [mode, setMode] = useState(client.invocation_policy.mode);
  const [targetText, setTargetText] = useState(client.invocation_policy.allowed_targets.join("\n"));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const targets = normalizeAllowedTargets(targetText);

  async function save() {
    setBusy(true);
    setError("");
    try {
      await api.updateClientInvocationPolicy(client.id, {
        operation_id: `policy_${crypto.randomUUID()}`,
        expected_revision: client.invocation_policy.revision,
        mode,
        allowed_targets: mode === "all" ? [] : targets,
      });
      notify("ok", translate("clients.policy.saved"));
      onSaved();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      id="client-invocation-policy-dialog"
      title={translate("clients.policy.editTitle")}
      subtitle={translate("clients.policy.editHint")}
      onClose={onClose}
      wide
    >
      <div className="formGrid">
        <Field label={translate("clients.policy.mode")}>
          <SelectControl
            value={mode}
            onChange={setMode}
            ariaLabel={translate("clients.policy.mode")}
            options={[
              {
                value: "all",
                label: translate("clients.policy.modeAll"),
                description: translate("clients.policy.modeAllHint"),
              },
              {
                value: "restricted",
                label: translate("clients.policy.modeRestricted"),
                description: translate("clients.policy.modeRestrictedHint"),
              },
            ]}
          />
        </Field>
        {mode === "restricted" ? (
          <Field
            label={translate("clients.policy.allowedTargets")}
            hint={translate("clients.policy.targetsHint")}
          >
            <textarea
              rows={8}
              spellCheck={false}
              autoCapitalize="none"
              value={targetText}
              placeholder={"orders\norders/create"}
              onChange={(event) => setTargetText(event.target.value)}
            />
          </Field>
        ) : null}
      </div>
      {mode === "restricted" && targets.length === 0 ? (
        <div className="inlineNotice">{translate("clients.policy.denyAllHint")}</div>
      ) : null}
      <div className="inlineNotice">{translate("clients.policy.newRunsHint")}</div>
      {error ? <div className="inlineNotice error">{error}</div> : null}
      <footer className="dialogFooter dialogFooterEnd">
        <button className="button" type="button" disabled={busy} onClick={onClose}>
          {translate("common.cancel")}
        </button>
        <button className="button primary" type="button" disabled={busy} onClick={save}>
          {busy ? translate("common.saving") : translate("common.saveChanges")}
        </button>
      </footer>
    </Modal>
  );
}

export function normalizeAllowedTargets(value: string): string[] {
  return [
    ...new Set(
      value
        .split(/[\n,]/)
        .map((item) => item.trim())
        .filter(Boolean),
    ),
  ].sort();
}
