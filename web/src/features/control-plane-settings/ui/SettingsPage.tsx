"use client";

import { useEffect, useState } from "react";
import type { ApiSettings } from "@/shared/api/types";

type Props = {
  settings: ApiSettings;
  sourceCount: number;
  appCount: number;
  credentialCount: number;
  liveWorkers: number;
  busy: boolean;
  onSave: (settings: ApiSettings) => void;
  onRefresh: () => void;
};

export function SettingsPage({ settings, sourceCount, appCount, credentialCount, liveWorkers, busy, onSave, onRefresh }: Props) {
  const [draft, setDraft] = useState(settings);

  useEffect(() => {
    setDraft(settings);
  }, [settings]);

  return (
    <div id="settingsPage" className="settingsPage">
      <form
        id="settingsForm"
        className="workspacePanel settingsForm"
        aria-label="Control plane settings"
        onSubmit={(event) => {
          event.preventDefault();
          onSave({
            workspace: draft.workspace.trim() || "default",
            token: draft.token,
            actor: draft.actor.trim(),
          });
        }}
      >
        <header className="panelHeader">
          <div>
            <span className="eyebrow">Control plane context</span>
            <h2>Settings</h2>
            <p>Workspace, API token, and actor are applied to control-plane requests from this browser.</p>
          </div>
          <button className="button" type="button" onClick={onRefresh} disabled={busy}>
            {busy ? "Refreshing" : "Refresh"}
          </button>
        </header>

        <div className="settingsGrid">
          <label className="field">
            Workspace
            <input
              id="workspaceInput"
              value={draft.workspace}
              onChange={(event) => setDraft({ ...draft, workspace: event.target.value })}
              spellCheck={false}
            />
          </label>
          <label className="field">
            API token
            <input
              id="tokenInput"
              type="password"
              placeholder="Optional"
              value={draft.token}
              onChange={(event) => setDraft({ ...draft, token: event.target.value })}
            />
          </label>
          <label className="field">
            Actor
            <input
              id="actorInput"
              placeholder="Required for deploy"
              value={draft.actor}
              onChange={(event) => setDraft({ ...draft, actor: event.target.value })}
              spellCheck={false}
            />
          </label>
        </div>

        <div className="actions end">
          <button className="button primary" type="submit">
            Save Settings
          </button>
        </div>
      </form>

      <section className="workspacePanel settingsSummary" aria-label="Current control plane context">
        <header className="panelHeader">
          <div>
            <span className="eyebrow">Current context</span>
            <h2>{settings.workspace || "default"}</h2>
            <p>{settings.actor ? `Actor ${settings.actor}` : "Actor is not set"}</p>
          </div>
          <span className={settings.actor ? "badge ok" : "badge warn"}>{settings.actor ? "ready" : "needs actor"}</span>
        </header>
        <div className="settingsSummaryGrid">
          <ContextItem label="Workspace" value={settings.workspace || "default"} tone="ok" />
          <ContextItem label="API token" value={settings.token ? "stored in browser" : "not set"} tone={settings.token ? "ok" : "neutral"} />
          <ContextItem label="Actor" value={settings.actor || "not set"} tone={settings.actor ? "ok" : "warn"} />
          <ContextItem label="Live workers" value={String(liveWorkers)} tone={liveWorkers > 0 ? "ok" : "warn"} />
        </div>
      </section>

      <section className="workspacePanel settingsSummary" aria-label="Workspace inventory">
        <header className="panelHeader">
          <div>
            <span className="eyebrow">Workspace inventory</span>
            <h2>Control-plane state</h2>
            <p>Counts are loaded from the selected workspace.</p>
          </div>
        </header>
        <div className="settingsMetricGrid">
          <Metric label="Sources" value={sourceCount} />
          <Metric label="Apps" value={appCount} />
          <Metric label="Credentials" value={credentialCount} />
          <Metric label="Workers" value={liveWorkers} />
        </div>
      </section>
    </div>
  );
}

function ContextItem({ label, value, tone }: { label: string; value: string; tone: "ok" | "warn" | "neutral" }) {
  return (
    <div className="readinessItem">
      <span className={`statusDot ${tone}`} aria-hidden="true" />
      <div>
        <strong>{label}</strong>
        <p>{value}</p>
      </div>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: number }) {
  return (
    <div className="metricTile">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}
