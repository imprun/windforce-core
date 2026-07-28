import {
  Cable,
  Clock3,
  ExternalLink,
  Globe2,
  Pencil,
  Plus,
  Power,
  PowerOff,
  Trash2,
  Webhook,
} from "lucide-react";
import { type FormEvent, useMemo, useState } from "react";
import {
  DefinitionList,
  EmptyState,
  ErrorNotice,
  Field,
  JsonBlock,
  Loading,
  Modal,
  Panel,
  Sheet,
} from "../components/ui";
import { actionDisplayName } from "../lib/action-label";
import type {
  ActionView,
  HTTPRouteBinding,
  TriggerAudit,
  TriggerDefinition,
  TriggerDelivery,
  TriggerKind,
} from "../lib/api";
import { errorMessage } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatRelative, formatTime } from "../lib/format";
import { Link } from "../lib/router";
import {
  buildTriggerPayload,
  draftFromTrigger,
  emptyTriggerDraft,
  httpRouteProvider,
  type TriggerDraft,
  triggerConfigSummary,
  triggerKindLabel,
} from "../lib/triggers";

type TriggerRow = {
  trigger: TriggerDefinition;
  latestDelivery: TriggerDelivery | null;
};

export function AppTriggers({
  sourceID,
  appKey,
  actions,
}: {
  sourceID: number;
  appKey: string;
  actions: ActionView[];
}) {
  const { api, notify } = useApp();
  const [editor, setEditor] = useState<TriggerDefinition | "new" | null>(null);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<TriggerDefinition | null>(null);
  const [busyID, setBusyID] = useState("");
  const capabilityState = useAsync(() => api.systemInfo(), [api]);

  const state = useAsync(async () => {
    const definitions = (await api.triggers()).items.filter((item) => item.app === appKey);
    return Promise.all(
      definitions.map(async (trigger): Promise<TriggerRow> => {
        try {
          const deliveries = (await api.triggerDeliveries(trigger.id)).items;
          return { trigger, latestDelivery: deliveries[0] || null };
        } catch {
          return { trigger, latestDelivery: null };
        }
      }),
    );
  }, [api, appKey]);

  const rows = useMemo(
    () =>
      [...(state.data || [])].sort((left, right) =>
        left.trigger.name.localeCompare(right.trigger.name),
      ),
    [state.data],
  );
  const selected = rows.find((row) => row.trigger.id === selectedID)?.trigger || null;
  const routeProvider = httpRouteProvider(capabilityState.data);

  async function setEnabled(trigger: TriggerDefinition) {
    setBusyID(trigger.id);
    try {
      await api.setTriggerEnabled(trigger.id, !trigger.enabled);
      notify("ok", `${trigger.name} ${trigger.enabled ? "disabled" : "enabled"}.`);
      state.reload();
    } catch (cause) {
      notify("error", errorMessage(cause));
    } finally {
      setBusyID("");
    }
  }

  async function deleteTrigger(trigger: TriggerDefinition) {
    setBusyID(trigger.id);
    try {
      await api.deleteTrigger(trigger.id);
      notify("ok", `${trigger.name} deleted.`);
      if (selectedID === trigger.id) setSelectedID(null);
      setDeleteTarget(null);
      state.reload();
    } catch (cause) {
      notify("error", errorMessage(cause));
    } finally {
      setBusyID("");
    }
  }

  return (
    <>
      <Panel
        title="Triggers"
        subtitle="Inbound event sources that admit Runs for this App. Outbound release notifications remain in Settings → Webhooks."
        actions={
          <button
            className="button primary"
            type="button"
            onClick={() => setEditor("new")}
            disabled={actions.length === 0}
            id="addTriggerButton"
          >
            <Plus aria-hidden="true" />
            Add trigger
          </button>
        }
      >
        {actions.length === 0 ? (
          <EmptyState title="Publish an App release before adding a Trigger.">
            <p>A Trigger needs an Action target from the active release.</p>
          </EmptyState>
        ) : null}
        {actions.length > 0 && state.error ? (
          <ErrorNotice message={state.error} onRetry={state.reload} />
        ) : null}
        {actions.length > 0 && state.loading && !state.data ? (
          <Loading label="Loading Triggers…" />
        ) : null}
        {actions.length > 0 && state.data && rows.length === 0 ? (
          <EmptyState title="No inbound Triggers configured for this App.">
            <p>Add a Webhook, Schedule, or RabbitMQ source. New Triggers start disabled.</p>
            <button className="button primary" type="button" onClick={() => setEditor("new")}>
              <Plus aria-hidden="true" />
              Add trigger
            </button>
          </EmptyState>
        ) : null}
        {rows.length > 0 ? (
          <div className="tableWrap">
            <table className="table triggerTable" id="appTriggers">
              <thead>
                <tr>
                  <th>Trigger</th>
                  <th>Kind</th>
                  <th>Action</th>
                  <th>Status</th>
                  <th>Latest delivery</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {rows.map(({ trigger, latestDelivery }) => (
                  <tr key={trigger.id}>
                    <td>
                      <button
                        className="tableLink"
                        type="button"
                        onClick={() => setSelectedID(trigger.id)}
                      >
                        {trigger.name}
                      </button>
                      <span className="cellSub">{triggerConfigSummary(trigger)}</span>
                    </td>
                    <td>
                      <TriggerKindBadge kind={trigger.kind} />
                    </td>
                    <td>
                      <Link
                        className="mono"
                        to={`/apps/${sourceID}/docs/actions/${encodeURIComponent(trigger.action)}`}
                      >
                        {trigger.action}
                      </Link>
                    </td>
                    <td>
                      <TriggerEnabledBadge enabled={trigger.enabled} />
                    </td>
                    <td>
                      {latestDelivery ? (
                        <>
                          <TriggerDeliveryBadge state={latestDelivery.state} />
                          <span className="cellSub">
                            {formatRelative(latestDelivery.updated_at)}
                          </span>
                        </>
                      ) : (
                        <span className="cellSub">No deliveries yet</span>
                      )}
                    </td>
                    <td className="rowActions">
                      <button
                        className="button small"
                        type="button"
                        onClick={() => setEditor(trigger)}
                      >
                        <Pencil aria-hidden="true" />
                        Edit
                      </button>
                      <button
                        className="button small"
                        type="button"
                        onClick={() => void setEnabled(trigger)}
                        disabled={busyID === trigger.id}
                        aria-label={`${trigger.enabled ? "Disable" : "Enable"} ${trigger.name}`}
                      >
                        {trigger.enabled ? (
                          <PowerOff aria-hidden="true" />
                        ) : (
                          <Power aria-hidden="true" />
                        )}
                        {trigger.enabled ? "Disable" : "Enable"}
                      </button>
                      <button
                        className="button small danger"
                        type="button"
                        onClick={() => setDeleteTarget(trigger)}
                        aria-label={`Delete ${trigger.name}`}
                      >
                        <Trash2 aria-hidden="true" />
                        Delete
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </Panel>

      {editor ? (
        <TriggerEditorDialog
          key={editor === "new" ? "new" : editor.id}
          appKey={appKey}
          actions={actions}
          existing={editor === "new" ? null : editor}
          onClose={() => setEditor(null)}
          onSaved={(trigger) => {
            setEditor(null);
            setSelectedID(trigger.id);
            state.reload();
          }}
        />
      ) : null}
      {selected ? (
        <TriggerDetailSheet
          trigger={selected}
          routeProvider={routeProvider}
          onClose={() => setSelectedID(null)}
          onEdit={() => setEditor(selected)}
        />
      ) : null}
      {deleteTarget ? (
        <DeleteTriggerDialog
          trigger={deleteTarget}
          busy={busyID === deleteTarget.id}
          onClose={() => setDeleteTarget(null)}
          onConfirm={() => void deleteTrigger(deleteTarget)}
        />
      ) : null}
    </>
  );
}

function TriggerEditorDialog({
  appKey,
  actions,
  existing,
  onClose,
  onSaved,
}: {
  appKey: string;
  actions: ActionView[];
  existing: TriggerDefinition | null;
  onClose: () => void;
  onSaved: (trigger: TriggerDefinition) => void;
}) {
  const { api, notify } = useApp();
  const [draft, setDraft] = useState<TriggerDraft>(() => {
    const initial = existing ? draftFromTrigger(existing) : emptyTriggerDraft();
    if (!initial.action) initial.action = actions[0]?.action_key || "";
    return initial;
  });
  const [busy, setBusy] = useState(false);
  const [formError, setFormError] = useState("");

  function update<K extends keyof TriggerDraft>(key: K, value: TriggerDraft[K]) {
    setDraft((current) => ({ ...current, [key]: value }));
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const result = buildTriggerPayload(draft, appKey, existing);
    if (!result.payload) {
      setFormError(result.error || "Review the Trigger configuration.");
      return;
    }
    setBusy(true);
    setFormError("");
    try {
      const trigger = existing
        ? await api.updateTrigger(existing.id, result.payload)
        : await api.createTrigger(result.payload);
      notify("ok", `${trigger.name} ${existing ? "updated" : "created disabled"}.`);
      onSaved(trigger);
    } catch (cause) {
      setFormError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title={existing ? `Edit ${existing.name}` : "Add trigger"}
      subtitle={`${appKey} · Configure an inbound source. Secret values are write-only.`}
      onClose={onClose}
      wide
      id="triggerEditorDialog"
    >
      <form className="dialogForm" onSubmit={(event) => void submit(event)}>
        {formError ? <ErrorNotice message={formError} /> : null}
        {!existing ? (
          <div className="inlineNotice">
            New Triggers are created disabled. Review the target and delivery policy, then enable
            the Trigger from the list.
          </div>
        ) : null}
        <div className="formGrid">
          <Field label="Name">
            <input
              id="triggerName"
              value={draft.name}
              onChange={(event) => update("name", event.target.value)}
              placeholder="Partner orders"
              required
            />
          </Field>
          <Field label="Target Action" hint="The App target is fixed by this page.">
            <select
              value={draft.action}
              onChange={(event) => update("action", event.target.value)}
              required
            >
              {actions.map((action) => (
                <option key={action.action_key} value={action.action_key}>
                  {actionDisplayName(action.display_name) || action.action_key} ({action.action_key}
                  )
                </option>
              ))}
            </select>
          </Field>
        </div>

        <fieldset className="triggerKindFieldset" disabled={Boolean(existing)}>
          <legend>Trigger kind</legend>
          <div className="triggerKindPicker">
            <TriggerKindOption
              kind="webhook"
              selected={draft.kind === "webhook"}
              title="Webhook"
              description="Receive signed HTTP events."
              onSelect={(kind) => update("kind", kind)}
            />
            <TriggerKindOption
              kind="schedule"
              selected={draft.kind === "schedule"}
              title="Schedule"
              description="Run on a cron schedule."
              onSelect={(kind) => update("kind", kind)}
            />
            <TriggerKindOption
              kind="rabbitmq"
              selected={draft.kind === "rabbitmq"}
              title="RabbitMQ"
              description="Consume a durable queue."
              onSelect={(kind) => update("kind", kind)}
            />
          </div>
          {existing ? <p className="fieldHint">Kind cannot be changed after creation.</p> : null}
        </fieldset>

        {draft.kind === "webhook" ? (
          <WebhookFields draft={draft} existing={existing} update={update} />
        ) : null}
        {draft.kind === "schedule" ? <ScheduleFields draft={draft} update={update} /> : null}
        {draft.kind === "rabbitmq" ? (
          <RabbitMQFields draft={draft} existing={existing} update={update} />
        ) : null}

        <div className="dialogFooter">
          <span className="fieldHint">
            {existing?.has_secret ? "A write-only secret is currently configured." : ""}
          </span>
          <div className="dialogFooterActions">
            <button className="button" type="button" onClick={onClose} disabled={busy}>
              Cancel
            </button>
            <button className="button primary" type="submit" disabled={busy}>
              {busy ? "Saving…" : existing ? "Save changes" : "Create trigger"}
            </button>
          </div>
        </div>
      </form>
    </Modal>
  );
}

function TriggerKindOption({
  kind,
  selected,
  title,
  description,
  onSelect,
}: {
  kind: TriggerKind;
  selected: boolean;
  title: string;
  description: string;
  onSelect: (kind: TriggerKind) => void;
}) {
  const Icon = kind === "webhook" ? Webhook : kind === "schedule" ? Clock3 : Cable;
  return (
    <label className={selected ? "triggerKindOption selected" : "triggerKindOption"}>
      <input
        id={`triggerKind-${kind}`}
        type="radio"
        name="trigger-kind"
        value={kind}
        checked={selected}
        onChange={() => onSelect(kind)}
      />
      <Icon aria-hidden="true" />
      <span>
        <strong>{title}</strong>
        <small>{description}</small>
      </span>
    </label>
  );
}

function WebhookFields({
  draft,
  existing,
  update,
}: {
  draft: TriggerDraft;
  existing: TriggerDefinition | null;
  update: <K extends keyof TriggerDraft>(key: K, value: TriggerDraft[K]) => void;
}) {
  return (
    <section className="triggerFormSection">
      <div className="triggerFormSectionHeader">
        <h3>Webhook security and input</h3>
        <p>The exact request body is authenticated with HMAC-SHA256.</p>
      </div>
      <div className="formGrid">
        <Field
          label={existing ? "Replace signing secret" : "Signing secret"}
          hint={
            existing
              ? "Leave blank to retain the current secret."
              : "Use Generate or provide a random secret."
          }
        >
          <div className="fieldWithAction">
            <input
              type="password"
              value={draft.webhookSecret}
              onChange={(event) => update("webhookSecret", event.target.value)}
              autoComplete="new-password"
              required={!existing}
            />
            <button
              className="button"
              type="button"
              onClick={() => update("webhookSecret", randomSecret())}
            >
              Generate
            </button>
          </div>
        </Field>
        <Field label="Input mode">
          <select
            value={draft.inputMode}
            onChange={(event) => update("inputMode", event.target.value as "json" | "raw")}
          >
            <option value="json">JSON body</option>
            <option value="raw">Raw envelope</option>
          </select>
        </Field>
        <Field label="Signature header">
          <input
            value={draft.signatureHeader}
            onChange={(event) => update("signatureHeader", event.target.value)}
          />
        </Field>
        <Field label="Delivery ID header">
          <input
            value={draft.deliveryIDHeader}
            onChange={(event) => update("deliveryIDHeader", event.target.value)}
          />
        </Field>
        <Field label="Correlation header">
          <input
            value={draft.correlationHeader}
            onChange={(event) => update("correlationHeader", event.target.value)}
          />
        </Field>
      </div>
    </section>
  );
}

function ScheduleFields({
  draft,
  update,
}: {
  draft: TriggerDraft;
  update: <K extends keyof TriggerDraft>(key: K, value: TriggerDraft[K]) => void;
}) {
  return (
    <section className="triggerFormSection">
      <div className="triggerFormSectionHeader">
        <h3>Schedule</h3>
        <p>Five-field cron using an explicit IANA timezone. Missed occurrences are not replayed.</p>
      </div>
      <div className="formGrid">
        <Field label="Cron expression" hint="Minute hour day-of-month month day-of-week">
          <input
            className="mono"
            value={draft.cron}
            onChange={(event) => update("cron", event.target.value)}
            required
          />
        </Field>
        <Field label="Timezone" hint="For example Asia/Seoul or UTC">
          <input
            className="mono"
            value={draft.timezone}
            onChange={(event) => update("timezone", event.target.value)}
            required
          />
        </Field>
      </div>
      <Field label="Action input" hint="Valid JSON supplied to every scheduled Run.">
        <textarea
          className="mono triggerJSONInput"
          value={draft.scheduleInput}
          onChange={(event) => update("scheduleInput", event.target.value)}
          spellCheck={false}
        />
      </Field>
    </section>
  );
}

function RabbitMQFields({
  draft,
  existing,
  update,
}: {
  draft: TriggerDraft;
  existing: TriggerDefinition | null;
  update: <K extends keyof TriggerDraft>(key: K, value: TriggerDraft[K]) => void;
}) {
  return (
    <section className="triggerFormSection">
      <div className="triggerFormSectionHeader">
        <h3>RabbitMQ consumer</h3>
        <p>The queue must already exist. Durable admission completes before ACK.</p>
      </div>
      <div className="formGrid">
        <Field label="Queue">
          <input
            className="mono"
            value={draft.queue}
            onChange={(event) => update("queue", event.target.value)}
            placeholder="orders.windforce"
            required
          />
        </Field>
        <Field
          label={existing ? "Replace connection URL" : "Connection URL"}
          hint={
            existing
              ? "Leave blank to retain the current URL."
              : "Stored encrypted and never returned."
          }
        >
          <input
            type="password"
            value={draft.rabbitMQURL}
            onChange={(event) => update("rabbitMQURL", event.target.value)}
            placeholder="amqps://user:password@broker/vhost"
            autoComplete="new-password"
            required={!existing}
          />
        </Field>
        <Field label="Concurrency">
          <input
            type="number"
            min="1"
            max="128"
            value={draft.concurrency}
            onChange={(event) => update("concurrency", event.target.value)}
          />
        </Field>
        <Field label="Prefetch" hint="Must be at least concurrency.">
          <input
            type="number"
            min="1"
            max="65535"
            value={draft.prefetch}
            onChange={(event) => update("prefetch", event.target.value)}
          />
        </Field>
        <Field label="Input mode">
          <select
            value={draft.inputMode}
            onChange={(event) => update("inputMode", event.target.value as "json" | "raw")}
          >
            <option value="json">JSON body</option>
            <option value="raw">Raw envelope</option>
          </select>
        </Field>
        <Field label="Delivery ID header" hint="AMQP message_id is preferred when available.">
          <input
            value={draft.deliveryIDHeader}
            onChange={(event) => update("deliveryIDHeader", event.target.value)}
          />
        </Field>
        <Field label="Consumer tag" hint="Optional stable broker consumer name.">
          <input
            value={draft.consumerTag}
            onChange={(event) => update("consumerTag", event.target.value)}
          />
        </Field>
      </div>
    </section>
  );
}

function TriggerDetailSheet({
  trigger,
  routeProvider,
  onClose,
  onEdit,
}: {
  trigger: TriggerDefinition;
  routeProvider: string;
  onClose: () => void;
  onEdit: () => void;
}) {
  const { api, settings } = useApp();
  const state = useAsync(async () => {
    const [deliveries, audit, routes] = await Promise.all([
      api.triggerDeliveries(trigger.id),
      api.triggerAudit(trigger.id),
      trigger.kind === "webhook" && routeProvider
        ? api.httpRouteBindings(trigger.id)
        : Promise.resolve({ items: [] }),
    ]);
    return { deliveries: deliveries.items, audit: audit.items, routes: routes.items };
  }, [api, trigger.id, trigger.kind, routeProvider]);
  const endpoint =
    trigger.kind === "webhook"
      ? `/api/v1/workspaces/${encodeURIComponent(settings.workspace)}/triggers/${encodeURIComponent(trigger.id)}/events`
      : "";

  return (
    <Sheet
      title={trigger.name}
      subtitle={`${triggerKindLabel(trigger.kind)} · ${trigger.app}/${trigger.action}`}
      onClose={onClose}
      id="triggerDetailSheet"
      actions={
        <>
          <span className="fieldHint">Secrets and payload values are never displayed.</span>
          <button className="button" type="button" onClick={onEdit}>
            <Pencil aria-hidden="true" />
            Edit configuration
          </button>
        </>
      }
    >
      <section className="sheetSection">
        <DefinitionList
          className="sheetFacts"
          items={[
            ["Status", <TriggerEnabledBadge enabled={trigger.enabled} />],
            ["Kind", triggerKindLabel(trigger.kind)],
            ["Target", <span className="mono">{`${trigger.app}/${trigger.action}`}</span>],
            ["Secret", trigger.has_secret ? "Configured · write-only" : "Not configured"],
            ["Updated", `${formatTime(trigger.updated_at)} · ${trigger.updated_by || "system"}`],
            ["Trigger ID", <span className="mono">{trigger.id}</span>],
          ]}
        />
        {endpoint ? (
          <div className="triggerEndpoint">
            <p className="fieldLabel">Canonical ingress</p>
            <code>{endpoint}</code>
            <p className="fieldHint">
              Always available. Public routes rewrite to this endpoint without bypassing Trigger
              authentication or admission.
            </p>
          </div>
        ) : null}
        <h3>Safe configuration</h3>
        <JsonBlock value={trigger.config} maxHeight={240} />
      </section>

      {state.error ? (
        <section className="sheetSection">
          <ErrorNotice message={state.error} onRetry={state.reload} />
        </section>
      ) : null}
      {state.loading && !state.data ? <Loading /> : null}
      {state.data ? (
        <>
          {trigger.kind === "webhook" && routeProvider ? (
            <HTTPRouteBindingsSection
              trigger={trigger}
              bindings={state.data.routes}
              routeProvider={routeProvider}
              onChanged={state.reload}
            />
          ) : null}
          <TriggerDeliveries deliveries={state.data.deliveries} />
          <TriggerAuditTrail audit={state.data.audit} />
        </>
      ) : null}
    </Sheet>
  );
}

function HTTPRouteBindingsSection({
  trigger,
  bindings,
  routeProvider,
  onChanged,
}: {
  trigger: TriggerDefinition;
  bindings: HTTPRouteBinding[];
  routeProvider: string;
  onChanged: () => void;
}) {
  const { api, notify } = useApp();
  const [editor, setEditor] = useState<HTTPRouteBinding | "new" | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<HTTPRouteBinding | null>(null);
  const [busyID, setBusyID] = useState("");

  async function deleteBinding(binding: HTTPRouteBinding) {
    setBusyID(binding.id);
    try {
      await api.deleteHTTPRouteBinding(trigger.id, binding.id);
      notify("ok", "Public route deletion requested.");
      setDeleteTarget(null);
      onChanged();
    } catch (cause) {
      notify("error", errorMessage(cause));
    } finally {
      setBusyID("");
    }
  }

  return (
    <section className="sheetSection triggerRoutesSection">
      <div className="sheetSectionHeading">
        <div>
          <h3>Public routes</h3>
          <p>
            {routeProvider} reconciles friendly URLs to the canonical ingress. Route readiness is
            reported asynchronously.
          </p>
        </div>
        <button
          className="button small primary"
          type="button"
          onClick={() => setEditor("new")}
          aria-label="Add public route"
        >
          <Plus aria-hidden="true" />
          Add
        </button>
      </div>
      {bindings.length === 0 ? (
        <div className="triggerRoutesEmpty">
          <Globe2 aria-hidden="true" />
          <div>
            <strong>No public route configured.</strong>
            <p>The canonical ingress remains available for direct integrations.</p>
          </div>
        </div>
      ) : (
        <div className="triggerRouteList">
          {bindings.map((binding) => {
            const deleting = Boolean(binding.delete_requested_at);
            return (
              <article className="triggerRouteRow" key={binding.id}>
                <div className="triggerRouteSummary">
                  <div className="triggerRouteTitle">
                    <HTTPRouteBindingBadge state={binding.state} />
                    {binding.public_url ? (
                      <a href={binding.public_url} target="_blank" rel="noreferrer">
                        {binding.public_url}
                        <ExternalLink aria-hidden="true" />
                      </a>
                    ) : (
                      <strong className="mono">{routeBindingAddress(binding)}</strong>
                    )}
                  </div>
                  <p>
                    <span className="mono">{binding.provider}</span>
                    {" · "}generation {binding.generation}
                    {" · "}updated {formatRelative(binding.updated_at)}
                  </p>
                  {binding.error_summary ? (
                    <p className="triggerRouteError" role="alert">
                      {binding.error_summary}
                    </p>
                  ) : null}
                </div>
                <div className="rowActions">
                  <button
                    className="button small"
                    type="button"
                    onClick={() => setEditor(binding)}
                    disabled={deleting}
                  >
                    <Pencil aria-hidden="true" />
                    Edit
                  </button>
                  <button
                    className="button small danger"
                    type="button"
                    onClick={() => setDeleteTarget(binding)}
                    disabled={deleting || busyID === binding.id}
                    aria-label={`Delete public route ${routeBindingAddress(binding)}`}
                  >
                    <Trash2 aria-hidden="true" />
                    {deleting ? "Deleting…" : "Delete"}
                  </button>
                </div>
              </article>
            );
          })}
        </div>
      )}
      {editor ? (
        <HTTPRouteBindingEditor
          key={editor === "new" ? "new" : editor.id}
          trigger={trigger}
          routeProvider={routeProvider}
          existing={editor === "new" ? null : editor}
          onClose={() => setEditor(null)}
          onSaved={() => {
            setEditor(null);
            onChanged();
          }}
        />
      ) : null}
      {deleteTarget ? (
        <DeleteHTTPRouteBindingDialog
          binding={deleteTarget}
          busy={busyID === deleteTarget.id}
          onClose={() => setDeleteTarget(null)}
          onConfirm={() => void deleteBinding(deleteTarget)}
        />
      ) : null}
    </section>
  );
}

function HTTPRouteBindingEditor({
  trigger,
  routeProvider,
  existing,
  onClose,
  onSaved,
}: {
  trigger: TriggerDefinition;
  routeProvider: string;
  existing: HTTPRouteBinding | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { api, notify } = useApp();
  const [hostname, setHostname] = useState(existing?.hostname || "");
  const [path, setPath] = useState(existing?.path || suggestedRoutePath(trigger));
  const [formError, setFormError] = useState("");
  const [busy, setBusy] = useState(false);
  const provider =
    existing?.provider && existing.provider !== "auto" ? existing.provider : routeProvider;

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const normalizedPath = path.trim();
    if (!normalizedPath.startsWith("/") || /[\\?#]/.test(normalizedPath)) {
      setFormError("Path must start with / and cannot contain a query, fragment, or backslash.");
      return;
    }
    setBusy(true);
    setFormError("");
    try {
      const payload = {
        hostname: hostname.trim() || undefined,
        path: normalizedPath,
        visibility: "public" as const,
        provider,
      };
      if (existing) {
        await api.updateHTTPRouteBinding(trigger.id, existing.id, payload);
      } else {
        await api.createHTTPRouteBinding(trigger.id, payload);
      }
      notify("ok", `Public route ${existing ? "updated" : "requested"}.`);
      onSaved();
    } catch (cause) {
      setFormError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title={existing ? "Edit public route" : "Add public route"}
      subtitle={`${trigger.name} · The Router Provider reports readiness after reconciliation.`}
      onClose={onClose}
      id="httpRouteBindingEditor"
    >
      <form className="dialogForm" onSubmit={(event) => void submit(event)}>
        {formError ? <ErrorNotice message={formError} /> : null}
        <Field
          label="Hostname"
          hint="Optional. Leave blank to let the Router Provider assign its default hostname."
        >
          <input
            className="mono"
            value={hostname}
            onChange={(event) => setHostname(event.target.value)}
            placeholder="hooks.example.com"
            autoComplete="off"
          />
        </Field>
        <Field label="Path" hint="A public path that rewrites to the canonical Trigger ingress.">
          <input
            className="mono"
            value={path}
            onChange={(event) => setPath(event.target.value)}
            placeholder="/hooks/my-app"
            required
          />
        </Field>
        <Field label="Router Provider">
          <div className="routeProviderValue">
            <Globe2 aria-hidden="true" />
            <span className="mono">{provider}</span>
          </div>
        </Field>
        <div className="inlineNotice">
          The friendly route keeps the same webhook signature, body-size, idempotency, and Run
          admission checks as the canonical ingress.
        </div>
        <div className="dialogFooter">
          <span className="fieldHint">New and changed routes begin in Pending state.</span>
          <div className="dialogFooterActions">
            <button className="button" type="button" onClick={onClose} disabled={busy}>
              Cancel
            </button>
            <button className="button primary" type="submit" disabled={busy}>
              {busy ? "Saving…" : existing ? "Save route" : "Request route"}
            </button>
          </div>
        </div>
      </form>
    </Modal>
  );
}

function DeleteHTTPRouteBindingDialog({
  binding,
  busy,
  onClose,
  onConfirm,
}: {
  binding: HTTPRouteBinding;
  busy: boolean;
  onClose: () => void;
  onConfirm: () => void;
}) {
  return (
    <Modal
      title="Delete public route?"
      subtitle={routeBindingAddress(binding)}
      onClose={onClose}
      id="deleteHTTPRouteBindingDialog"
    >
      <div className="inlineNotice error" role="alert">
        The route enters Deleting until the Router Provider confirms cleanup. The Trigger and its
        canonical ingress remain available.
      </div>
      <div className="dialogFooter">
        <span />
        <div className="dialogFooterActions">
          <button className="button" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button className="button danger" type="button" onClick={onConfirm} disabled={busy}>
            <Trash2 aria-hidden="true" />
            {busy ? "Requesting…" : "Delete public route"}
          </button>
        </div>
      </div>
    </Modal>
  );
}

function HTTPRouteBindingBadge({ state }: { state: HTTPRouteBinding["state"] }) {
  const className =
    state === "ready"
      ? "badge badge-good"
      : state === "error"
        ? "badge badge-critical"
        : state === "deleting"
          ? "badge badge-warning"
          : "badge badge-neutral";
  return <span className={className}>{routeBindingStateLabel(state)}</span>;
}

function routeBindingStateLabel(state: HTTPRouteBinding["state"]): string {
  if (state === "ready") return "Ready";
  if (state === "error") return "Error";
  if (state === "deleting") return "Deleting";
  if (state === "deleted") return "Deleted";
  return "Pending";
}

function routeBindingAddress(binding: Pick<HTTPRouteBinding, "hostname" | "path">): string {
  return `${binding.hostname || "Provider hostname"}${binding.path}`;
}

function suggestedRoutePath(trigger: TriggerDefinition): string {
  const app = trigger.app.toLowerCase().replace(/[^a-z0-9._-]+/g, "-");
  const name = trigger.name.toLowerCase().replace(/[^a-z0-9._-]+/g, "-");
  return `/hooks/${app}/${name}`.replace(/-+/g, "-");
}

function TriggerDeliveries({ deliveries }: { deliveries: TriggerDelivery[] }) {
  return (
    <section className="sheetSection">
      <h3>Delivery history</h3>
      {deliveries.length === 0 ? (
        <p className="cellSub">No deliveries recorded yet.</p>
      ) : (
        <div className="tableWrap">
          <table className="table triggerDeliveryTable">
            <thead>
              <tr>
                <th>State</th>
                <th>Delivery</th>
                <th>Run</th>
                <th>When</th>
              </tr>
            </thead>
            <tbody>
              {deliveries.map((delivery) => (
                <tr key={delivery.id}>
                  <td>
                    <TriggerDeliveryBadge state={delivery.state} />
                    {delivery.error_summary ? (
                      <span className="cellSub">{delivery.error_summary}</span>
                    ) : null}
                  </td>
                  <td className="mono">{delivery.delivery_id}</td>
                  <td className="mono">{delivery.run_id || "—"}</td>
                  <td>
                    {formatRelative(delivery.updated_at)}
                    <span className="cellSub">{formatTime(delivery.updated_at)}</span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

function TriggerAuditTrail({ audit }: { audit: TriggerAudit[] }) {
  return (
    <section className="sheetSection">
      <h3>Audit</h3>
      {audit.length === 0 ? (
        <p className="cellSub">No audit events recorded.</p>
      ) : (
        <ol className="triggerAuditList">
          {audit.map((event) => (
            <li key={event.id}>
              <span className="triggerAuditMarker" aria-hidden="true" />
              <div>
                <strong>{auditLabel(event.kind)}</strong>
                <p>{event.detail || "No additional detail."}</p>
                <small>
                  {event.actor || "system"} · {formatTime(event.created_at)}
                </small>
              </div>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

function DeleteTriggerDialog({
  trigger,
  busy,
  onClose,
  onConfirm,
}: {
  trigger: TriggerDefinition;
  busy: boolean;
  onClose: () => void;
  onConfirm: () => void;
}) {
  return (
    <Modal
      title={`Delete ${trigger.name}?`}
      subtitle="This removes the Trigger definition and stops future admissions. Existing Runs remain."
      onClose={onClose}
      id="deleteTriggerDialog"
    >
      <div className="inlineNotice error" role="alert">
        This action cannot be undone. Disable the Trigger instead if you may need it later.
      </div>
      <div className="dialogFooter">
        <span />
        <div className="dialogFooterActions">
          <button className="button" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button className="button danger" type="button" onClick={onConfirm} disabled={busy}>
            <Trash2 aria-hidden="true" />
            {busy ? "Deleting…" : "Delete trigger"}
          </button>
        </div>
      </div>
    </Modal>
  );
}

function TriggerKindBadge({ kind }: { kind: TriggerKind }) {
  const Icon = kind === "webhook" ? Webhook : kind === "schedule" ? Clock3 : Cable;
  return (
    <span className="badge badge-neutral triggerKindBadge">
      <Icon aria-hidden="true" />
      {triggerKindLabel(kind)}
    </span>
  );
}

function TriggerEnabledBadge({ enabled }: { enabled: boolean }) {
  return (
    <span className={`badge ${enabled ? "badge-good" : "badge-neutral"}`}>
      <span className="badgeIcon" aria-hidden="true">
        {enabled ? "●" : "○"}
      </span>
      {enabled ? "Enabled" : "Disabled"}
    </span>
  );
}

function TriggerDeliveryBadge({ state }: { state: TriggerDelivery["state"] }) {
  const className =
    state === "admitted"
      ? "badge badge-good"
      : state === "retryable"
        ? "badge badge-warning"
        : "badge badge-critical";
  return <span className={className}>{deliveryLabel(state)}</span>;
}

function deliveryLabel(state: TriggerDelivery["state"]): string {
  if (state === "admitted") return "Admitted";
  if (state === "retryable") return "Retrying";
  return "Rejected";
}

function auditLabel(kind: string): string {
  return kind
    .replace(/^trigger_/, "")
    .split("_")
    .map((part) => `${part.slice(0, 1).toUpperCase()}${part.slice(1)}`)
    .join(" ");
}

function randomSecret(): string {
  const bytes = new Uint8Array(32);
  globalThis.crypto.getRandomValues(bytes);
  return Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join("");
}
