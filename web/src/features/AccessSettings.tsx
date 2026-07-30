import { Check, Copy, ExternalLink, ShieldCheck } from "lucide-react";
import { useEffect, useState } from "react";
import { DefinitionList, Field, Panel } from "../components/ui";
import { useApp } from "../lib/app-context";
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

export function CLIConnectionPanel() {
  const { settings } = useApp();
  const apiURL = globalThis.location?.origin || "";

  return (
    <Panel
      title={translate("settings.cliConnection")}
      subtitle={translate("settings.cliConnectionHint")}
    >
      <div className="inlineNotice accessFlowNotice">{translate("settings.cliTokenOrderHint")}</div>
      <div className="cliConnectionGrid">
        <Field
          label={translate("settings.controlPlaneURL")}
          hint={translate("settings.controlPlaneURLHint")}
        >
          <CopyableSetting label={translate("settings.controlPlaneURL")} value={apiURL} />
        </Field>
        <Field
          label={translate("settings.workspaceID")}
          hint={translate("settings.workspaceIDHint")}
        >
          <CopyableSetting label={translate("settings.workspaceID")} value={settings.workspace} />
        </Field>
      </div>
    </Panel>
  );
}

export function HostedAccessPanels({ hostConsole }: { hostConsole: HostConsoleConfig | null }) {
  const health = useControlPlaneHealth();

  return (
    <>
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
      </Panel>
      <Panel title={translate("settings.hostedCLI")} subtitle={translate("settings.hostedCLIHint")}>
        <div className="hostedCLIAction">
          <p>{translate("settings.hostedCLIBody")}</p>
          {hostConsole ? (
            <a className="button primary" href={hostConsole.url}>
              <ExternalLink aria-hidden="true" />
              {translate("settings.openHostConsole")}
            </a>
          ) : (
            <span className="inlineNotice">{translate("settings.hostConsoleUnavailable")}</span>
          )}
        </div>
      </Panel>
    </>
  );
}

export function LocalBrowserAccessPanel() {
  const { settings, updateSettings, notify } = useApp();
  const [token, setToken] = useState(settings.token);
  const [actor, setActor] = useState(settings.actor);
  const health = useControlPlaneHealth();

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
  }

  return (
    <Panel
      title={translate("settings.localAccess")}
      subtitle={translate("settings.localAccessHint")}
    >
      <div className="formGrid localAccessGrid">
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
      <div className="localAccessFooter">
        <DefinitionList items={[[translate("settings.controlPlane"), healthLabel(health)]]} />
        <button
          className="button primary"
          type="button"
          id="saveSettings"
          disabled={!dirty}
          onClick={save}
        >
          {translate("settings.saveLocalAccess")}
        </button>
      </div>
    </Panel>
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
