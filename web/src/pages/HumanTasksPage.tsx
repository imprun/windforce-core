import { type FormEvent, useEffect, useMemo, useState } from "react";
import { Layout } from "../components/Layout";
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
import { errorMessage, type HumanTask, type HumanTaskState, type JSONSchema } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatRelative, formatTime } from "../lib/format";
import { translate } from "../shared/i18n";

type TaskFilter = "pending" | "all";

export function HumanTasksPage() {
  const { api, notify } = useApp();
  const [filter, setFilter] = useState<TaskFilter>("pending");
  const [selected, setSelected] = useState<HumanTask | null>(null);
  const [canceling, setCanceling] = useState<HumanTask | null>(null);
  const state = useAsync(
    () => api.humanTasks(filter === "pending" ? "pending" : ""),
    [api, filter],
  );

  useEffect(() => {
    const interval = window.setInterval(state.reload, 5000);
    return () => window.clearInterval(interval);
  }, [state.reload]);

  async function decide(
    task: HumanTask,
    outcome: "submit" | "cancel",
    value?: Record<string, unknown>,
  ) {
    await api.decideHumanTask(task.id, { outcome, value });
    setSelected(null);
    setCanceling(null);
    notify("ok", translate(outcome === "submit" ? "humanTasks.decided" : "humanTasks.canceled"));
    state.reload();
  }

  async function cancel(task: HumanTask) {
    try {
      await decide(task, "cancel");
    } catch (cause) {
      notify("error", errorMessage(cause));
    }
  }

  return (
    <Layout
      title={translate("navigation.humanTasks")}
      subtitle={translate("humanTasks.subtitle")}
      actions={
        <button className="button" type="button" onClick={state.reload}>
          {translate("common.refresh")}
        </button>
      }
    >
      <Panel
        title={translate("humanTasks.queue")}
        subtitle={translate("humanTasks.queueHint")}
        actions={
          <SelectControl
            value={filter}
            onChange={setFilter}
            ariaLabel={translate("common.status")}
            options={[
              { value: "pending", label: translate("humanTasks.pending") },
              { value: "all", label: translate("humanTasks.all") },
            ]}
          />
        }
      >
        {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
        {state.loading && !state.data ? <Loading /> : null}
        {state.data?.items.length === 0 ? (
          <EmptyState title={translate("humanTasks.empty")} />
        ) : null}
        {state.data?.items.length ? (
          <div className="tableWrap">
            <table className="table humanTaskTable">
              <thead>
                <tr>
                  <th>{translate("humanTasks.request")}</th>
                  <th>{translate("humanTasks.target")}</th>
                  <th>{translate("common.status")}</th>
                  <th>{translate("humanTasks.expires")}</th>
                  <th aria-label={translate("common.actions")} />
                </tr>
              </thead>
              <tbody>
                {state.data.items.map((task) => (
                  <tr key={task.id}>
                    <td>
                      <strong className="cellTitle">{task.title}</strong>
                      <small className="cellSub mono">{task.id}</small>
                    </td>
                    <td>
                      <span>{task.app || "—"}</span>
                      <small className="cellSub mono">{task.action || task.key}</small>
                    </td>
                    <td>
                      <TaskStateBadge state={task.state} />
                      {task.terminal_cause ? (
                        <small className="cellSub">
                          {translate("humanTasks.terminalCause", { cause: task.terminal_cause })}
                        </small>
                      ) : null}
                    </td>
                    <td title={formatTime(task.expires_at)}>{formatRelative(task.expires_at)}</td>
                    <td className="rowActions">
                      <button
                        className={
                          task.state === "pending" ? "button primary small" : "button small"
                        }
                        type="button"
                        onClick={() => setSelected(task)}
                      >
                        {task.state === "pending"
                          ? translate("humanTasks.resolve")
                          : translate("common.viewAudit")}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </Panel>

      {selected ? (
        <HumanTaskDialog
          task={selected}
          onClose={() => setSelected(null)}
          onSubmit={(value) => decide(selected, "submit", value)}
          onCancel={() => setCanceling(selected)}
        />
      ) : null}
      {canceling ? (
        <ConfirmDialog
          title={translate("humanTasks.cancelTitle")}
          description={translate("humanTasks.cancelHint")}
          confirmLabel={translate("humanTasks.cancelTask")}
          onCancel={() => setCanceling(null)}
          onConfirm={() => void cancel(canceling)}
        />
      ) : null}
    </Layout>
  );
}

function TaskStateBadge({ state }: { state: HumanTaskState }) {
  const tone =
    state === "pending" ? "badge-warning" : state === "decided" ? "badge-good" : "badge-neutral";
  return <span className={`badge ${tone}`}>{translate(`humanTasks.state.${state}`)}</span>;
}

function HumanTaskDialog({
  task,
  onClose,
  onSubmit,
  onCancel,
}: {
  task: HumanTask;
  onClose: () => void;
  onSubmit: (value: Record<string, unknown>) => Promise<void>;
  onCancel: () => void;
}) {
  const [value, setValue] = useState<Record<string, unknown>>(() =>
    schemaDefaults(task.input_schema),
  );
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const pending = task.state === "pending";
  const missingRequired = useMemo(
    () => (task.input_schema?.required || []).some((key) => isEmpty(value[key])),
    [task.input_schema?.required, value],
  );

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setError("");
    try {
      await onSubmit(value);
    } catch (cause) {
      setError(errorMessage(cause));
      setSubmitting(false);
    }
  }

  return (
    <Modal
      id="humanTaskDialog"
      title={task.title || translate("humanTasks.reviewTitle")}
      subtitle={task.description || translate("humanTasks.reviewHint")}
      onClose={onClose}
      wide
    >
      <div className="humanTaskMeta">
        <span className="mono">
          {task.app || "—"} / {task.action || task.key}
        </span>
        <TaskStateBadge state={task.state} />
        {task.has_private_context ? (
          <span className="badge badge-neutral">{translate("humanTasks.privateContext")}</span>
        ) : null}
      </div>
      {error ? <ErrorNotice message={error} /> : null}
      <form className="humanTaskForm" onSubmit={submit}>
        <SchemaFields
          schema={task.input_schema || { type: "object" }}
          value={value}
          disabled={!pending || submitting}
          onChange={setValue}
        />
        <footer className="dialogFooter dialogFooterBetween">
          <div>
            {pending ? (
              <button
                className="button danger"
                type="button"
                disabled={submitting}
                onClick={onCancel}
              >
                {translate("humanTasks.cancelTask")}
              </button>
            ) : null}
          </div>
          <div className="dialogFooterActions">
            <button className="button" type="button" onClick={onClose}>
              {translate("common.close")}
            </button>
            {pending ? (
              <button
                className="button primary"
                type="submit"
                disabled={submitting || missingRequired}
              >
                {submitting ? translate("humanTasks.submitting") : translate("humanTasks.submit")}
              </button>
            ) : null}
          </div>
        </footer>
      </form>
    </Modal>
  );
}

function SchemaFields({
  schema,
  value,
  disabled,
  onChange,
}: {
  schema: JSONSchema;
  value: Record<string, unknown>;
  disabled: boolean;
  onChange: (value: Record<string, unknown>) => void;
}) {
  const required = new Set(schema.required || []);
  const entries = Object.entries(schema.properties || {});
  if (!entries.length) return <EmptyState title={translate("humanTasks.empty")} />;
  return (
    <div className="formGrid humanTaskFields">
      {entries.map(([name, field]) => (
        <SchemaField
          key={name}
          name={name}
          schema={field}
          required={required.has(name)}
          value={value[name]}
          disabled={disabled}
          onChange={(next) => onChange({ ...value, [name]: next })}
        />
      ))}
    </div>
  );
}

function SchemaField({
  name,
  schema,
  required,
  value,
  disabled,
  onChange,
}: {
  name: string;
  schema: JSONSchema;
  required: boolean;
  value: unknown;
  disabled: boolean;
  onChange: (value: unknown) => void;
}) {
  const label = `${schema.title || name}${required ? ` · ${translate("humanTasks.required")}` : ""}`;
  if (schema.enum?.every((item) => typeof item === "string")) {
    return (
      <Field label={label} hint={schema.description}>
        <SelectControl
          value={typeof value === "string" ? value : ""}
          onChange={onChange}
          disabled={disabled}
          ariaLabel={label}
          options={schema.enum.map((item) => ({ value: String(item), label: String(item) }))}
        />
      </Field>
    );
  }
  if (schema.type === "boolean") {
    return (
      <Field label={label} hint={schema.description}>
        <SelectControl
          value={value === true ? "true" : value === false ? "false" : ""}
          onChange={(next) => onChange(next === "" ? undefined : next === "true")}
          disabled={disabled}
          ariaLabel={label}
          options={[
            { value: "", label: "—" },
            { value: "true", label: translate("humanTasks.booleanTrue") },
            { value: "false", label: translate("humanTasks.booleanFalse") },
          ]}
        />
      </Field>
    );
  }
  if (schema.type === "string" || !schema.type) {
    return (
      <Field label={label} hint={schema.description}>
        <input
          disabled={disabled}
          required={required}
          value={typeof value === "string" ? value : ""}
          onChange={(event) => onChange(event.target.value)}
        />
      </Field>
    );
  }
  if (schema.type === "number" || schema.type === "integer") {
    return (
      <Field label={label} hint={schema.description}>
        <input
          type="number"
          step={schema.type === "integer" ? 1 : "any"}
          disabled={disabled}
          required={required}
          value={typeof value === "number" ? value : ""}
          onChange={(event) =>
            onChange(event.target.value === "" ? undefined : Number(event.target.value))
          }
        />
      </Field>
    );
  }
  return (
    <div className="inlineNotice">
      {label}: {translate("humanTasks.unsupportedField")}
    </div>
  );
}

function schemaDefaults(schema?: JSONSchema): Record<string, unknown> {
  return Object.fromEntries(
    Object.entries(schema?.properties || {}).flatMap(([key, field]) =>
      field.default === undefined ? [] : [[key, field.default]],
    ),
  );
}

function isEmpty(value: unknown): boolean {
  return value === undefined || value === null || value === "";
}
