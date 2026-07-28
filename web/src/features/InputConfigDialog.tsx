import { BookOpen, Lock, Plus, Save, Trash2, Unlock } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Field, Modal, SelectControl } from "../components/ui";
import { actionDisplayName } from "../lib/action-label";
import {
  type ActionView,
  type Client,
  errorMessage,
  type InputConfig,
  type InputConfigPayload,
} from "../lib/api";
import { useApp } from "../lib/app-context";
import {
  formatInputSettingExample,
  type InputSettingDefinition,
  inputSettingDefinitions,
  validateInputSettingValue,
} from "../lib/input-setting-schema";
import { formatSchemaValue } from "../lib/schema-document";
import { translate } from "../shared/i18n";

export type InputConfigRow = {
  key: string;
  valueText: string;
  locked: boolean;
  custom?: boolean;
};

export function inputConfigRows(config?: InputConfig): InputConfigRow[] {
  if (!config) return [{ key: "", valueText: "", locked: false }];
  return Object.entries(config.config).map(([key, value]) => ({
    key,
    valueText: formatInputSettingExample(value),
    locked: config.locked_keys.includes(key),
  }));
}

export function inputConfigPayload(
  rows: InputConfigRow[],
  actionKey: string,
  clientID: string,
  definitions: InputSettingDefinition[] = [],
): InputConfigPayload {
  const config: Record<string, unknown> = {};
  const lockedKeys: string[] = [];
  for (const row of rows) {
    const key = row.key.trim();
    if (!key) continue;
    if (Object.hasOwn(config, key)) {
      throw new Error(translate("inputSettings.duplicateKey", { key }));
    }
    try {
      config[key] = JSON.parse(row.valueText);
    } catch {
      throw new Error(translate("inputSettings.valueMustBeJSON", { key }));
    }
    const definition = definitions.find((candidate) => candidate.key === key);
    if (definition) {
      const validationError = validateInputSettingValue(definition, config[key]);
      if (validationError) throw new Error(validationError);
    }
    if (row.locked) lockedKeys.push(key);
  }
  return {
    action_key: actionKey,
    client_id: clientID || undefined,
    config,
    locked_keys: lockedKeys,
  };
}

export function InputConfigDialog({
  appKey,
  actions,
  clients,
  existing,
  fixedClientID,
  onClose,
  onSaved,
}: {
  appKey: string;
  actions: ActionView[];
  clients: Client[];
  existing?: InputConfig;
  fixedClientID?: string;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { api, notify } = useApp();
  const [actionKey, setActionKey] = useState(existing?.action_key || "");
  const [clientID, setClientID] = useState(fixedClientID ?? existing?.client_id ?? "");
  const [rows, setRows] = useState<InputConfigRow[]>(() => inputConfigRows(existing));
  const [schemaState, setSchemaState] = useState<{
    input: unknown;
    operator: unknown;
    loading: boolean;
    error: string;
  }>({ input: {}, operator: {}, loading: false, error: "" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const identityLocked = Boolean(existing);
  const definitions = useMemo(
    () => inputSettingDefinitions(schemaState.input, schemaState.operator),
    [schemaState.input, schemaState.operator],
  );
  const operatorDefinitions = definitions.filter((definition) => definition.source === "operator");
  const requestDefinitions = definitions.filter((definition) => definition.source === "request");

  useEffect(() => {
    let active = true;
    if (!actionKey) {
      setSchemaState({ input: {}, operator: {}, loading: false, error: "" });
      return () => {
        active = false;
      };
    }
    setSchemaState((current) => ({ ...current, loading: true, error: "" }));
    api
      .actionSchemas(appKey, actionKey)
      .then((schemas) => {
        if (!active) return;
        setSchemaState({
          input: schemas.input_schema,
          operator: schemas.operator_settings_schema,
          loading: false,
          error: "",
        });
      })
      .catch((cause) => {
        if (!active) return;
        setSchemaState({ input: {}, operator: {}, loading: false, error: errorMessage(cause) });
      });
    return () => {
      active = false;
    };
  }, [actionKey, api, appKey]);

  function updateRow(index: number, patch: Partial<InputConfigRow>) {
    setRows((current) =>
      current.map((row, rowIndex) => (rowIndex === index ? { ...row, ...patch } : row)),
    );
  }

  async function save() {
    setError("");
    let payload: InputConfigPayload;
    try {
      payload = inputConfigPayload(rows, actionKey, clientID, definitions);
    } catch (cause) {
      setError(errorMessage(cause));
      return;
    }
    setBusy(true);
    try {
      await api.setInputConfig(appKey, payload);
      notify(
        "ok",
        translate("inputSettings.savedFor", {
          app: appKey,
          action: actionKey ? ` / ${actionKey}` : "",
        }),
      );
      onSaved();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    if (!existing || !window.confirm(translate("inputSettings.deleteLayerConfirm"))) return;
    setBusy(true);
    setError("");
    try {
      await api.deleteInputConfig(appKey, existing.action_key, existing.client_id || "");
      notify("ok", translate("inputSettings.deleted"));
      onSaved();
    } catch (cause) {
      setError(errorMessage(cause));
      setBusy(false);
    }
  }

  const fixedClient = clients.find((client) => client.id === fixedClientID);
  const fixedClientLabel =
    fixedClientID === ""
      ? translate("inputSettings.allClientsDefaultShort")
      : fixedClient?.name || fixedClientID;
  return (
    <Modal
      title={existing ? translate("inputSettings.editTitle") : translate("inputSettings.addTitle")}
      subtitle={translate("inputSettings.dialogSubtitle", { app: appKey })}
      onClose={onClose}
      wide
    >
      <div className="formGrid">
        <Field label={translate("inputSettings.clientScope")}>
          {fixedClientID !== undefined ? (
            <input value={fixedClientLabel} disabled />
          ) : (
            <SelectControl
              value={clientID}
              disabled={identityLocked}
              onChange={setClientID}
              ariaLabel={translate("inputSettings.clientScope")}
              options={[
                { value: "", label: translate("inputSettings.allClientsDefaultShort") },
                ...clients.map((client) => ({ value: client.id, label: client.name })),
              ]}
            />
          )}
        </Field>
        <Field label={translate("inputSettings.actionScope")}>
          <SelectControl
            value={actionKey}
            disabled={identityLocked}
            onChange={setActionKey}
            ariaLabel={translate("inputSettings.actionScope")}
            options={[
              { value: "", label: translate("inputSettings.allActionsDefault") },
              ...actions.map((action) => ({
                value: action.action_key,
                label: `${actionDisplayName(action.display_name) || action.action_key} · ${action.action_key}`,
              })),
            ]}
          />
        </Field>
      </div>

      {!actionKey ? (
        <div className="inlineNotice info">{translate("inputSettings.chooseActionHint")}</div>
      ) : schemaState.loading ? (
        <div className="inlineNotice info">{translate("inputSettings.loadingDocumented")}</div>
      ) : schemaState.error ? (
        <div className="inlineNotice warning">
          {translate("inputSettings.documentedLoadFailed")} {schemaState.error}
        </div>
      ) : definitions.length === 0 ? (
        <div className="inlineNotice info">{translate("inputSettings.noDocumentedSettings")}</div>
      ) : (
        <div className="inputConfigCatalogSummary">
          <BookOpen size={16} aria-hidden="true" />
          <span>
            {translate("inputSettings.documentedSummary", {
              operator: operatorDefinitions.length,
              request: requestDefinitions.length,
            })}
          </span>
        </div>
      )}

      <div className="inputConfigEditor">
        {rows.map((row, index) => {
          const definition = definitions.find((candidate) => candidate.key === row.key);
          const custom = row.custom || Boolean(row.key && !definition);
          return (
            <div className="inputConfigRow" key={index}>
              <div className="inputConfigKeyField">
                <label htmlFor={`input-setting-key-${index}`}>
                  {translate("inputSettings.settingKey")}
                </label>
                {actionKey && !schemaState.loading && definitions.length > 0 ? (
                  <SelectControl
                    id={`input-setting-key-${index}`}
                    value={custom ? "__custom__" : row.key}
                    onChange={(key) => {
                      if (key === "__custom__") {
                        updateRow(index, {
                          key: definition ? "" : row.key,
                          custom: true,
                          valueText: definition ? "" : row.valueText,
                        });
                        return;
                      }
                      const selected = definitions.find((candidate) => candidate.key === key);
                      updateRow(index, {
                        key,
                        custom: false,
                        valueText: selected ? formatInputSettingExample(selected.example) : "",
                      });
                    }}
                    ariaLabel={translate("inputSettings.settingKeyNamed", { index: index + 1 })}
                    options={[
                      { value: "", label: translate("inputSettings.selectDocumentedKey") },
                      ...operatorDefinitions.map((candidate) => ({
                        value: candidate.key,
                        label: `${candidate.title ? `${candidate.title} · ` : ""}${candidate.key}`,
                        description: translate("inputSettings.operatorSetting"),
                      })),
                      ...requestDefinitions.map((candidate) => ({
                        value: candidate.key,
                        label: `${candidate.title ? `${candidate.title} · ` : ""}${candidate.key}`,
                        description: translate("inputSettings.requestField"),
                      })),
                      { value: "__custom__", label: translate("inputSettings.customKey") },
                    ]}
                  />
                ) : (
                  <input
                    id={`input-setting-key-${index}`}
                    className="mono"
                    value={row.key}
                    placeholder="SETTING_KEY"
                    onChange={(event) =>
                      updateRow(index, { key: event.target.value, custom: true })
                    }
                    aria-label={translate("inputSettings.settingKeyNamed", { index: index + 1 })}
                  />
                )}
                {custom && actionKey && definitions.length > 0 ? (
                  <input
                    className="mono inputConfigCustomKey"
                    value={row.key}
                    placeholder="CUSTOM_SETTING_KEY"
                    onChange={(event) =>
                      updateRow(index, { key: event.target.value, custom: true })
                    }
                    aria-label={translate("inputSettings.customSettingKeyNamed", {
                      index: index + 1,
                    })}
                  />
                ) : null}
              </div>
              <div className="inputConfigValueField">
                <label htmlFor={`input-setting-value-${index}`}>
                  {translate("inputSettings.appliedValue")}
                </label>
                <InputSettingValueEditor
                  id={`input-setting-value-${index}`}
                  row={row}
                  definition={definition}
                  onChange={(valueText) => updateRow(index, { valueText })}
                />
              </div>
              <div className="inputConfigRowActions">
                <button
                  className={
                    row.locked ? "button small primary iconButton" : "button small iconButton"
                  }
                  type="button"
                  title={
                    row.locked
                      ? translate("inputSettings.lockedHint")
                      : translate("inputSettings.unlockedHint")
                  }
                  aria-label={
                    row.locked
                      ? translate("inputSettings.unlockKey")
                      : translate("inputSettings.lockKey")
                  }
                  aria-pressed={row.locked}
                  onClick={() => updateRow(index, { locked: !row.locked })}
                >
                  {row.locked ? (
                    <Lock size={16} aria-hidden="true" />
                  ) : (
                    <Unlock size={16} aria-hidden="true" />
                  )}
                </button>
                <button
                  className="button small iconButton"
                  type="button"
                  title={translate("inputSettings.removeKey")}
                  aria-label={translate("inputSettings.removeKey")}
                  onClick={() =>
                    setRows((current) => current.filter((_, rowIndex) => rowIndex !== index))
                  }
                >
                  <Trash2 size={16} aria-hidden="true" />
                </button>
              </div>
              {definition ? (
                <InputSettingGuide
                  definition={definition}
                  onUseExample={() =>
                    updateRow(index, { valueText: formatInputSettingExample(definition.example) })
                  }
                />
              ) : (
                <p className="inputConfigCustomHelp">{translate("inputSettings.customKeyHint")}</p>
              )}
            </div>
          );
        })}
        <button
          className="button small inputConfigAdd"
          type="button"
          onClick={() =>
            setRows((current) => [...current, { key: "", valueText: "", locked: false }])
          }
        >
          <Plus size={16} aria-hidden="true" />
          {translate("inputSettings.addKey")}
        </button>
      </div>

      {error ? <div className="inlineNotice error">{error}</div> : null}
      <footer className="dialogFooter">
        <span>
          {existing ? (
            <button className="button danger" type="button" disabled={busy} onClick={remove}>
              <Trash2 size={16} aria-hidden="true" />
              {translate("inputSettings.deleteLayer")}
            </button>
          ) : null}
        </span>
        <div className="dialogFooterActions">
          <button className="button" type="button" disabled={busy} onClick={onClose}>
            {translate("common.cancel")}
          </button>
          <button className="button primary" type="button" disabled={busy} onClick={save}>
            <Save size={16} aria-hidden="true" />
            {busy ? translate("common.saving") : translate("inputSettings.save")}
          </button>
        </div>
      </footer>
    </Modal>
  );
}

function InputSettingValueEditor({
  id,
  row,
  definition,
  onChange,
}: {
  id: string;
  row: InputConfigRow;
  definition?: InputSettingDefinition;
  onChange: (valueText: string) => void;
}) {
  if (definition?.constValue !== undefined) {
    return <input id={id} className="mono" value={row.valueText} disabled />;
  }
  if (definition?.enumValues?.length) {
    return (
      <SelectControl
        id={id}
        className="mono"
        value={normalizedEnumValue(row.valueText, definition.enumValues)}
        onChange={onChange}
        ariaLabel={translate("inputSettings.allowedValue")}
        options={[
          { value: "", label: translate("inputSettings.selectAllowedValue") },
          ...definition.enumValues.map((value) => ({
            value: formatInputSettingExample(value),
            label: formatSchemaValue(value),
          })),
        ]}
      />
    );
  }
  if (definition?.type === "boolean") {
    return (
      <label className="inputConfigBoolean" htmlFor={id}>
        <input
          id={id}
          type="checkbox"
          checked={row.valueText === "true"}
          onChange={(event) => onChange(String(event.target.checked))}
        />
        <span>
          {row.valueText === "true" ? translate("common.enabled") : translate("common.disabled")}
        </span>
      </label>
    );
  }
  if (definition?.type === "number" || definition?.type === "integer") {
    return (
      <input
        id={id}
        type="number"
        value={row.valueText}
        onChange={(event) => onChange(event.target.value)}
      />
    );
  }
  return (
    <textarea
      id={id}
      className="mono"
      rows={definition?.type === "object" || definition?.type === "array" ? 5 : 2}
      value={row.valueText}
      placeholder={definition ? formatInputSettingExample(definition.example) : '{"key":"value"}'}
      onChange={(event) => onChange(event.target.value)}
    />
  );
}

function InputSettingGuide({
  definition,
  onUseExample,
}: {
  definition: InputSettingDefinition;
  onUseExample: () => void;
}) {
  return (
    <aside className="inputConfigGuide">
      <div className="inputConfigGuideHeading">
        <div>
          <span className={definition.source === "operator" ? "badge info" : "badge neutral"}>
            {definition.source === "operator"
              ? translate("inputSettings.operatorSetting")
              : translate("inputSettings.requestField")}
          </span>
          <span className="badge neutral mono">{definition.type}</span>
        </div>
        <button className="button small" type="button" onClick={onUseExample}>
          {translate("inputSettings.useExample")}
        </button>
      </div>
      <strong>{definition.title || definition.key}</strong>
      <p>{definition.description || translate("inputSettings.noDescriptionProvided")}</p>
      {definition.fields.length > 0 ? (
        <table className="inputConfigFieldGuide" aria-label={`${definition.key} fields`}>
          <tbody>
            {definition.fields.map((field) => (
              <tr className="inputConfigFieldGuideRow" key={field.name}>
                <td>
                  <code>{field.name}</code>
                </td>
                <td>
                  {field.title || field.description || translate("inputSettings.noDescription")}
                  <small>
                    {field.type}
                    {field.required
                      ? translate("inputSettings.requiredSuffix")
                      : translate("inputSettings.optionalSuffix")}
                    {field.enumValues?.length
                      ? ` · ${field.enumValues.map(formatSchemaValue).join(" | ")}`
                      : ""}
                  </small>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
      <div className="inputConfigExample">
        <span>{translate("inputSettings.example")}</span>
        <pre>{formatInputSettingExample(definition.example)}</pre>
      </div>
    </aside>
  );
}

function normalizedEnumValue(valueText: string, values: unknown[]): string {
  try {
    const parsed = JSON.parse(valueText);
    const match = values.find((value) => JSON.stringify(value) === JSON.stringify(parsed));
    return match === undefined ? "" : formatInputSettingExample(match);
  } catch {
    return "";
  }
}
