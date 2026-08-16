import {
  Boxes,
  Braces,
  KeyRound,
  LockKeyhole,
  Pencil,
  Plus,
  RefreshCw,
  Trash2,
} from "lucide-react";
import { type ReactNode, useMemo, useState } from "react";
import { Layout } from "../components/Layout";
import { SettingsNav } from "../components/SettingsNav";
import {
  ConfirmDialog,
  EmptyState,
  ErrorNotice,
  Field,
  Loading,
  Modal,
  SelectControl,
} from "../components/ui";
import type { Resource, ResourceType, Variable } from "../lib/api";
import { errorMessage } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { translate } from "../shared/i18n";

type RuntimeTab = "variables" | "resources" | "types";
type Editor =
  | { kind: "variable"; item?: Variable }
  | { kind: "resource"; item?: Resource }
  | { kind: "type"; item?: ResourceType }
  | null;
type DeleteTarget =
  | { kind: "variable"; item: Variable }
  | { kind: "resource"; item: Resource }
  | { kind: "type"; item: ResourceType }
  | null;

export function VariablesResourcesPage() {
  const { api, notify } = useApp();
  const [tab, setTab] = useState<RuntimeTab>("variables");
  const [query, setQuery] = useState("");
  const [editor, setEditor] = useState<Editor>(null);
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState("");
  const state = useAsync(async () => {
    const [variables, resources, resourceTypes] = await Promise.all([
      api.variables(),
      api.resources(),
      api.resourceTypes(),
    ]);
    return { variables, resources, resourceTypes };
  }, [api]);

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!state.data || !needle) return state.data;
    return {
      variables: state.data.variables.filter((item) =>
        [item.path, item.app_key, item.description].some((value) =>
          (value || "").toLowerCase().includes(needle),
        ),
      ),
      resources: state.data.resources.filter((item) =>
        [item.path, item.app_key, item.resource_type, item.description].some((value) =>
          (value || "").toLowerCase().includes(needle),
        ),
      ),
      resourceTypes: state.data.resourceTypes.filter((item) =>
        [item.name, item.version, item.description].some((value) =>
          value.toLowerCase().includes(needle),
        ),
      ),
    };
  }, [query, state.data]);

  function createCurrent() {
    if (tab === "variables") setEditor({ kind: "variable" });
    else if (tab === "resources") setEditor({ kind: "resource" });
    else setEditor({ kind: "type" });
  }

  async function remove() {
    if (!deleteTarget) return;
    setDeleting(true);
    setDeleteError("");
    try {
      if (deleteTarget.kind === "variable") {
        await api.deleteVariable(deleteTarget.item.path, deleteTarget.item.app_key);
      } else if (deleteTarget.kind === "resource") {
        await api.deleteResource(deleteTarget.item.path, deleteTarget.item.app_key);
      } else {
        await api.deleteResourceType(deleteTarget.item.name, deleteTarget.item.version);
      }
      notify("ok", translate("runtimeConfig.deleted"));
      setDeleteTarget(null);
      state.reload();
    } catch (cause) {
      setDeleteError(errorMessage(cause));
    } finally {
      setDeleting(false);
    }
  }

  const currentCount =
    tab === "variables"
      ? filtered?.variables.length
      : tab === "resources"
        ? filtered?.resources.length
        : filtered?.resourceTypes.length;

  return (
    <Layout
      title={translate("runtimeConfig.pageTitle")}
      subtitle={translate("runtimeConfig.subtitle")}
      actions={
        <>
          <input
            className="searchInput"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={translate("runtimeConfig.filter")}
            aria-label={translate("runtimeConfig.filter")}
          />
          <button className="button" type="button" onClick={() => state.reload()}>
            <RefreshCw aria-hidden="true" /> {translate("common.refresh")}
          </button>
          <button className="button primary" type="button" onClick={createCurrent}>
            <Plus aria-hidden="true" /> {createLabel(tab)}
          </button>
        </>
      }
    >
      <SettingsNav />
      <div className="runtimeConfigIntro inlineNotice">
        <LockKeyhole aria-hidden="true" />
        <span>{translate("runtimeConfig.securityNotice")}</span>
      </div>
      <div
        className="tabBar runtimeConfigTabs"
        role="tablist"
        aria-label={translate("runtimeConfig.sections")}
      >
        <RuntimeTabButton tab="variables" current={tab} onChange={setTab} icon={<KeyRound />} />
        <RuntimeTabButton tab="resources" current={tab} onChange={setTab} icon={<Boxes />} />
        <RuntimeTabButton tab="types" current={tab} onChange={setTab} icon={<Braces />} />
      </div>
      {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
      {state.loading && !state.data ? <Loading label={translate("runtimeConfig.loading")} /> : null}
      {filtered ? (
        <section aria-live="polite" aria-label={tabLabel(tab)}>
          <div className="runtimeConfigSectionHeader">
            <p>{sectionHint(tab)}</p>
            <span className="badge badge-neutral">
              {translate("runtimeConfig.itemCount", { count: currentCount || 0 })}
            </span>
          </div>
          {tab === "variables" ? (
            <VariableTable
              items={filtered.variables}
              query={query}
              onEdit={(item) => setEditor({ kind: "variable", item })}
              onDelete={(item) => setDeleteTarget({ kind: "variable", item })}
            />
          ) : tab === "resources" ? (
            <ResourceTable
              items={filtered.resources}
              query={query}
              onEdit={(item) => setEditor({ kind: "resource", item })}
              onDelete={(item) => setDeleteTarget({ kind: "resource", item })}
            />
          ) : (
            <ResourceTypeTable
              items={filtered.resourceTypes}
              resources={state.data?.resources || []}
              query={query}
              onEdit={(item) => setEditor({ kind: "type", item })}
              onDelete={(item) => setDeleteTarget({ kind: "type", item })}
            />
          )}
        </section>
      ) : null}
      {editor?.kind === "variable" ? (
        <VariableDialog
          item={editor.item}
          onClose={() => setEditor(null)}
          onSaved={() => {
            setEditor(null);
            state.reload();
          }}
        />
      ) : null}
      {editor?.kind === "resource" ? (
        <ResourceDialog
          item={editor.item}
          resourceTypes={state.data?.resourceTypes || []}
          onClose={() => setEditor(null)}
          onSaved={() => {
            setEditor(null);
            state.reload();
          }}
        />
      ) : null}
      {editor?.kind === "type" ? (
        <ResourceTypeDialog
          item={editor.item}
          onClose={() => setEditor(null)}
          onSaved={() => {
            setEditor(null);
            state.reload();
          }}
        />
      ) : null}
      {deleteTarget ? (
        <ConfirmDialog
          title={translate("runtimeConfig.deleteTitle")}
          description={deleteDescription(deleteTarget, deleteError)}
          confirmLabel={deleting ? translate("common.deleting") : translate("common.delete")}
          onConfirm={() => void remove()}
          onCancel={() => {
            setDeleteTarget(null);
            setDeleteError("");
          }}
        />
      ) : null}
    </Layout>
  );
}

function RuntimeTabButton({
  tab,
  current,
  onChange,
  icon,
}: {
  tab: RuntimeTab;
  current: RuntimeTab;
  onChange: (tab: RuntimeTab) => void;
  icon: ReactNode;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={current === tab}
      className={current === tab ? "tab active" : "tab"}
      onClick={() => onChange(tab)}
    >
      {icon} {tabLabel(tab)}
    </button>
  );
}

function VariableTable({
  items,
  query,
  onEdit,
  onDelete,
}: {
  items: Variable[];
  query: string;
  onEdit: (item: Variable) => void;
  onDelete: (item: Variable) => void;
}) {
  if (!items.length) return <RuntimeEmpty query={query} kind="variable" />;
  return (
    <div className="tableWrap">
      <table className="table runtimeConfigTable">
        <thead>
          <tr>
            <th>{translate("runtimeConfig.path")}</th>
            <th>{translate("runtimeConfig.scope")}</th>
            <th>{translate("runtimeConfig.value")}</th>
            <th>{translate("common.description")}</th>
            <th aria-label={translate("common.rowActions")} />
          </tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr key={`${item.app_key}:${item.path}`}>
              <td className="mono cellTitle">{item.path}</td>
              <td>{item.app_key || translate("runtimeConfig.workspaceScope")}</td>
              <td>
                {item.is_secret ? (
                  <span className="badge badge-warning">
                    <LockKeyhole aria-hidden="true" /> {translate("runtimeConfig.writeOnly")}
                  </span>
                ) : (
                  <code className="runtimeConfigValue">
                    {item.value || translate("common.emptyValue")}
                  </code>
                )}
              </td>
              <td className="cellSub">{item.description || "—"}</td>
              <RowActions onEdit={() => onEdit(item)} onDelete={() => onDelete(item)} />
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ResourceTable({
  items,
  query,
  onEdit,
  onDelete,
}: {
  items: Resource[];
  query: string;
  onEdit: (item: Resource) => void;
  onDelete: (item: Resource) => void;
}) {
  if (!items.length) return <RuntimeEmpty query={query} kind="resource" />;
  return (
    <div className="tableWrap">
      <table className="table runtimeConfigTable">
        <thead>
          <tr>
            <th>{translate("runtimeConfig.path")}</th>
            <th>{translate("runtimeConfig.scope")}</th>
            <th>{translate("runtimeConfig.type")}</th>
            <th>{translate("runtimeConfig.preview")}</th>
            <th>{translate("common.description")}</th>
            <th aria-label={translate("common.rowActions")} />
          </tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr key={`${item.app_key || "workspace"}:${item.path}`}>
              <td className="mono cellTitle">{item.path}</td>
              <td>{item.app_key || translate("runtimeConfig.workspaceScope")}</td>
              <td>{item.resource_type || translate("runtimeConfig.untyped")}</td>
              <td>
                <code className="runtimeConfigValue">{compactJSON(item.value)}</code>
              </td>
              <td className="cellSub">{item.description || "—"}</td>
              <RowActions onEdit={() => onEdit(item)} onDelete={() => onDelete(item)} />
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ResourceTypeTable({
  items,
  resources,
  query,
  onEdit,
  onDelete,
}: {
  items: ResourceType[];
  resources: Resource[];
  query: string;
  onEdit: (item: ResourceType) => void;
  onDelete: (item: ResourceType) => void;
}) {
  if (!items.length) return <RuntimeEmpty query={query} kind="type" />;
  return (
    <div className="tableWrap">
      <table className="table runtimeConfigTable">
        <thead>
          <tr>
            <th>{translate("common.name")}</th>
            <th>{translate("runtimeConfig.version")}</th>
            <th>{translate("runtimeConfig.usedBy")}</th>
            <th>{translate("common.description")}</th>
            <th aria-label={translate("common.rowActions")} />
          </tr>
        </thead>
        <tbody>
          {items.map((item) => {
            const reference = `${item.name}@${item.version}`;
            const used = resources.filter(
              (resource) =>
                resource.resource_type === reference ||
                (item.version === "1" && resource.resource_type === item.name),
            ).length;
            return (
              <tr key={reference}>
                <td className="mono cellTitle">{item.name}</td>
                <td className="mono">{item.version}</td>
                <td>{translate("runtimeConfig.resourceCount", { count: used })}</td>
                <td className="cellSub">{item.description || "—"}</td>
                <RowActions onEdit={() => onEdit(item)} onDelete={() => onDelete(item)} />
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function RowActions({ onEdit, onDelete }: { onEdit: () => void; onDelete: () => void }) {
  return (
    <td className="rowActions">
      <div className="rowActionsInner">
        <button className="button small" type="button" onClick={onEdit}>
          <Pencil aria-hidden="true" /> {translate("common.edit")}
        </button>
        <button className="button small danger" type="button" onClick={onDelete}>
          <Trash2 aria-hidden="true" /> {translate("common.delete")}
        </button>
      </div>
    </td>
  );
}

function RuntimeEmpty({ query, kind }: { query: string; kind: "variable" | "resource" | "type" }) {
  return (
    <EmptyState title={query ? translate("runtimeConfig.noMatches") : emptyLabel(kind)}>
      <span>{query ? translate("runtimeConfig.changeFilter") : emptyHint(kind)}</span>
    </EmptyState>
  );
}

function VariableDialog({
  item,
  onClose,
  onSaved,
}: {
  item?: Variable;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { api, notify } = useApp();
  const [path, setPath] = useState(item?.path || "");
  const [appKey, setAppKey] = useState(item?.app_key || "");
  const [value, setValue] = useState(item?.is_secret ? "" : item?.value || "");
  const [secret, setSecret] = useState(item?.is_secret || false);
  const [description, setDescription] = useState(item?.description || "");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  async function save() {
    if (!path.trim()) return setError(translate("runtimeConfig.validation.path"));
    if (secret && !value) return setError(translate("runtimeConfig.validation.secretValue"));
    setSaving(true);
    setError("");
    try {
      await api.setVariable({
        path: path.trim(),
        app_key: appKey.trim(),
        value,
        is_secret: secret,
        description: description.trim(),
      });
      notify(
        "ok",
        translate(item ? "runtimeConfig.variableReplaced" : "runtimeConfig.variableCreated"),
      );
      onSaved();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      title={translate(item ? "runtimeConfig.editVariable" : "runtimeConfig.newVariable")}
      subtitle={translate("runtimeConfig.variableDialogHint")}
      onClose={onClose}
    >
      <div className="dialogForm">
        {error ? <ErrorNotice message={error} /> : null}
        {item?.is_secret ? (
          <div className="inlineNotice warning">
            {translate("runtimeConfig.secretReplaceNotice")}
          </div>
        ) : null}
        <div className="formGrid">
          <Field label={translate("runtimeConfig.path")} hint={translate("runtimeConfig.pathHint")}>
            <input
              value={path}
              disabled={Boolean(item)}
              onChange={(event) => setPath(event.target.value)}
            />
          </Field>
          <Field
            label={translate("runtimeConfig.appScope")}
            hint={translate("runtimeConfig.appScopeHint")}
          >
            <input
              value={appKey}
              disabled={Boolean(item)}
              onChange={(event) => setAppKey(event.target.value)}
            />
          </Field>
        </div>
        <label className="toggleField">
          <input
            type="checkbox"
            checked={secret}
            disabled={item?.is_secret}
            onChange={(event) => setSecret(event.target.checked)}
          />
          <span>
            {translate("runtimeConfig.secretVariable")}
            <small>{translate("runtimeConfig.secretVariableHint")}</small>
          </span>
        </label>
        <Field
          label={secret ? translate("runtimeConfig.secretValue") : translate("runtimeConfig.value")}
          hint={secret ? translate("runtimeConfig.secretValueHint") : undefined}
        >
          <input
            className="mono"
            type={secret ? "password" : "text"}
            autoComplete="off"
            value={value}
            onChange={(event) => setValue(event.target.value)}
          />
        </Field>
        <Field label={translate("common.description")}>
          <input value={description} onChange={(event) => setDescription(event.target.value)} />
        </Field>
      </div>
      <DialogActions saving={saving} onCancel={onClose} onSave={() => void save()} />
    </Modal>
  );
}

function ResourceDialog({
  item,
  resourceTypes,
  onClose,
  onSaved,
}: {
  item?: Resource;
  resourceTypes: ResourceType[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const { api, notify } = useApp();
  const [path, setPath] = useState(item?.path || "");
  const [appKey, setAppKey] = useState(item?.app_key || "");
  const [resourceType, setResourceType] = useState(item?.resource_type || "");
  const [value, setValue] = useState(JSON.stringify(item?.value ?? {}, null, 2));
  const [description, setDescription] = useState(item?.description || "");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  async function save() {
    if (!path.trim()) return setError(translate("runtimeConfig.validation.path"));
    let parsed: unknown;
    try {
      parsed = JSON.parse(value);
    } catch {
      return setError(translate("runtimeConfig.validation.json"));
    }
    setSaving(true);
    setError("");
    try {
      await api.setResource({
        path: path.trim(),
        app_key: appKey.trim(),
        value: parsed,
        resource_type: resourceType,
        description: description.trim(),
      });
      notify(
        "ok",
        translate(item ? "runtimeConfig.resourceUpdated" : "runtimeConfig.resourceCreated"),
      );
      onSaved();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      title={translate(item ? "runtimeConfig.editResource" : "runtimeConfig.newResource")}
      subtitle={translate("runtimeConfig.resourceDialogHint")}
      onClose={onClose}
      wide
    >
      <div className="dialogForm">
        {error ? <ErrorNotice message={error} /> : null}
        <div className="inlineNotice">{translate("runtimeConfig.referenceHint")}</div>
        <div className="formGrid">
          <Field label={translate("runtimeConfig.path")} hint={translate("runtimeConfig.pathHint")}>
            <input
              value={path}
              disabled={Boolean(item)}
              onChange={(event) => setPath(event.target.value)}
            />
          </Field>
          <Field
            label={translate("runtimeConfig.appScope")}
            hint={translate("runtimeConfig.appScopeHint")}
          >
            <input
              value={appKey}
              disabled={Boolean(item)}
              onChange={(event) => setAppKey(event.target.value)}
            />
          </Field>
        </div>
        <div className="formGrid">
          <Field label={translate("runtimeConfig.type")}>
            <SelectControl
              value={resourceType}
              onChange={setResourceType}
              options={[
                { value: "", label: translate("runtimeConfig.untyped") },
                ...resourceTypes.map((type) => ({
                  value: `${type.name}@${type.version}`,
                  label: `${type.name} @ ${type.version}`,
                  description: type.description,
                })),
              ]}
            />
          </Field>
        </div>
        <Field
          label={translate("runtimeConfig.jsonValue")}
          hint={translate("runtimeConfig.schemaValidationHint")}
        >
          <textarea
            className="runtimeConfigEditor mono"
            value={value}
            spellCheck={false}
            onChange={(event) => setValue(event.target.value)}
          />
        </Field>
        <Field label={translate("common.description")}>
          <input value={description} onChange={(event) => setDescription(event.target.value)} />
        </Field>
      </div>
      <DialogActions saving={saving} onCancel={onClose} onSave={() => void save()} />
    </Modal>
  );
}

function ResourceTypeDialog({
  item,
  onClose,
  onSaved,
}: {
  item?: ResourceType;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { api, notify } = useApp();
  const [name, setName] = useState(item?.name || "");
  const [version, setVersion] = useState(item?.version || "1");
  const [schema, setSchema] = useState(JSON.stringify(item?.schema ?? { type: "object" }, null, 2));
  const [description, setDescription] = useState(item?.description || "");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  async function save() {
    if (!name.trim() || !version.trim())
      return setError(translate("runtimeConfig.validation.typeIdentity"));
    let parsed: unknown;
    try {
      parsed = JSON.parse(schema);
    } catch {
      return setError(translate("runtimeConfig.validation.json"));
    }
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return setError(translate("runtimeConfig.validation.schemaObject"));
    }
    setSaving(true);
    setError("");
    try {
      await api.setResourceType({
        name: name.trim(),
        version: version.trim(),
        schema: parsed as Record<string, unknown>,
        description: description.trim(),
      });
      notify("ok", translate(item ? "runtimeConfig.typeUpdated" : "runtimeConfig.typeCreated"));
      onSaved();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      title={translate(item ? "runtimeConfig.editType" : "runtimeConfig.newType")}
      subtitle={translate("runtimeConfig.typeDialogHint")}
      onClose={onClose}
      wide
    >
      <div className="dialogForm">
        {error ? <ErrorNotice message={error} /> : null}
        <div className="formGrid">
          <Field label={translate("common.name")}>
            <input
              value={name}
              disabled={Boolean(item)}
              onChange={(event) => setName(event.target.value)}
            />
          </Field>
          <Field label={translate("runtimeConfig.version")}>
            <input
              className="mono"
              value={version}
              disabled={Boolean(item)}
              onChange={(event) => setVersion(event.target.value)}
            />
          </Field>
        </div>
        <Field
          label={translate("runtimeConfig.jsonSchema")}
          hint={translate("runtimeConfig.jsonSchemaHint")}
        >
          <textarea
            className="runtimeConfigEditor mono"
            value={schema}
            spellCheck={false}
            onChange={(event) => setSchema(event.target.value)}
          />
        </Field>
        <Field label={translate("common.description")}>
          <input value={description} onChange={(event) => setDescription(event.target.value)} />
        </Field>
      </div>
      <DialogActions saving={saving} onCancel={onClose} onSave={() => void save()} />
    </Modal>
  );
}

function DialogActions({
  saving,
  onCancel,
  onSave,
}: {
  saving: boolean;
  onCancel: () => void;
  onSave: () => void;
}) {
  return (
    <footer className="dialogFooter dialogFooterEnd">
      <button className="button" type="button" disabled={saving} onClick={onCancel}>
        {translate("common.cancel")}
      </button>
      <button className="button primary" type="button" disabled={saving} onClick={onSave}>
        {saving ? translate("common.saving") : translate("common.save")}
      </button>
    </footer>
  );
}

function compactJSON(value: unknown): string {
  const encoded = JSON.stringify(value);
  if (!encoded) return translate("common.emptyValue");
  return encoded.length > 72 ? `${encoded.slice(0, 69)}…` : encoded;
}

function createLabel(tab: RuntimeTab): string {
  if (tab === "variables") return translate("runtimeConfig.newVariable");
  if (tab === "resources") return translate("runtimeConfig.newResource");
  return translate("runtimeConfig.newType");
}

function sectionHint(tab: RuntimeTab): string {
  if (tab === "variables") return translate("runtimeConfig.variablesHint");
  if (tab === "resources") return translate("runtimeConfig.resourcesHint");
  return translate("runtimeConfig.typesHint");
}

function tabLabel(tab: RuntimeTab): string {
  if (tab === "variables") return translate("runtimeConfig.tab.variables");
  if (tab === "resources") return translate("runtimeConfig.tab.resources");
  return translate("runtimeConfig.tab.types");
}

function emptyLabel(kind: "variable" | "resource" | "type"): string {
  if (kind === "variable") return translate("runtimeConfig.empty.variable");
  if (kind === "resource") return translate("runtimeConfig.empty.resource");
  return translate("runtimeConfig.empty.type");
}

function emptyHint(kind: "variable" | "resource" | "type"): string {
  if (kind === "variable") return translate("runtimeConfig.emptyHint.variable");
  if (kind === "resource") return translate("runtimeConfig.emptyHint.resource");
  return translate("runtimeConfig.emptyHint.type");
}

function deleteDescription(target: NonNullable<DeleteTarget>, error: string): string {
  if (error) return error;
  if (target.kind === "variable")
    return translate("runtimeConfig.deleteVariableHint", { name: target.item.path });
  if (target.kind === "resource")
    return translate("runtimeConfig.deleteResourceHint", { name: target.item.path });
  return translate("runtimeConfig.deleteTypeHint", {
    name: `${target.item.name}@${target.item.version}`,
  });
}
