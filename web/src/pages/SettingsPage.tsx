import { Copy } from "lucide-react";
import { useEffect, useState } from "react";
import { Layout } from "../components/Layout";
import { SettingsNav } from "../components/SettingsNav";
import { DefinitionList, Field, Panel } from "../components/ui";
import { useApp } from "../lib/app-context";

export const CLI_TOKEN_ENV = "WINDFORCE_CORE_API_TOKEN";

export function cliProfileCommand(apiURL: string, workspace: string): string {
  return `windforce profile set ${workspace} --api-url "${apiURL}" --workspace ${workspace} --token-env ${CLI_TOKEN_ENV} --use`;
}

export function SettingsPage() {
  const { settings, updateSettings, notify } = useApp();
  const [token, setToken] = useState(settings.token);
  const [actor, setActor] = useState(settings.actor);
  const [health, setHealth] = useState<string>("checking…");
  const apiURL = globalThis.location?.origin || "";
  const profileCommand = cliProfileCommand(apiURL, settings.workspace);

  useEffect(() => {
    setToken(settings.token);
    setActor(settings.actor);
  }, [settings]);

  useEffect(() => {
    let canceled = false;
    fetch("/readyz")
      .then((response) => response.json())
      .then((payload: { ready?: boolean }) => {
        if (!canceled) setHealth(payload.ready ? "control plane ready" : "control plane not ready");
      })
      .catch(() => {
        if (!canceled) setHealth("control plane unreachable");
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
    notify("ok", "Settings saved.");
  }

  return (
    <Layout
      title="Settings"
      subtitle="CLI connection details, browser authentication, and audit context."
      actions={
        <button
          className="button primary"
          type="button"
          id="saveSettings"
          disabled={!dirty}
          onClick={handleSave}
        >
          Save settings
        </button>
      }
    >
      <SettingsNav />
      <Panel
        title="CLI connection"
        subtitle="Non-secret connection details for the active workspace."
      >
        <div className="cliConnectionGrid">
          <Field label="Control plane URL" hint="The root address currently used by this browser.">
            <CopyableSetting label="control plane URL" value={apiURL} />
          </Field>
          <Field label="Workspace ID" hint="Passed to the CLI as --workspace.">
            <CopyableSetting label="workspace ID" value={settings.workspace} />
          </Field>
          <Field
            label="Token environment"
            hint="Set this variable to the one-time token issued from the workspace Access tab."
          >
            <CopyableSetting label="token environment variable" value={CLI_TOKEN_ENV} />
          </Field>
        </div>
        <Field
          label="Profile command"
          hint="The token value is intentionally excluded from this command."
        >
          <div className="cliProfileCommand">
            <CopyableSetting label="CLI profile command" value={profileCommand} />
          </div>
        </Field>
      </Panel>

      <Panel
        title="Browser API access"
        subtitle="Credential used by this Web UI for requests in the active workspace."
      >
        <div className="formGrid">
          <Field
            label="API token"
            hint="Paste the workspace token here only when this browser must authenticate its requests."
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
        <DefinitionList items={[["Status", health]]} />
      </Panel>

      <Panel
        title="Audit actor"
        subtitle="Recorded as the subject of releases, cancels, and other state changes. Not an authentication credential."
      >
        <div className="formGrid">
          <Field
            label="Actor"
            hint="With real authentication the actor comes from the request principal; local development defaults to local-dev."
          >
            <input
              id="settingsActor"
              value={actor}
              onChange={(event) => setActor(event.target.value)}
            />
          </Field>
        </div>
      </Panel>
    </Layout>
  );
}

function CopyableSetting({ label, value }: { label: string; value: string }) {
  const { notify } = useApp();

  return (
    <div className="copyField">
      <code title={value}>{value}</code>
      <button
        className="button iconButton"
        type="button"
        title={`Copy ${label}`}
        aria-label={`Copy ${label}`}
        onClick={async () => {
          await navigator.clipboard.writeText(value);
          notify("ok", `${label} copied.`);
        }}
      >
        <Copy size={16} aria-hidden="true" />
      </button>
    </div>
  );
}
