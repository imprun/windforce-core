import { Lock, Pencil, Unlock } from "lucide-react";
import type { ReactNode } from "react";
import type { InputConfig } from "../lib/api";
import { formatRelative, formatTime } from "../lib/format";

export function formatInputSettingValue(value: unknown): string {
  return JSON.stringify(value, null, 2) ?? String(value);
}

export type InputSettingScopeItem = {
  key: string;
  config: InputConfig;
  primaryLabel: string;
  primaryValue: ReactNode;
  primaryMeta: string;
  actionName: string;
  actionMeta: string;
  editLabel: string;
  editDisabled?: boolean;
  onEdit: () => void;
};

export function InputSettingScopeList({
  id,
  items,
}: {
  id: string;
  items: InputSettingScopeItem[];
}) {
  return (
    <div className="inputSettingsList" id={id}>
      {items.map((item) => (
        <section className="inputSettingScope" key={item.key}>
          <header className="inputSettingScopeHeader">
            <div className="inputSettingFact inputSettingPrimaryScope">
              <span className="inputSettingFactLabel">{item.primaryLabel}</span>
              <span className="inputSettingFactValue">{item.primaryValue}</span>
              <span className="inputSettingFactMeta">{item.primaryMeta}</span>
            </div>
            <div className="inputSettingFact inputSettingActionScope">
              <span className="inputSettingFactLabel">Action scope</span>
              <span className="inputSettingFactValue">{item.actionName}</span>
              <span className="inputSettingFactMeta mono">{item.actionMeta}</span>
            </div>
            <div
              className="inputSettingFact inputSettingChange"
              title={formatTime(item.config.updated_at)}
            >
              <span className="inputSettingFactLabel">Last change</span>
              <span className="inputSettingFactValue">
                {formatRelative(item.config.updated_at)}
              </span>
              <span className="inputSettingFactMeta">
                {formatTime(item.config.updated_at)} · {item.config.updated_by}
              </span>
            </div>
            <button
              className="button small iconButton inputSettingEdit"
              type="button"
              title="Edit input settings"
              aria-label={item.editLabel}
              disabled={item.editDisabled}
              onClick={item.onEdit}
            >
              <Pencil size={15} aria-hidden="true" />
            </button>
          </header>

          <table className="inputSettingValues" aria-label="Applied input values">
            <thead>
              <tr className="inputSettingValuesHeader">
                <th scope="col">Input key</th>
                <th scope="col">Applied value</th>
                <th scope="col">Request policy</th>
              </tr>
            </thead>
            <tbody>
              {Object.entries(item.config.config).map(([key, value]) => {
                const locked = item.config.locked_keys.includes(key);
                return (
                  <tr className="inputSettingValueRow" key={key}>
                    <td className="inputSettingValueCell">
                      <span className="inputSettingFieldLabel">Input key</span>
                      <code className="inputSettingKey">{key}</code>
                    </td>
                    <td className="inputSettingValueCell">
                      <span className="inputSettingFieldLabel">Applied value</span>
                      <pre className="inputSettingValue">{formatInputSettingValue(value)}</pre>
                    </td>
                    <td className="inputSettingValueCell">
                      <span className="inputSettingFieldLabel">Request policy</span>
                      <span className={locked ? "inputSettingPolicy locked" : "inputSettingPolicy"}>
                        {locked ? (
                          <Lock size={14} aria-hidden="true" />
                        ) : (
                          <Unlock size={14} aria-hidden="true" />
                        )}
                        {locked ? "Request cannot override" : "Request may override"}
                      </span>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </section>
      ))}
    </div>
  );
}
