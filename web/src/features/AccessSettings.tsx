import { Check, Copy, ExternalLink, ShieldCheck } from "lucide-react";
import { useEffect, useState } from "react";
import { DefinitionList, Field, Modal, Panel } from "../components/ui";
import { useApp } from "../lib/app-context";
import { Link } from "../lib/router";
import type { HostConsoleConfig } from "../lib/runtime-config";
import { translate } from "../shared/i18n";

export type ControlPlaneHealth = "checking" | "ready" | "notReady" | "unreachable";

export function useControlPlaneHealth(): ControlPlaneHealth {
  const [health, setHealth] = useState<ControlPlaneHealth>("checking");

  useEffect(() => {
    let canceled = false;
    fetch("/readyz")
      .then((response) => response.json())
      .then((payload: { ready?: boolean }) => {
        if (!canceled) setHealth(payload.ready ? "ready" : "notReady");
      })
      .catch(() => {
        if (!canceled) setHealth("unreachable");
      });
    return () => {
      canceled = true;
    };
  }, []);

  return health;
}

export function healthLabel(health: ControlPlaneHealth): string {
  if (health === "ready") return translate("settings.health.ready");
  if (health === "notReady") return translate("settings.health.notReady");
  if (health === "unreachable") return translate("settings.health.unreachable");
  return translate("settings.health.checking");
}

export function CoreAPIConnectionPanel() {
  const { settings } = useApp();
  const apiURL = globalThis.location?.origin || "";
  const workspaceAPIURL = `${apiURL}/api/w/${encodeURIComponent(settings.workspace)}`;

  return (
    <Panel
      title={translate("settings.coreAPIConnection")}
      subtitle={translate("settings.coreAPIConnectionHint")}
    >
      <div className="accessScopeNotice">
        <p>{translate("settings.invocationCredentialHint")}</p>
        <Link className="button" to="/clients">
          {translate("settings.openClientRegistry")}
        </Link>
      </div>
      <div className="apiConnectionGrid">
        <Field label={translate("settings.coreURL")} hint={translate("settings.coreURLHint")}>
          <CopyableSetting label={translate("settings.coreURL")} value={apiURL} />
        </Field>
        <Field
          label={translate("settings.workspaceAPIBase")}
          hint={translate("settings.workspaceAPIBaseHint")}
        >
          <CopyableSetting label={translate("settings.workspaceAPIBase")} value={workspaceAPIURL} />
        </Field>
      </div>
    </Panel>
  );
}

export function HostedAccessPanel({ hostConsole }: { hostConsole: HostConsoleConfig | null }) {
  const health = useControlPlaneHealth();

  return (
    <Panel
      title={translate("settings.hostedAccess")}
      subtitle={translate("settings.hostedAccessHint")}
    >
      <div className="hostedAccessSummary">
        <ShieldCheck aria-hidden="true" />
        <DefinitionList
          items={[
            [translate("settings.authentication"), translate("shell.managedByHost")],
            [translate("settings.auditActor"), translate("settings.hostPrincipalActor")],
            [translate("settings.controlPlane"), healthLabel(health)],
          ]}
        />
      </div>
      <div className="hostedAccessAction">
        <p>{translate("settings.hostedAccessBody")}</p>
        {hostConsole ? (
          <a className="button primary" href={hostConsole.url}>
            <ExternalLink aria-hidden="true" />
            {translate("settings.manageAccessInHost")}
          </a>
        ) : (
          <span className="inlineNotice">{translate("settings.hostConsoleUnavailable")}</span>
        )}
      </div>
    </Panel>
  );
}

export function LocalBrowserAccessDialog({ onClose }: { onClose: () => void }) {
  const { settings, updateSettings, notify } = useApp();
  const [token, setToken] = useState(settings.token);
  const [actor, setActor] = useState(settings.actor);

  useEffect(() => {
    setToken(settings.token);
    setActor(settings.actor);
  }, [settings]);

  const dirty = token !== settings.token || actor !== settings.actor;

  function save() {
    updateSettings({
      workspace: settings.workspace,
      token: token.trim(),
      actor: actor.trim(),
    });
    notify("ok", translate("settings.saved"));
    onClose();
  }

  return (
    <Modal
      title={translate("shell.localAccess")}
      subtitle={translate("settings.localAccessHint")}
      onClose={onClose}
      compact
    >
      <form
        onSubmit={(event) => {
          event.preventDefault();
          if (dirty) save();
        }}
      >
        <div className="formGrid">
          <Field label={translate("settings.apiToken")} hint={translate("settings.apiTokenHint")}>
            <input
              id="settingsToken"
              type="password"
              value={token}
              onChange={(event) => setToken(event.target.value)}
              autoComplete="off"
            />
          </Field>
          <Field label={translate("settings.actor")} hint={translate("settings.actorHint")}>
            <input
              id="settingsActor"
              value={actor}
              onChange={(event) => setActor(event.target.value)}
            />
          </Field>
        </div>
        <footer className="dialogFooter dialogFooterEnd">
          <button className="button" type="button" onClick={onClose}>
            {translate("common.cancel")}
          </button>
          <button className="button primary" type="submit" id="saveSettings" disabled={!dirty}>
            {translate("settings.saveLocalAccess")}
          </button>
        </footer>
      </form>
    </Modal>
  );
}

function CopyableSetting({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false);

  return (
    <div className="copyField">
      <code title={value}>{value}</code>
      <button
        className="button small"
        type="button"
        title={translate("common.copyNamed", { name: label })}
        aria-label={translate("common.copyNamed", { name: label })}
        onClick={async () => {
          await navigator.clipboard.writeText(value);
          setCopied(true);
        }}
      >
        {copied ? <Check size={16} aria-hidden="true" /> : <Copy size={16} aria-hidden="true" />}
        {copied ? translate("common.copied") : translate("common.copy")}
      </button>
    </div>
  );
}
