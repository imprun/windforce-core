import { useEffect, useState } from "react";
import { Layout } from "../components/Layout";
import { SettingsNav } from "../components/SettingsNav";
import { DefinitionList, Field, Panel } from "../components/ui";
import { useApp } from "../lib/app-context";

export function SettingsPage() {
  const { settings, updateSettings, api, notify } = useApp();
  const [token, setToken] = useState(settings.token);
  const [actor, setActor] = useState(settings.actor);
  const [health, setHealth] = useState<string>("checking…");

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
  }, [api]);

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
      subtitle="Authentication and audit context stored in this browser."
      actions={
        <button className="button primary" type="button" id="saveSettings" disabled={!dirty} onClick={handleSave}>
          Save settings
        </button>
      }
    >
      <SettingsNav />
      <Panel title="API access" subtitle="Credential used for control-plane requests in the active workspace.">
        <div className="formGrid">
          <Field label="API token" hint="Sent as Authorization: Bearer. Leave empty when the control plane runs without --admin-token-env.">
            <input
              id="settingsToken"
              type="password"
              value={token}
              onChange={(event) => setToken(event.target.value)}
              autoComplete="off"
            />
          </Field>
        </div>
        <DefinitionList items={[["Active workspace", settings.workspace], ["Status", health]]} />
      </Panel>

      <Panel
        title="Audit actor"
        subtitle="Recorded as the subject of releases, cancels, and other state changes. Not an authentication credential."
      >
        <div className="formGrid">
          <Field label="Actor" hint="With real authentication the actor comes from the request principal; local development defaults to local-dev.">
            <input id="settingsActor" value={actor} onChange={(event) => setActor(event.target.value)} />
          </Field>
        </div>
      </Panel>
    </Layout>
  );
}
