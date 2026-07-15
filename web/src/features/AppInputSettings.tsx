import { Lock, Pencil, Plus, Unlock } from "lucide-react";
import { useMemo, useState } from "react";
import { EmptyState, ErrorNotice, Loading, Panel } from "../components/ui";
import { type AppDetail, type InputConfig } from "../lib/api";
import { actionDisplayName } from "../lib/action-label";
import { useApp, useAsync } from "../lib/app-context";
import { formatRelative, formatTime } from "../lib/format";
import { Link } from "../lib/router";
import { InputConfigDialog } from "./InputConfigDialog";

export function formatInputSettingValue(value: unknown): string {
  return JSON.stringify(value, null, 2) ?? String(value);
}

export function AppInputSettings({ detail }: { detail: AppDetail }) {
  const { api } = useApp();
  const [editing, setEditing] = useState<InputConfig | "new" | null>(null);
  const state = useAsync(
    async () => {
      const [configs, clients, audit] = await Promise.all([
        api.appInputConfigs(detail.app.app_key),
        api.clients(),
        api.appInputConfigAudit(detail.app.app_key),
      ]);
      return { configs, clients, audit };
    },
    [api, detail.app.app_key],
  );
  const clientsByID = useMemo(
    () => new Map((state.data?.clients || []).map((client) => [client.id, client])),
    [state.data?.clients],
  );
  const actionsByKey = useMemo(
    () => new Map(detail.actions.map((action) => [action.action_key, action])),
    [detail.actions],
  );

  function finish() {
    setEditing(null);
    state.reload();
  }

  return (
    <>
      <Panel
        title="Input settings"
        subtitle="Values applied before execution. Locked values cannot be overridden by the incoming request."
        actions={
          <button className="button primary" type="button" onClick={() => setEditing("new")}>
            <Plus size={16} aria-hidden="true" />
            Add settings
          </button>
        }
      >
        {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
        {state.loading && !state.data ? <Loading /> : null}
        {state.data && state.data.configs.length === 0 ? (
          <EmptyState title="No input settings for this app." />
        ) : null}
        {state.data && state.data.configs.length > 0 ? (
          <div className="inputSettingsList" id="appInputSettings">
            {state.data.configs.map((config) => {
              const client = config.client_id ? clientsByID.get(config.client_id) : undefined;
              const action = config.action_key ? actionsByKey.get(config.action_key) : undefined;
              return (
                <section className="inputSettingScope" key={`${config.client_id || "default"}-${config.action_key || "all"}`}>
                  <header className="inputSettingScopeHeader">
                    <div className="inputSettingFact inputSettingClientScope">
                      <span className="inputSettingFactLabel">Client scope</span>
                      {client ? (
                        <Link className="inputSettingFactValue" to={`/clients/${client.id}`}>
                          {client.name}
                        </Link>
                      ) : (
                        <span className="inputSettingFactValue">All clients</span>
                      )}
                      <span className="inputSettingFactMeta">{client ? "Client override" : "App default"}</span>
                    </div>
                    <div className="inputSettingFact inputSettingActionScope">
                      <span className="inputSettingFactLabel">Action scope</span>
                      <span className="inputSettingFactValue">
                        {action ? actionDisplayName(action.display_name) || action.action_key : "All actions"}
                      </span>
                      <span className="inputSettingFactMeta mono">{config.action_key || "App default"}</span>
                    </div>
                    <div className="inputSettingFact inputSettingChange" title={formatTime(config.updated_at)}>
                      <span className="inputSettingFactLabel">Last change</span>
                      <span className="inputSettingFactValue">{formatRelative(config.updated_at)}</span>
                      <span className="inputSettingFactMeta">
                        {formatTime(config.updated_at)} · {config.updated_by}
                      </span>
                    </div>
                    <button
                      className="button small iconButton inputSettingEdit"
                      type="button"
                      title="Edit input settings"
                      aria-label={`Edit input settings for ${client?.name || "all clients"}, ${action ? actionDisplayName(action.display_name) || action.action_key : "all actions"}`}
                      onClick={() => setEditing(config)}
                    >
                      <Pencil size={15} aria-hidden="true" />
                    </button>
                  </header>

                  <div className="inputSettingValues" role="table" aria-label="Applied input values">
                    <div className="inputSettingValuesHeader" role="row">
                      <span role="columnheader">Input key</span>
                      <span role="columnheader">Applied value</span>
                      <span role="columnheader">Request policy</span>
                    </div>
                    {Object.entries(config.config).map(([key, value]) => {
                      const locked = config.locked_keys.includes(key);
                      return (
                        <div className="inputSettingValueRow" role="row" key={key}>
                          <div className="inputSettingValueCell" role="cell">
                            <span className="inputSettingFieldLabel">Input key</span>
                            <code className="inputSettingKey">{key}</code>
                          </div>
                          <div className="inputSettingValueCell" role="cell">
                            <span className="inputSettingFieldLabel">Applied value</span>
                            <pre className="inputSettingValue">{formatInputSettingValue(value)}</pre>
                          </div>
                          <div className="inputSettingValueCell" role="cell">
                            <span className="inputSettingFieldLabel">Request policy</span>
                            <span className={locked ? "inputSettingPolicy locked" : "inputSettingPolicy"}>
                              {locked ? <Lock size={14} aria-hidden="true" /> : <Unlock size={14} aria-hidden="true" />}
                              {locked ? "Request cannot override" : "Request may override"}
                            </span>
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </section>
              );
            })}
          </div>
        ) : null}
      </Panel>

      {state.data?.audit.length ? (
        <Panel title="Input settings audit" subtitle="Configuration changes without stored input values.">
          <div className="tableWrap">
            <table className="table" id="appInputSettingsAudit">
              <thead>
                <tr>
                  <th>When</th>
                  <th>Actor</th>
                  <th>Scope</th>
                  <th>Change</th>
                  <th>Summary</th>
                </tr>
              </thead>
              <tbody>
                {state.data.audit.slice(0, 20).map((record) => (
                  <tr key={record.id}>
                    <td title={formatTime(record.created_at)}>{formatRelative(record.created_at)}</td>
                    <td>{record.actor}</td>
                    <td className="mono">
                      {record.client_id ? clientsByID.get(record.client_id)?.name || record.client_id : "all clients"} / {record.action_key || "all actions"}
                    </td>
                    <td>{record.kind}</td>
                    <td className="mono">{record.detail || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Panel>
      ) : null}

      {editing && state.data ? (
        <InputConfigDialog
          appKey={detail.app.app_key}
          actions={detail.actions}
          clients={state.data.clients}
          existing={editing === "new" ? undefined : editing}
          onClose={() => setEditing(null)}
          onSaved={finish}
        />
      ) : null}
    </>
  );
}
