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
  SelectControl,
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
import { translate } from "../shared/i18n";

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
      notify(
        "ok",
        translate(trigger.enabled ? "trigger.notify.disabled" : "trigger.notify.enabled", {
          name: trigger.name,
        }),
      );
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
      notify("ok", translate("trigger.notify.deleted", { name: trigger.name }));
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
        title={translate("trigger.title")}
        subtitle={translate("trigger.subtitle")}
        actions={
          <button
            className="button primary"
            type="button"
            onClick={() => setEditor("new")}
            disabled={actions.length === 0}
            id="addTriggerButton"
          >
            <Plus aria-hidden="true" />
            {translate("trigger.add")}
          </button>
        }
      >
        {actions.length === 0 ? (
          <EmptyState title={translate("trigger.publishFirst")}>
            <p>{translate("trigger.needsAction")}</p>
          </EmptyState>
        ) : null}
        {actions.length > 0 && state.error ? (
          <ErrorNotice message={state.error} onRetry={state.reload} />
        ) : null}
        {actions.length > 0 && state.loading && !state.data ? (
          <Loading label={translate("trigger.loading")} />
        ) : null}
        {actions.length > 0 && state.data && rows.length === 0 ? (
          <EmptyState title={translate("trigger.empty")}>
            <p>{translate("trigger.emptyHint")}</p>
            <button className="button primary" type="button" onClick={() => setEditor("new")}>
              <Plus aria-hidden="true" />
              {translate("trigger.add")}
            </button>
          </EmptyState>
        ) : null}
        {rows.length > 0 ? (
          <div className="tableWrap">
            <table className="table triggerTable" id="appTriggers">
              <thead>
                <tr>
                  <th>{translate("trigger.column.trigger")}</th>
                  <th>{translate("trigger.column.kind")}</th>
                  <th>{translate("trigger.column.action")}</th>
                  <th>{translate("common.status")}</th>
                  <th>{translate("trigger.column.latestDelivery")}</th>
                  <th>{translate("common.actions")}</th>
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
                        <span className="cellSub">{translate("trigger.noDeliveries")}</span>
                      )}
                    </td>
                    <td className="rowActions">
                      <button
                        className="button small"
                        type="button"
                        onClick={() => setEditor(trigger)}
                      >
                        <Pencil aria-hidden="true" />
                        {translate("common.edit")}
                      </button>
                      <button
                        className="button small"
                        type="button"
                        onClick={() => void setEnabled(trigger)}
                        disabled={busyID === trigger.id}
                        aria-label={translate(
                          trigger.enabled ? "trigger.disableNamed" : "trigger.enableNamed",
                          { name: trigger.name },
                        )}
                      >
                        {trigger.enabled ? (
                          <PowerOff aria-hidden="true" />
                        ) : (
                          <Power aria-hidden="true" />
                        )}
                        {trigger.enabled
                          ? translate("trigger.disable")
                          : translate("trigger.enable")}
                      </button>
                      <button
                        className="button small danger"
                        type="button"
                        onClick={() => setDeleteTarget(trigger)}
                        aria-label={translate("trigger.deleteNamed", { name: trigger.name })}
                      >
                        <Trash2 aria-hidden="true" />
                        {translate("common.delete")}
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
      setFormError(result.error || translate("trigger.reviewConfiguration"));
      return;
    }
    setBusy(true);
    setFormError("");
    try {
      const trigger = existing
        ? await api.updateTrigger(existing.id, result.payload)
        : await api.createTrigger(result.payload);
      notify(
        "ok",
        translate(existing ? "trigger.notify.updated" : "trigger.notify.createdDisabled", {
          name: trigger.name,
        }),
      );
      onSaved(trigger);
    } catch (cause) {
      setFormError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title={
        existing ? translate("trigger.edit", { name: existing.name }) : translate("trigger.add")
      }
      subtitle={translate("trigger.dialogSubtitle", { app: appKey })}
      onClose={onClose}
      wide
      id="triggerEditorDialog"
    >
      <form className="dialogForm" onSubmit={(event) => void submit(event)}>
        {formError ? <ErrorNotice message={formError} /> : null}
        {!existing ? (
          <div className="inlineNotice">{translate("trigger.newDisabledNotice")}</div>
        ) : null}
        <div className="formGrid">
          <Field label={translate("common.name")}>
            <input
              id="triggerName"
              value={draft.name}
              onChange={(event) => update("name", event.target.value)}
              placeholder="Partner orders"
              required
            />
          </Field>
          <Field
            label={translate("trigger.field.targetAction")}
            hint={translate("trigger.field.targetFixed", {
              actionKey: draft.action || "—",
            })}
          >
            <SelectControl
              value={draft.action}
              onChange={(value) => update("action", value)}
              ariaLabel={translate("trigger.field.targetAction")}
              options={actions.map((action) => ({
                value: action.action_key,
                label: actionDisplayName(action.display_name) || action.action_key,
                description: action.action_key,
              }))}
            />
          </Field>
        </div>

        <fieldset className="triggerKindFieldset" disabled={Boolean(existing)}>
          <legend>{translate("trigger.field.kind")}</legend>
          <div className="triggerKindPicker">
            <TriggerKindOption
              kind="webhook"
              selected={draft.kind === "webhook"}
              title={translate("trigger.kind.webhook")}
              description={translate("trigger.kind.webhookHint")}
              onSelect={(kind) => update("kind", kind)}
            />
            <TriggerKindOption
              kind="schedule"
              selected={draft.kind === "schedule"}
              title={translate("trigger.kind.schedule")}
              description={translate("trigger.kind.scheduleHint")}
              onSelect={(kind) => update("kind", kind)}
            />
            <TriggerKindOption
              kind="rabbitmq"
              selected={draft.kind === "rabbitmq"}
              title={translate("trigger.kind.rabbitmq")}
              description={translate("trigger.kind.rabbitmqHint")}
              onSelect={(kind) => update("kind", kind)}
            />
          </div>
          {existing ? <p className="fieldHint">{translate("trigger.kindImmutable")}</p> : null}
        </fieldset>

        {draft.kind === "webhook" ? (
          <WebhookFields draft={draft} existing={existing} update={update} />
        ) : null}
        {draft.kind === "schedule" ? <ScheduleFields draft={draft} update={update} /> : null}
        {draft.kind === "rabbitmq" ? (
          <RabbitMQFields draft={draft} existing={existing} update={update} />
        ) : null}
        <CompletionFields draft={draft} existing={existing} update={update} />

        <div className="dialogFooter">
          <span className="fieldHint">
            {existing?.has_secret ? translate("trigger.secretConfigured") : ""}
          </span>
          <div className="dialogFooterActions">
            <button className="button" type="button" onClick={onClose} disabled={busy}>
              {translate("common.cancel")}
            </button>
            <button className="button primary" type="submit" disabled={busy}>
              {busy
                ? translate("common.saving")
                : existing
                  ? translate("trigger.saveChanges")
                  : translate("trigger.create")}
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
        <h3>{translate("trigger.webhook.heading")}</h3>
        <p>{translate("trigger.webhook.description")}</p>
      </div>
      <div className="formGrid">
        <Field
          label={
            existing
              ? translate("trigger.webhook.replaceSecret")
              : translate("trigger.webhook.signingSecret")
          }
          hint={existing ? translate("trigger.secretRetain") : translate("trigger.secretGenerate")}
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
              {translate("common.generate")}
            </button>
          </div>
        </Field>
        <Field label={translate("trigger.field.inputMode")}>
          <SelectControl
            value={draft.inputMode}
            onChange={(value) => update("inputMode", value)}
            ariaLabel={translate("trigger.webhook.inputMode")}
            options={[
              { value: "json", label: translate("trigger.input.json") },
              { value: "raw", label: translate("trigger.input.raw") },
            ]}
          />
        </Field>
        <Field label={translate("trigger.field.signatureHeader")}>
          <input
            value={draft.signatureHeader}
            onChange={(event) => update("signatureHeader", event.target.value)}
          />
        </Field>
        <Field label={translate("trigger.field.deliveryIdHeader")}>
          <input
            value={draft.deliveryIDHeader}
            onChange={(event) => update("deliveryIDHeader", event.target.value)}
          />
        </Field>
        <Field label={translate("trigger.field.correlationHeader")}>
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
        <h3>{translate("trigger.schedule.heading")}</h3>
        <p>{translate("trigger.schedule.description")}</p>
      </div>
      <div className="formGrid">
        <Field label={translate("trigger.field.cron")} hint={translate("trigger.field.cronHint")}>
          <input
            className="mono"
            value={draft.cron}
            onChange={(event) => update("cron", event.target.value)}
            required
          />
        </Field>
        <Field
          label={translate("trigger.field.timezone")}
          hint={translate("trigger.field.timezoneHint")}
        >
          <input
            className="mono"
            value={draft.timezone}
            onChange={(event) => update("timezone", event.target.value)}
            required
          />
        </Field>
      </div>
      <Field
        label={translate("trigger.field.actionInput")}
        hint={translate("trigger.field.actionInputHint")}
      >
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
        <h3>{translate("trigger.rabbitmq.heading")}</h3>
        <p>{translate("trigger.rabbitmq.description")}</p>
      </div>
      <div className="formGrid">
        <Field label={translate("trigger.field.queue")}>
          <input
            className="mono"
            value={draft.queue}
            onChange={(event) => update("queue", event.target.value)}
            placeholder="orders.windforce"
            required
          />
        </Field>
        <Field
          label={
            existing
              ? translate("trigger.rabbitmq.replaceURL")
              : translate("trigger.rabbitmq.connectionURL")
          }
          hint={existing ? translate("trigger.urlRetain") : translate("trigger.urlStored")}
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
        <Field label={translate("trigger.field.concurrency")}>
          <input
            type="number"
            min="1"
            max="128"
            value={draft.concurrency}
            onChange={(event) => update("concurrency", event.target.value)}
          />
        </Field>
        <Field
          label={translate("trigger.field.prefetch")}
          hint={translate("trigger.field.prefetchHint")}
        >
          <input
            type="number"
            min="1"
            max="65535"
            value={draft.prefetch}
            onChange={(event) => update("prefetch", event.target.value)}
          />
        </Field>
        <Field label={translate("trigger.field.inputMode")}>
          <SelectControl
            value={draft.inputMode}
            onChange={(value) => update("inputMode", value)}
            ariaLabel={translate("trigger.rabbitmq.inputMode")}
            options={[
              { value: "json", label: translate("trigger.input.json") },
              { value: "raw", label: translate("trigger.input.raw") },
            ]}
          />
        </Field>
        <Field
          label={translate("trigger.field.deliveryIdHeader")}
          hint={translate("trigger.field.deliveryIdHint")}
        >
          <input
            value={draft.deliveryIDHeader}
            onChange={(event) => update("deliveryIDHeader", event.target.value)}
          />
        </Field>
        <Field
          label={translate("trigger.field.consumerTag")}
          hint={translate("trigger.field.consumerTagHint")}
        >
          <input
            value={draft.consumerTag}
            onChange={(event) => update("consumerTag", event.target.value)}
          />
        </Field>
      </div>
    </section>
  );
}

function CompletionFields({
  draft,
  existing,
  update,
}: {
  draft: TriggerDraft;
  existing: TriggerDefinition | null;
  update: <K extends keyof TriggerDraft>(key: K, value: TriggerDraft[K]) => void;
}) {
  return (
    <section className="triggerFormSection triggerCompletionSection">
      <div className="triggerFormSectionHeader">
        <h3>{translate("trigger.completion.heading")}</h3>
        <p>{translate("trigger.completion.description")}</p>
      </div>
      <div className="formGrid">
        <Field label={translate("trigger.field.outputDelivery")}>
          <SelectControl
            id="triggerCompletionMode"
            value={draft.completionMode}
            onChange={(value) => update("completionMode", value)}
            ariaLabel={translate("trigger.field.outputDelivery")}
            options={[
              {
                value: "poll",
                label: translate("trigger.output.poll"),
                description: translate("trigger.output.pollHint"),
              },
              {
                value: "callback",
                label: translate("trigger.output.callback"),
                description: translate("trigger.output.callbackHint"),
              },
              {
                value: "publish",
                label: translate("trigger.output.rabbitmq"),
                description: translate("trigger.output.rabbitmqHint"),
              },
              {
                value: "none",
                label: translate("trigger.output.none"),
                description: translate("trigger.output.noneHint"),
              },
            ]}
          />
        </Field>
        {draft.kind === "webhook" ? (
          <Field
            label={translate("trigger.field.httpResponse")}
            hint={translate("trigger.field.httpResponseHint")}
          >
            <SelectControl
              value={draft.responseMode}
              onChange={(value) => update("responseMode", value)}
              ariaLabel={translate("trigger.webhook.responseMode")}
              options={[
                {
                  value: "async",
                  label: translate("trigger.response.accepted"),
                  description: translate("trigger.response.acceptedHint"),
                },
                {
                  value: "wait",
                  label: translate("trigger.response.wait"),
                  description: translate("trigger.response.waitHint"),
                },
              ]}
            />
          </Field>
        ) : null}
        {draft.kind === "webhook" && draft.responseMode === "wait" ? (
          <Field label={translate("trigger.field.waitTimeout")}>
            <input
              type="number"
              min="1"
              max="60"
              value={draft.responseTimeout}
              onChange={(event) => update("responseTimeout", event.target.value)}
            />
          </Field>
        ) : null}
        {draft.completionMode === "callback" ? (
          <>
            <Field
              label={translate("trigger.field.callbackEndpoint")}
              hint={translate("trigger.field.callbackEndpointHint")}
            >
              <input
                className="mono"
                type="url"
                value={draft.callbackEndpoint}
                onChange={(event) => update("callbackEndpoint", event.target.value)}
                placeholder="https://partner.example.com/windforce/completions"
                required
              />
            </Field>
            <Field
              label={
                existing?.completion.mode === "callback"
                  ? translate("trigger.callback.replaceSecret")
                  : translate("trigger.callback.signingSecret")
              }
              hint={
                existing?.completion.mode === "callback"
                  ? translate("trigger.callback.secretRetain")
                  : translate("trigger.callback.secretHint")
              }
            >
              <div className="fieldWithAction">
                <input
                  type="password"
                  value={draft.callbackSigningSecret}
                  onChange={(event) => update("callbackSigningSecret", event.target.value)}
                  autoComplete="new-password"
                  required={existing?.completion.mode !== "callback"}
                />
                <button
                  className="button"
                  type="button"
                  onClick={() => update("callbackSigningSecret", randomSecret())}
                >
                  {translate("common.generate")}
                </button>
              </div>
            </Field>
          </>
        ) : null}
        {draft.completionMode === "publish" ? (
          <>
            <Field
              label={
                existing?.completion.mode === "publish"
                  ? translate("trigger.publish.replaceURL")
                  : translate("trigger.publish.connectionURL")
              }
              hint={
                existing?.completion.mode === "publish"
                  ? translate("trigger.urlRetain")
                  : translate("trigger.urlStored")
              }
            >
              <input
                type="password"
                value={draft.publishRabbitMQURL}
                onChange={(event) => update("publishRabbitMQURL", event.target.value)}
                placeholder="amqps://user:password@broker/vhost"
                autoComplete="new-password"
                required={existing?.completion.mode !== "publish"}
              />
            </Field>
            <Field
              label={translate("trigger.field.exchange")}
              hint={translate("trigger.field.exchangeHint")}
            >
              <input
                className="mono"
                value={draft.publishExchange}
                onChange={(event) => update("publishExchange", event.target.value)}
              />
            </Field>
            <Field label={translate("trigger.field.routingKey")}>
              <input
                className="mono"
                value={draft.publishRoutingKey}
                onChange={(event) => update("publishRoutingKey", event.target.value)}
                placeholder="windforce.completions"
                required
              />
            </Field>
          </>
        ) : null}
      </div>
      {draft.completionMode === "poll" ? (
        <div className="inlineNotice">{translate("trigger.response.securityNotice")}</div>
      ) : null}
      {draft.completionMode === "none" ? (
        <div className="inlineNotice warning">{translate("trigger.output.noneWarning")}</div>
      ) : null}
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
          <span className="fieldHint">{translate("trigger.secretPayloadHidden")}</span>
          <button className="button" type="button" onClick={onEdit}>
            <Pencil aria-hidden="true" />
            {translate("trigger.editConfiguration")}
          </button>
        </>
      }
    >
      <section className="sheetSection">
        <DefinitionList
          className="sheetFacts"
          items={[
            [translate("common.status"), <TriggerEnabledBadge enabled={trigger.enabled} />],
            [translate("trigger.detail.kind"), triggerKindLabel(trigger.kind)],
            [
              translate("trigger.detail.target"),
              <span className="mono">{`${trigger.app}/${trigger.action}`}</span>,
            ],
            [translate("trigger.detail.output"), completionPolicyLabel(trigger)],
            [
              translate("trigger.detail.httpResponse"),
              trigger.kind === "webhook"
                ? responsePolicyLabel(trigger)
                : translate("common.notApplicable"),
            ],
            [
              translate("trigger.detail.secret"),
              trigger.has_secret
                ? translate("trigger.secretConfiguredWriteOnly")
                : translate("common.notConfigured"),
            ],
            [
              translate("common.updated"),
              `${formatTime(trigger.updated_at)} · ${trigger.updated_by || "system"}`,
            ],
            [translate("trigger.detail.triggerID"), <span className="mono">{trigger.id}</span>],
          ]}
        />
        {endpoint ? (
          <div className="triggerEndpoint">
            <p className="fieldLabel">{translate("trigger.canonicalIngress")}</p>
            <code>{endpoint}</code>
            <p className="fieldHint">{translate("trigger.canonicalIngressHint")}</p>
          </div>
        ) : null}
        <h3>{translate("trigger.safeConfiguration")}</h3>
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
          <TriggerDeliveries deliveries={state.data.deliveries} workspace={settings.workspace} />
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
      notify("ok", translate("trigger.route.deleteRequested"));
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
          <h3>{translate("trigger.route.heading")}</h3>
          <p>{translate("trigger.route.description", { provider: routeProvider })}</p>
        </div>
        <button
          className="button small primary"
          type="button"
          onClick={() => setEditor("new")}
          aria-label={translate("trigger.route.add")}
        >
          <Plus aria-hidden="true" />
          {translate("common.add")}
        </button>
      </div>
      {bindings.length === 0 ? (
        <div className="triggerRoutesEmpty">
          <Globe2 aria-hidden="true" />
          <div>
            <strong>{translate("trigger.route.empty")}</strong>
            <p>{translate("trigger.route.emptyHint")}</p>
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
                    {" · "}
                    {translate("trigger.route.generation", { generation: binding.generation })}
                    {" · "}
                    {translate("trigger.route.updated", {
                      time: formatRelative(binding.updated_at),
                    })}
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
                    {translate("common.edit")}
                  </button>
                  <button
                    className="button small danger"
                    type="button"
                    onClick={() => setDeleteTarget(binding)}
                    disabled={deleting || busyID === binding.id}
                    aria-label={translate("trigger.route.deleteNamed", {
                      route: routeBindingAddress(binding),
                    })}
                  >
                    <Trash2 aria-hidden="true" />
                    {deleting ? translate("common.deleting") : translate("common.delete")}
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
      setFormError(translate("trigger.route.pathInvalid"));
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
      notify(
        "ok",
        translate(existing ? "trigger.route.notifyUpdated" : "trigger.route.notifyRequested"),
      );
      onSaved();
    } catch (cause) {
      setFormError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title={existing ? translate("trigger.route.edit") : translate("trigger.route.add")}
      subtitle={translate("trigger.route.dialogSubtitle", { name: trigger.name })}
      onClose={onClose}
      id="httpRouteBindingEditor"
    >
      <form className="dialogForm" onSubmit={(event) => void submit(event)}>
        {formError ? <ErrorNotice message={formError} /> : null}
        <Field
          label={translate("trigger.route.hostname")}
          hint={translate("trigger.route.hostnameHint")}
        >
          <input
            className="mono"
            value={hostname}
            onChange={(event) => setHostname(event.target.value)}
            placeholder="hooks.example.com"
            autoComplete="off"
          />
        </Field>
        <Field label={translate("trigger.route.path")} hint={translate("trigger.route.pathHint")}>
          <input
            className="mono"
            value={path}
            onChange={(event) => setPath(event.target.value)}
            placeholder="/hooks/my-app"
            required
          />
        </Field>
        <Field label={translate("trigger.route.provider")}>
          <div className="routeProviderValue">
            <Globe2 aria-hidden="true" />
            <span className="mono">{provider}</span>
          </div>
        </Field>
        <div className="inlineNotice">{translate("trigger.route.securityNotice")}</div>
        <div className="dialogFooter">
          <span className="fieldHint">{translate("trigger.route.pendingHint")}</span>
          <div className="dialogFooterActions">
            <button className="button" type="button" onClick={onClose} disabled={busy}>
              {translate("common.cancel")}
            </button>
            <button className="button primary" type="submit" disabled={busy}>
              {busy
                ? translate("common.saving")
                : existing
                  ? translate("trigger.route.save")
                  : translate("trigger.route.request")}
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
      title={translate("trigger.route.deleteTitle")}
      subtitle={routeBindingAddress(binding)}
      onClose={onClose}
      id="deleteHTTPRouteBindingDialog"
    >
      <div className="inlineNotice error" role="alert">
        {translate("trigger.route.deleteWarning")}
      </div>
      <div className="dialogFooter">
        <span />
        <div className="dialogFooterActions">
          <button className="button" type="button" onClick={onClose} disabled={busy}>
            {translate("common.cancel")}
          </button>
          <button className="button danger" type="button" onClick={onConfirm} disabled={busy}>
            <Trash2 aria-hidden="true" />
            {busy ? translate("common.requesting") : translate("trigger.route.delete")}
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
  if (state === "ready") return translate("trigger.state.ready");
  if (state === "error") return translate("trigger.state.error");
  if (state === "deleting") return translate("trigger.state.deleting");
  if (state === "deleted") return translate("trigger.state.deleted");
  return translate("trigger.state.pending");
}

function routeBindingAddress(binding: Pick<HTTPRouteBinding, "hostname" | "path">): string {
  return `${binding.hostname || translate("trigger.route.providerHostname")}${binding.path}`;
}

function suggestedRoutePath(trigger: TriggerDefinition): string {
  const app = trigger.app.toLowerCase().replace(/[^a-z0-9._-]+/g, "-");
  const name = trigger.name.toLowerCase().replace(/[^a-z0-9._-]+/g, "-");
  return `/hooks/${app}/${name}`.replace(/-+/g, "-");
}

function TriggerDeliveries({
  deliveries,
  workspace,
}: {
  deliveries: TriggerDelivery[];
  workspace: string;
}) {
  return (
    <section className="sheetSection">
      <h3>{translate("trigger.delivery.heading")}</h3>
      {deliveries.length === 0 ? (
        <p className="cellSub">{translate("trigger.delivery.empty")}</p>
      ) : (
        <div className="tableWrap">
          <table className="table triggerDeliveryTable">
            <thead>
              <tr>
                <th>{translate("trigger.delivery.state")}</th>
                <th>{translate("trigger.delivery.delivery")}</th>
                <th>{translate("trigger.delivery.run")}</th>
                <th>{translate("trigger.delivery.output")}</th>
                <th>{translate("trigger.delivery.when")}</th>
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
                    <TriggerCompletionBadge state={delivery.completion_state} />
                    {delivery.run_id && delivery.completion.mode === "poll" ? (
                      <span className="cellSub mono">
                        {`/api/v1/workspaces/${encodeURIComponent(workspace)}/runs/${encodeURIComponent(delivery.run_id)}/result`}
                      </span>
                    ) : null}
                    {delivery.completion_error_summary ? (
                      <span className="cellSub">{delivery.completion_error_summary}</span>
                    ) : null}
                  </td>
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

function TriggerCompletionBadge({ state }: { state: TriggerDelivery["completion_state"] }) {
  const className =
    state === "succeeded" || state === "available"
      ? "badge badge-good"
      : state === "failed"
        ? "badge badge-critical"
        : state === "retrying" || state === "delivering"
          ? "badge badge-warning"
          : "badge badge-neutral";
  return <span className={className}>{completionStateLabel(state)}</span>;
}

function completionStateLabel(state: TriggerDelivery["completion_state"]): string {
  if (state === "available") return translate("trigger.state.pollReady");
  if (state === "succeeded") return translate("trigger.state.delivered");
  if (state === "failed") return translate("trigger.state.failed");
  if (state === "retrying") return translate("trigger.state.retrying");
  if (state === "delivering") return translate("trigger.state.delivering");
  if (state === "pending") return translate("trigger.state.pending");
  if (state === "ignored") return translate("trigger.output.none");
  return translate("trigger.state.waiting");
}

function completionPolicyLabel(trigger: TriggerDefinition): string {
  if (trigger.completion.mode === "callback") {
    return translate("trigger.completion.callbackSummary", {
      endpoint:
        trigger.completion.callback?.endpoint || translate("trigger.completion.endpointMissing"),
    });
  }
  if (trigger.completion.mode === "publish") {
    const publish = trigger.completion.publish;
    return translate("trigger.completion.rabbitMQSummary", {
      exchange: publish?.exchange || translate("trigger.completion.defaultExchange"),
      routingKey: publish?.routing_key || translate("trigger.completion.routingKeyMissing"),
    });
  }
  if (trigger.completion.mode === "poll") return translate("trigger.completion.polling");
  return translate("trigger.output.none");
}

function responsePolicyLabel(trigger: TriggerDefinition): string {
  if (trigger.response.mode === "wait") {
    return translate("trigger.response.waitSummary", {
      seconds: trigger.response.timeout_seconds || 30,
    });
  }
  return translate("trigger.response.accepted");
}

function TriggerAuditTrail({ audit }: { audit: TriggerAudit[] }) {
  return (
    <section className="sheetSection">
      <h3>{translate("trigger.audit.heading")}</h3>
      {audit.length === 0 ? (
        <p className="cellSub">{translate("trigger.audit.empty")}</p>
      ) : (
        <ol className="triggerAuditList">
          {audit.map((event) => (
            <li key={event.id}>
              <span className="triggerAuditMarker" aria-hidden="true" />
              <div>
                <strong>{auditLabel(event.kind)}</strong>
                <p>{event.detail || translate("trigger.audit.noDetail")}</p>
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
      title={translate("trigger.delete.titleNamed", { name: trigger.name })}
      subtitle={translate("trigger.delete.subtitle")}
      onClose={onClose}
      id="deleteTriggerDialog"
    >
      <div className="inlineNotice error" role="alert">
        {translate("trigger.delete.warning")}
      </div>
      <div className="dialogFooter">
        <span />
        <div className="dialogFooterActions">
          <button className="button" type="button" onClick={onClose} disabled={busy}>
            {translate("common.cancel")}
          </button>
          <button className="button danger" type="button" onClick={onConfirm} disabled={busy}>
            <Trash2 aria-hidden="true" />
            {busy ? translate("common.deleting") : translate("trigger.delete.action")}
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
      {enabled ? translate("common.enabled") : translate("common.disabled")}
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
  if (state === "admitted") return translate("trigger.state.admitted");
  if (state === "retryable") return translate("trigger.state.retrying");
  return translate("trigger.state.rejected");
}

function auditLabel(kind: string): string {
  if (kind === "created") return translate("trigger.audit.created");
  if (kind === "updated") return translate("trigger.audit.updated");
  if (kind === "enabled") return translate("trigger.audit.enabled");
  if (kind === "disabled") return translate("trigger.audit.disabled");
  if (kind === "deleted") return translate("trigger.audit.deleted");
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
