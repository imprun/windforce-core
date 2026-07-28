import { Copy } from "lucide-react";
import { useEffect, useState } from "react";
import { Layout } from "../components/Layout";
import { SettingsNav } from "../components/SettingsNav";
import { DefinitionList, Field, Panel } from "../components/ui";
import { useApp } from "../lib/app-context";
import { translate } from "../shared/i18n";

export const CLI_TOKEN_ENV = "WINDFORCE_CORE_API_TOKEN";

export function cliProfileCommand(apiURL: string, workspace: string): string {
  return `windforce profile set ${workspace} --api-url "${apiURL}" --workspace ${workspace} --token-env ${CLI_TOKEN_ENV} --use`;
}

export function SettingsPage() {
  const { settings, updateSettings, notify, runtimeConfig } = useApp();
  const [token, setToken] = useState(settings.token);
  const [actor, setActor] = useState(settings.actor);
  const [health, setHealth] = useState<"checking" | "ready" | "notReady" | "unreachable">(
    "checking",
  );
  const apiURL = globalThis.location?.origin || "";
  const profileCommand = cliProfileCommand(apiURL, settings.workspace);
  const hosted = Boolean(runtimeConfig?.hostAccount);

  useEffect(() => {
    setToken(settings.token);
    setActor(settings.actor);
  }, [settings]);

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

  const dirty = token !== settings.token || actor !== settings.actor;

  function handleSave() {
    updateSettings({
      workspace: settings.workspace,
      token: token.trim(),
      actor: actor.trim(),
    });
    notify("ok", translate("settings.saved"));
  }

  return (
    <Layout
      title={translate("navigation.settings")}
      subtitle={translate("settings.subtitle")}
      actions={
        hosted ? null : (
          <button
            className="button primary"
            type="button"
            id="saveSettings"
            disabled={!dirty}
            onClick={handleSave}
          >
            {translate("settings.save")}
          </button>
        )
      }
    >
      <SettingsNav />
      <Panel
        title={translate("settings.cliConnection")}
        subtitle={translate("settings.cliConnectionHint")}
      >
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
          <Field
            label={translate("settings.tokenEnvironment")}
            hint={translate("settings.tokenEnvironmentHint")}
          >
            <CopyableSetting label={translate("settings.tokenEnvironment")} value={CLI_TOKEN_ENV} />
          </Field>
        </div>
        <Field
          label={translate("settings.profileCommand")}
          hint={translate("settings.profileCommandHint")}
        >
          <div className="cliProfileCommand">
            <CopyableSetting label={translate("settings.profileCommand")} value={profileCommand} />
          </div>
        </Field>
      </Panel>

      {hosted ? (
        <Panel
          title={translate("settings.hostedAccess")}
          subtitle={translate("settings.hostedAccessHint")}
        >
          <DefinitionList
            items={[
              [translate("settings.authentication"), translate("shell.managedByHost")],
              [translate("settings.auditActor"), translate("settings.hostPrincipalActor")],
              [translate("settings.controlPlane"), healthLabel(health)],
            ]}
          />
        </Panel>
      ) : (
        <>
          <Panel
            title={translate("settings.browserAPIAccess")}
            subtitle={translate("settings.browserAPIAccessHint")}
          >
            <div className="formGrid">
              <Field
                label={translate("settings.apiToken")}
                hint={translate("settings.apiTokenHint")}
              >
                <input
                  id="settingsToken"
                  type="password"
                  value={token}
                  onChange={(event) => setToken(event.target.value)}
                  autoComplete="off"
                />
              </Field>
            </div>
            <DefinitionList items={[[translate("common.status"), healthLabel(health)]]} />
          </Panel>

          <Panel
            title={translate("settings.auditActor")}
            subtitle={translate("settings.auditActorHint")}
          >
            <div className="formGrid">
              <Field label={translate("settings.actor")} hint={translate("settings.actorHint")}>
                <input
                  id="settingsActor"
                  value={actor}
                  onChange={(event) => setActor(event.target.value)}
                />
              </Field>
            </div>
          </Panel>
        </>
      )}
    </Layout>
  );
}

function healthLabel(health: "checking" | "ready" | "notReady" | "unreachable"): string {
  if (health === "ready") return translate("settings.health.ready");
  if (health === "notReady") return translate("settings.health.notReady");
  if (health === "unreachable") return translate("settings.health.unreachable");
  return translate("settings.health.checking");
}

function CopyableSetting({ label, value }: { label: string; value: string }) {
  const { notify } = useApp();

  return (
    <div className="copyField">
      <code title={value}>{value}</code>
      <button
        className="button iconButton"
        type="button"
        title={translate("common.copyNamed", { name: label })}
        aria-label={translate("common.copyNamed", { name: label })}
        onClick={async () => {
          await navigator.clipboard.writeText(value);
          notify("ok", translate("common.copiedNamed", { name: label }));
        }}
      >
        <Copy size={16} aria-hidden="true" />
      </button>
    </div>
  );
}
