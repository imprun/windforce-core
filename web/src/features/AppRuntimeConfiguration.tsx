import {
  Boxes,
  KeyRound,
  LockKeyhole,
  Pencil,
  Plus,
  RefreshCw,
  ShieldAlert,
  Trash2,
} from "lucide-react";
import { type ReactNode, useState } from "react";
import {
  ConfirmDialog,
  EmptyState,
  ErrorNotice,
  Field,
  Loading,
  Modal,
  Panel,
  SelectControl,
} from "../components/ui";
import type {
  AppRuntimeLifecycle,
  AppRuntimeState,
  Resource,
  ResourceType,
  Variable,
} from "../lib/api";
import { errorMessage } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatTime } from "../lib/format";
import { translate } from "../shared/i18n";

type RuntimeTab = "variables" | "resources";
type Editor = { kind: "variable"; item?: Variable } | { kind: "resource"; item?: Resource } | null;
type DeleteTarget =
  | { kind: "variable"; item: Variable }
  | { kind: "resource"; item: Resource }
  | null;

export function AppRuntimeConfiguration({ appKey }: { appKey: string }) {
  const { api, notify } = useApp();
  const [tab, setTab] = useState<RuntimeTab>("variables");
  const [editor, setEditor] = useState<Editor>(null);
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget>(null);
  const [lifecycleTarget, setLifecycleTarget] = useState<AppRuntimeState | null>(null);
  const [purging, setPurging] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [actionError, setActionError] = useState("");
  const state = useAsync(async () => {
    const [variables, resources, resourceTypes, lifecycle, audit] = await Promise.all([
      api.variables(),
      api.resources(),
      api.resourceTypes(),
      api.appRuntimeLifecycle(appKey),
      api.appRuntimeLifecycleAudit(appKey),
    ]);
    return {
      variables: variables.filter((item) => item.app_key === appKey),
      resources: resources.filter((item) => item.app_key === appKey),
      resourceTypes,
      lifecycle,
      audit: audit.audits,
    };
  }, [api, appKey]);

  async function remove() {
    if (!deleteTarget) return;
    setDeleting(true);
    setActionError("");
    try {
      if (deleteTarget.kind === "variable") {
        await api.deleteVariable(deleteTarget.item.path, appKey);
      } else {
        await api.deleteResource(deleteTarget.item.path, appKey);
      }
      notify("ok", translate("runtimeConfig.deleted"));
      setDeleteTarget(null);
      state.reload();
    } catch (cause) {
      setActionError(errorMessage(cause));
    } finally {
      setDeleting(false);
    }
  }

  if (state.loading && !state.data) return <Loading label={translate("appRuntime.loading")} />;
  if (state.error) return <ErrorNotice message={state.error} onRetry={state.reload} />;
  if (!state.data) return null;

  const { variables, resources, resourceTypes, lifecycle, audit } = state.data;
  return (
    <div className="appRuntimeConfiguration">
      <Panel
        title={translate("appRuntime.lifecycleTitle")}
        subtitle={translate("appRuntime.lifecycleSubtitle")}
        actions={
          <button className="button" type="button" onClick={state.reload}>
            <RefreshCw aria-hidden="true" /> {translate("common.refresh")}
          </button>
        }
      >
        <div className="appRuntimeLifecycleSummary">
          <div>
            <span className={`badge ${lifecycleBadge(lifecycle.state)}`}>
              {lifecycleLabel(lifecycle.state)}
            </span>
            <p>{lifecycleDescription(lifecycle.state)}</p>
            {lifecycle.reason ? (
              <p className="cellSub">
                {translate("appRuntime.reason")}: {lifecycle.reason}
              </p>
            ) : null}
          </div>
          <div className="panelActions">
            {lifecycle.state !== "active" ? (
              <button
                className="button primary"
                type="button"
                data-ui-guide="app-runtime-reactivate"
                onClick={() => setLifecycleTarget("active")}
              >
                {translate("appRuntime.reactivate")}
              </button>
            ) : (
              <button
                className="button"
                type="button"
                data-ui-guide="app-runtime-retire"
                onClick={() => setLifecycleTarget("tombstoned")}
              >
                {translate("appRuntime.tombstone")}
              </button>
            )}
            {lifecycle.state !== "revoked" ? (
              <button
                className="button danger"
                type="button"
                data-ui-guide="app-runtime-revoke"
                onClick={() => setLifecycleTarget("revoked")}
              >
                <ShieldAlert aria-hidden="true" /> {translate("appRuntime.revoke")}
              </button>
            ) : null}
          </div>
        </div>
        <div className="appRuntimeRevision">
          <span>{translate("appRuntime.revision", { revision: lifecycle.revision })}</span>
          {lifecycle.updatedAt ? <span>{formatTime(lifecycle.updatedAt)}</span> : null}
        </div>
      </Panel>

      <Panel
        title={translate("appRuntime.configTitle")}
        subtitle={translate("appRuntime.configSubtitle")}
        actions={
          <button
            className="button primary"
            type="button"
            disabled={lifecycle.state !== "active"}
            onClick={() => setEditor({ kind: tab === "variables" ? "variable" : "resource" })}
          >
            <Plus aria-hidden="true" />
            {translate(
              tab === "variables" ? "runtimeConfig.newVariable" : "runtimeConfig.newResource",
            )}
          </button>
        }
      >
        <div className="runtimeConfigIntro inlineNotice">
          <LockKeyhole aria-hidden="true" />
          <span>{translate("appRuntime.securityNotice")}</span>
        </div>
        <div
          className="tabBar runtimeConfigTabs"
          role="tablist"
          aria-label={translate("appRuntime.configTitle")}
        >
          <RuntimeTabButton tab="variables" current={tab} onChange={setTab} icon={<KeyRound />} />
          <RuntimeTabButton tab="resources" current={tab} onChange={setTab} icon={<Boxes />} />
        </div>
        <div className="runtimeConfigSectionHeader">
          <p>
            {translate(
              tab === "variables" ? "appRuntime.variablesHint" : "appRuntime.resourcesHint",
            )}
          </p>
          <span className="badge badge-neutral">
            {translate("runtimeConfig.itemCount", {
              count: tab === "variables" ? variables.length : resources.length,
            })}
          </span>
        </div>
        {tab === "variables" ? (
          <VariableTable
            items={variables}
            onEdit={(item) => setEditor({ kind: "variable", item })}
            onDelete={(item) => setDeleteTarget({ kind: "variable", item })}
          />
        ) : (
          <ResourceTable
            items={resources}
            onEdit={(item) => setEditor({ kind: "resource", item })}
            onDelete={(item) => setDeleteTarget({ kind: "resource", item })}
          />
        )}
      </Panel>

      <Panel
        title={translate("appRuntime.auditTitle")}
        subtitle={translate("appRuntime.auditSubtitle")}
      >
        {audit.length ? (
          <div className="tableWrap">
            <table className="table runtimeConfigTable">
              <thead>
                <tr>
                  <th>{translate("appRuntime.state")}</th>
                  <th>{translate("appRuntime.reason")}</th>
                  <th>{translate("appRuntime.actor")}</th>
                  <th>{translate("appRuntime.changedAt")}</th>
                </tr>
              </thead>
              <tbody>
                {[...audit].reverse().map((item, index) => (
                  <tr key={`${item.revision}:${item.createdAt}:${index}`}>
                    <td>
                      <span className={`badge ${lifecycleBadge(item.state)}`}>
                        {item.purged ? translate("appRuntime.purged") : lifecycleLabel(item.state)}
                      </span>
                    </td>
                    <td className="cellSub">
                      {item.reason || "—"}
                      {item.forced ? ` · ${translate("appRuntime.forced")}` : ""}
                    </td>
                    <td className="mono">{item.actor || "—"}</td>
                    <td>{formatTime(item.createdAt)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <EmptyState title={translate("appRuntime.noAudit")} />
        )}
      </Panel>

      <Panel
        title={translate("appRuntime.purgeTitle")}
        subtitle={translate("appRuntime.purgeSubtitle")}
      >
        <div className="dangerZoneRow">
          <p>{translate("appRuntime.purgeHint")}</p>
          <button
            className="button danger"
            type="button"
            disabled={lifecycle.state === "active"}
            onClick={() => setPurging(true)}
          >
            <Trash2 aria-hidden="true" /> {translate("appRuntime.purge")}
          </button>
        </div>
      </Panel>

      {editor?.kind === "variable" ? (
        <AppVariableDialog
          appKey={appKey}
          item={editor.item}
          onClose={() => setEditor(null)}
          onSaved={() => {
            setEditor(null);
            state.reload();
          }}
        />
      ) : null}
      {editor?.kind === "resource" ? (
        <AppResourceDialog
          appKey={appKey}
          item={editor.item}
          resourceTypes={resourceTypes}
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
          description={
            actionError ||
            translate(
              deleteTarget.kind === "variable"
                ? "runtimeConfig.deleteVariableHint"
                : "runtimeConfig.deleteResourceHint",
              { name: deleteTarget.item.path },
            )
          }
          confirmLabel={deleting ? translate("common.deleting") : translate("common.delete")}
          onConfirm={() => void remove()}
          onCancel={() => {
            setDeleteTarget(null);
            setActionError("");
          }}
        />
      ) : null}
      {lifecycleTarget ? (
        <LifecycleDialog
          appKey={appKey}
          current={lifecycle}
          target={lifecycleTarget}
          onClose={() => setLifecycleTarget(null)}
          onSaved={() => {
            setLifecycleTarget(null);
            state.reload();
          }}
        />
      ) : null}
      {purging ? (
        <PurgeDialog
          appKey={appKey}
          onClose={() => setPurging(false)}
          onPurged={() => {
            setPurging(false);
            state.reload();
          }}
        />
      ) : null}
    </div>
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
      data-ui-guide={`app-runtime-tab-${tab}`}
      aria-selected={current === tab}
      className={current === tab ? "tab active" : "tab"}
      onClick={() => onChange(tab)}
    >
      {icon}{" "}
      {translate(
        tab === "variables" ? "runtimeConfig.tab.variables" : "runtimeConfig.tab.resources",
      )}
    </button>
  );
}

function VariableTable({
  items,
  onEdit,
  onDelete,
}: {
  items: Variable[];
  onEdit: (item: Variable) => void;
  onDelete: (item: Variable) => void;
}) {
  if (!items.length)
    return (
      <EmptyState title={translate("appRuntime.noVariables")}>
        <span>{translate("appRuntime.noVariablesHint")}</span>
      </EmptyState>
    );
  return (
    <div className="tableWrap">
      <table className="table runtimeConfigTable">
        <thead>
          <tr>
            <th>{translate("runtimeConfig.path")}</th>
            <th>{translate("runtimeConfig.value")}</th>
            <th>{translate("appRuntime.revisionLabel")}</th>
            <th>{translate("common.description")}</th>
            <th aria-label={translate("common.rowActions")} />
          </tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr key={item.path}>
              <td className="mono cellTitle">{item.path}</td>
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
              <td className="mono">{item.revision}</td>
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
  onEdit,
  onDelete,
}: {
  items: Resource[];
  onEdit: (item: Resource) => void;
  onDelete: (item: Resource) => void;
}) {
  if (!items.length)
    return (
      <EmptyState title={translate("appRuntime.noResources")}>
        <span>{translate("appRuntime.noResourcesHint")}</span>
      </EmptyState>
    );
  return (
    <div className="tableWrap">
      <table className="table runtimeConfigTable">
        <thead>
          <tr>
            <th>{translate("runtimeConfig.path")}</th>
            <th>{translate("runtimeConfig.type")}</th>
            <th>{translate("runtimeConfig.preview")}</th>
            <th>{translate("appRuntime.revisionLabel")}</th>
            <th aria-label={translate("common.rowActions")} />
          </tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr key={item.path}>
              <td className="mono cellTitle">{item.path}</td>
              <td className="mono">{item.resource_type}</td>
              <td>
                <code className="runtimeConfigValue">{compactJSON(item.value)}</code>
              </td>
              <td className="mono">{item.revision}</td>
              <RowActions onEdit={() => onEdit(item)} onDelete={() => onDelete(item)} />
            </tr>
          ))}
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

function AppVariableDialog({
  appKey,
  item,
  onClose,
  onSaved,
}: {
  appKey: string;
  item?: Variable;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { api, notify } = useApp();
  const [path, setPath] = useState(item?.path || "");
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
        app_key: appKey,
        path: path.trim(),
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
      subtitle={translate("appRuntime.fixedScope", { app: appKey })}
      onClose={onClose}
    >
      <div className="dialogForm">
        {error ? <ErrorNotice message={error} /> : null}
        <Field label={translate("runtimeConfig.path")} hint={translate("runtimeConfig.pathHint")}>
          <input
            value={path}
            disabled={Boolean(item)}
            onChange={(event) => setPath(event.target.value)}
          />
        </Field>
        <label className="toggleField">
          <input
            type="checkbox"
            checked={secret}
            disabled={Boolean(item)}
            onChange={(event) => setSecret(event.target.checked)}
          />
          <span>
            {translate("runtimeConfig.secretVariable")}
            <small>{translate("runtimeConfig.secretVariableHint")}</small>
          </span>
        </label>
        <Field
          label={secret ? translate("runtimeConfig.secretValue") : translate("runtimeConfig.value")}
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

function AppResourceDialog({
  appKey,
  item,
  resourceTypes,
  onClose,
  onSaved,
}: {
  appKey: string;
  item?: Resource;
  resourceTypes: ResourceType[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const { api, notify } = useApp();
  const [path, setPath] = useState(item?.path || "");
  const [resourceType, setResourceType] = useState(item?.resource_type || "");
  const [value, setValue] = useState(JSON.stringify(item?.value ?? {}, null, 2));
  const [description, setDescription] = useState(item?.description || "");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  async function save() {
    if (!path.trim()) return setError(translate("runtimeConfig.validation.path"));
    if (!resourceType) return setError(translate("appRuntime.validation.resourceType"));
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
        app_key: appKey,
        path: path.trim(),
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
      subtitle={translate("appRuntime.fixedScope", { app: appKey })}
      onClose={onClose}
      wide
    >
      <div className="dialogForm">
        {error ? <ErrorNotice message={error} /> : null}
        <div className="formGrid">
          <Field label={translate("runtimeConfig.path")} hint={translate("runtimeConfig.pathHint")}>
            <input
              value={path}
              disabled={Boolean(item)}
              onChange={(event) => setPath(event.target.value)}
            />
          </Field>
          <Field
            label={translate("runtimeConfig.type")}
            hint={translate("appRuntime.resourceTypeRequired")}
          >
            <SelectControl
              value={resourceType}
              onChange={setResourceType}
              options={resourceTypes.map((type) => ({
                value: `${type.name}@${type.version}`,
                label: `${type.name} @ ${type.version}`,
                description: type.description,
              }))}
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

function LifecycleDialog({
  appKey,
  current,
  target,
  onClose,
  onSaved,
}: {
  appKey: string;
  current: AppRuntimeLifecycle;
  target: AppRuntimeState;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { api, notify } = useApp();
  const [reason, setReason] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  async function save() {
    setSaving(true);
    setError("");
    try {
      await api.setAppRuntimeLifecycle(appKey, {
        state: target,
        reason: reason.trim(),
        expectedRevision: current.revision,
      });
      notify("ok", translate("appRuntime.lifecycleUpdated"));
      onSaved();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSaving(false);
    }
  }
  return (
    <Modal
      title={translate(`appRuntime.confirm.${target}`)}
      subtitle={lifecycleDescription(target)}
      onClose={onClose}
      compact
    >
      <div className="dialogForm">
        {error ? <ErrorNotice message={error} /> : null}
        <Field label={translate("appRuntime.reason")} hint={translate("appRuntime.reasonHint")}>
          <textarea value={reason} onChange={(event) => setReason(event.target.value)} />
        </Field>
      </div>
      <DialogActions
        saving={saving}
        onCancel={onClose}
        onSave={() => void save()}
        danger={target === "revoked"}
      />
    </Modal>
  );
}

function PurgeDialog({
  appKey,
  onClose,
  onPurged,
}: {
  appKey: string;
  onClose: () => void;
  onPurged: () => void;
}) {
  const { api, notify } = useApp();
  const [reason, setReason] = useState("");
  const [force, setForce] = useState(false);
  const [confirmation, setConfirmation] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  async function purge() {
    if (confirmation !== appKey) return;
    setSaving(true);
    setError("");
    try {
      await api.purgeAppRuntimeConfig(appKey, { reason: reason.trim(), force });
      notify("ok", translate("appRuntime.purgeCompleted"));
      onPurged();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setSaving(false);
    }
  }
  return (
    <Modal
      title={translate("appRuntime.purgeConfirmTitle")}
      subtitle={translate("appRuntime.purgeConfirmHint")}
      onClose={onClose}
      compact
    >
      <div className="dialogForm">
        {error ? <ErrorNotice message={error} /> : null}
        <Field label={translate("appRuntime.reason")}>
          <textarea value={reason} onChange={(event) => setReason(event.target.value)} />
        </Field>
        <label className="toggleField">
          <input
            type="checkbox"
            checked={force}
            onChange={(event) => setForce(event.target.checked)}
          />
          <span>
            {translate("appRuntime.forcePurge")}
            <small>{translate("appRuntime.forcePurgeHint")}</small>
          </span>
        </label>
        <Field label={translate("appRuntime.typeToConfirm", { app: appKey })}>
          <input
            className="mono"
            autoComplete="off"
            value={confirmation}
            onChange={(event) => setConfirmation(event.target.value)}
          />
        </Field>
      </div>
      <footer className="dialogFooter dialogFooterEnd">
        <button className="button" type="button" disabled={saving} onClick={onClose}>
          {translate("common.cancel")}
        </button>
        <button
          className="button danger"
          type="button"
          disabled={saving || confirmation !== appKey}
          onClick={() => void purge()}
        >
          {saving ? translate("common.deleting") : translate("appRuntime.purge")}
        </button>
      </footer>
    </Modal>
  );
}

function DialogActions({
  saving,
  onCancel,
  onSave,
  danger = false,
}: {
  saving: boolean;
  onCancel: () => void;
  onSave: () => void;
  danger?: boolean;
}) {
  return (
    <footer className="dialogFooter dialogFooterEnd">
      <button className="button" type="button" disabled={saving} onClick={onCancel}>
        {translate("common.cancel")}
      </button>
      <button
        className={`button ${danger ? "danger" : "primary"}`}
        type="button"
        data-ui-guide="app-runtime-lifecycle-save"
        disabled={saving}
        onClick={onSave}
      >
        {saving ? translate("common.saving") : translate("common.save")}
      </button>
    </footer>
  );
}

function lifecycleBadge(state: AppRuntimeState): string {
  if (state === "active") return "badge-success";
  if (state === "tombstoned") return "badge-warning";
  return "badge-critical";
}

function lifecycleLabel(state: AppRuntimeState): string {
  return translate(`appRuntime.state.${state}`);
}

function lifecycleDescription(state: AppRuntimeState): string {
  return translate(`appRuntime.stateDescription.${state}`);
}

function compactJSON(value: unknown): string {
  const encoded = JSON.stringify(value);
  if (!encoded) return translate("common.emptyValue");
  return encoded.length > 72 ? `${encoded.slice(0, 69)}…` : encoded;
}
