import { KeyRound, Search, Trash2 } from "lucide-react";
import { type FormEvent, useEffect, useMemo, useState } from "react";
import { ErrorNotice, Field, Modal, Panel } from "../components/ui";
import type { AppSummary, WebhookSubscription } from "../lib/api";
import { errorMessage, webhookAppKeys } from "../lib/api";
import { useApp } from "../lib/app-context";
import { formatTime } from "../lib/format";
import { translate } from "../shared/i18n";
import { WebhookEventPicker } from "./WebhookEventPicker";
import { WebhookSecretDialog } from "./WebhookSecretDialog";
import { WebhookSubscriptionStatus, webhookEventLabel } from "./WebhookStatus";

type Props = {
  subscription: WebhookSubscription;
  apps: AppSummary[];
  onUpdated: (subscription: WebhookSubscription) => void;
  onDeleted: () => void;
};

export function WebhookOverview({ subscription, apps, onUpdated, onDeleted }: Props) {
  const { api, notify } = useApp();
  const [name, setName] = useState(subscription.name);
  const [endpoint, setEndpoint] = useState("");
  const [enabled, setEnabled] = useState(subscription.enabled);
  const [eventTypes, setEventTypes] = useState(subscription.event_types || []);
  const [scope, setScope] = useState<"all" | "selected">(
    webhookAppKeys(subscription).length ? "selected" : "all",
  );
  const [selectedApps, setSelectedApps] = useState(webhookAppKeys(subscription));
  const [search, setSearch] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [rotateOpen, setRotateOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteConfirmation, setDeleteConfirmation] = useState("");
  const [secret, setSecret] = useState("");

  useEffect(() => {
    setName(subscription.name);
    setEndpoint("");
    setEnabled(subscription.enabled);
    setEventTypes(subscription.event_types || []);
    setScope(webhookAppKeys(subscription).length ? "selected" : "all");
    setSelectedApps(webhookAppKeys(subscription));
  }, [subscription]);

  const availableApps = useMemo(() => {
    const byKey = new Map(apps.map((app) => [app.app_key, app]));
    for (const appKey of selectedApps) {
      if (!byKey.has(appKey)) {
        byKey.set(appKey, {
          id: appKey,
          workspace_id: subscription.workspace_id,
          app_key: appKey,
          git_source_id: 0,
          commit_sha: "",
          entrypoint: translate("webhook.appNotRegistered"),
          tag: "",
          timeout_s: 0,
          script_lang: "",
          bundle_status: "missing",
          updated_at: subscription.updated_at,
          effective_route_tag: "",
          actions_count: 0,
        });
      }
    }
    const query = search.trim().toLowerCase();
    return [...byKey.values()]
      .filter((app) => !query || app.app_key.toLowerCase().includes(query))
      .sort((left, right) => left.app_key.localeCompare(right.app_key));
  }, [apps, search, selectedApps, subscription.updated_at, subscription.workspace_id]);

  function toggleApp(appKey: string) {
    setSelectedApps((current) =>
      current.includes(appKey)
        ? current.filter((key) => key !== appKey)
        : [...current, appKey].sort(),
    );
  }

  async function save(event: FormEvent) {
    event.preventDefault();
    const normalizedName = name.trim();
    const nextApps = scope === "all" ? [] : selectedApps;
    if (!normalizedName) {
      setError(translate("webhook.validation.name"));
      return;
    }
    if (scope === "selected" && nextApps.length === 0) {
      setError(translate("webhook.validation.appScope"));
      return;
    }
    if (eventTypes.length === 0) {
      setError(translate("webhook.validation.eventTypes"));
      return;
    }
    const payload: Parameters<typeof api.updateWebhookSubscription>[1] = {};
    if (normalizedName !== subscription.name) payload.name = normalizedName;
    if (endpoint.trim()) payload.endpoint = endpoint.trim();
    if (enabled !== subscription.enabled) payload.enabled = enabled;
    if (JSON.stringify(eventTypes) !== JSON.stringify(subscription.event_types || []))
      payload.event_types = eventTypes;
    if (JSON.stringify(nextApps) !== JSON.stringify(webhookAppKeys(subscription)))
      payload.app_keys = nextApps;
    if (Object.keys(payload).length === 0) {
      notify("info", translate("webhook.noSettingsChanged"));
      return;
    }
    setBusy(true);
    setError("");
    try {
      const result = await api.updateWebhookSubscription(subscription.id, payload);
      onUpdated(result.subscription);
      notify("ok", translate("webhook.saved", { name: result.subscription.name }));
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function rotateSecret() {
    setBusy(true);
    setError("");
    try {
      const result = await api.updateWebhookSubscription(subscription.id, {
        rotate_signing_secret: true,
      });
      setRotateOpen(false);
      onUpdated(result.subscription);
      setSecret(result.signing_secret || "");
      notify("ok", translate("webhook.secretRotated"));
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    setBusy(true);
    setError("");
    try {
      await api.deleteWebhookSubscription(subscription.id);
      notify("ok", translate("webhook.deleted", { name: subscription.name }));
      onDeleted();
    } catch (cause) {
      setError(errorMessage(cause));
      setBusy(false);
    }
  }

  const deleted = Boolean(subscription.deleted_at);
  return (
    <div className="webhookDetailStack">
      <Panel
        title={translate("webhook.configuration")}
        subtitle={translate("webhook.configurationHint")}
      >
        <div className="webhookConfigSummary">
          <div>
            <span className="fieldLabel">{translate("common.status")}</span>
            <WebhookSubscriptionStatus enabled={subscription.enabled} deleted={deleted} />
          </div>
          <div>
            <span className="fieldLabel">{translate("webhook.receiverHost")}</span>
            <strong className="mono">{subscription.endpoint_summary}</strong>
            <span className="fieldHint">{translate("webhook.pathsHidden")}</span>
          </div>
          <div>
            <span className="fieldLabel">{translate("webhook.events")}</span>
            <strong>{(subscription.event_types || []).map(webhookEventLabel).join(", ")}</strong>
            <span className="fieldHint mono">{(subscription.event_types || []).join(", ")}</span>
          </div>
          <div>
            <span className="fieldLabel">{translate("webhook.lastUpdated")}</span>
            <strong>{formatTime(subscription.updated_at)}</strong>
            <span className="fieldHint">
              {translate("common.by", {
                actor: subscription.updated_by || translate("common.system"),
              })}
            </span>
          </div>
        </div>

        {deleted ? (
          <div className="inlineNotice warning">{translate("webhook.deletedReadOnly")}</div>
        ) : (
          <form className="webhookEditForm" onSubmit={save}>
            <div className="formGrid">
              <Field label={translate("common.name")} hint={translate("webhook.editNameHint")}>
                <input
                  id="webhookEditName"
                  maxLength={200}
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                />
              </Field>
              <Field
                label={translate("webhook.replaceEndpoint")}
                hint={translate("webhook.replaceEndpointHint")}
              >
                <input
                  id="webhookEditEndpoint"
                  type="url"
                  value={endpoint}
                  onChange={(event) => setEndpoint(event.target.value)}
                  placeholder="https://hooks.example.com/windforce"
                  spellCheck={false}
                />
              </Field>
            </div>
            <label className="toggleField">
              <input
                type="checkbox"
                checked={enabled}
                onChange={(event) => setEnabled(event.target.checked)}
              />
              <span>
                <strong>{translate("webhook.enableDeliveries")}</strong>
                <small>{translate("webhook.enableDeliveriesHint")}</small>
              </span>
            </label>

            <WebhookEventPicker selected={eventTypes} onChange={setEventTypes} disabled={busy} />

            <fieldset className="webhookScopeFieldset">
              <legend>{translate("webhook.appScope")}</legend>
              <div className="segmented webhookScopeMode">
                <button
                  type="button"
                  className={scope === "all" ? "segment active" : "segment"}
                  onClick={() => setScope("all")}
                >
                  {translate("webhook.allApps")}
                </button>
                <button
                  type="button"
                  className={scope === "selected" ? "segment active" : "segment"}
                  onClick={() => setScope("selected")}
                >
                  {translate("webhook.selectedApps")}
                </button>
              </div>
              {scope === "selected" ? (
                <div className="appScopePicker compact">
                  <label className="scopeSearch">
                    <Search size={16} aria-hidden="true" />
                    <input
                      aria-label={translate("webhook.filterApps")}
                      placeholder={translate("webhook.filterApps")}
                      value={search}
                      onChange={(event) => setSearch(event.target.value)}
                    />
                  </label>
                  <div className="appScopeList" id="webhookEditAppScope">
                    {availableApps.map((app) => (
                      <label className="appScopeOption" key={app.app_key}>
                        <input
                          type="checkbox"
                          checked={selectedApps.includes(app.app_key)}
                          onChange={() => toggleApp(app.app_key)}
                        />
                        <span>
                          <strong>{app.app_key}</strong>
                          <small>{app.entrypoint}</small>
                        </span>
                      </label>
                    ))}
                  </div>
                </div>
              ) : (
                <p className="fieldHint">{translate("webhook.receiveEveryApp")}</p>
              )}
            </fieldset>
            {error ? <ErrorNotice message={error} /> : null}
            <div className="formActions">
              <button
                className="button primary"
                type="submit"
                disabled={busy}
                id="saveWebhookButton"
              >
                {busy ? translate("common.saving") : translate("common.saveChanges")}
              </button>
            </div>
          </form>
        )}
      </Panel>

      <Panel
        title={translate("webhook.signingSecret")}
        subtitle={translate("webhook.signingSecretHint")}
      >
        <div className="webhookSecurityRow">
          <div className="securityIdentity">
            <KeyRound size={18} aria-hidden="true" />
            <div>
              <strong>
                {subscription.has_signing_secret
                  ? translate("webhook.signingEnabled")
                  : translate("webhook.noSigningSecret")}
              </strong>
              <p>{translate("webhook.rotationHint")}</p>
            </div>
          </div>
          {!deleted ? (
            <button className="button" type="button" onClick={() => setRotateOpen(true)}>
              {translate("webhook.rotateSecret")}
            </button>
          ) : null}
        </div>
      </Panel>

      {!deleted ? (
        <Panel title={translate("webhook.delete")} subtitle={translate("webhook.deleteHint")}>
          <div className="dangerZoneRow">
            <p>{translate("webhook.deleteIrreversible")}</p>
            <button className="button danger" type="button" onClick={() => setDeleteOpen(true)}>
              <Trash2 size={16} aria-hidden="true" />
              {translate("webhook.delete")}
            </button>
          </div>
        </Panel>
      ) : null}

      {rotateOpen ? (
        <Modal
          title={translate("webhook.rotateConfirm")}
          subtitle={translate("webhook.rotateConfirmHint")}
          onClose={() => setRotateOpen(false)}
        >
          <div className="dialogFooter">
            <button className="button" type="button" onClick={() => setRotateOpen(false)}>
              {translate("common.cancel")}
            </button>
            <button className="button primary" type="button" disabled={busy} onClick={rotateSecret}>
              {busy ? translate("webhook.rotating") : translate("webhook.rotateSecret")}
            </button>
          </div>
        </Modal>
      ) : null}
      {deleteOpen ? (
        <Modal
          title={translate("webhook.deleteConfirm")}
          subtitle={translate("webhook.deleteConfirmHint")}
          onClose={() => setDeleteOpen(false)}
        >
          <Field label={translate("webhook.typeToConfirm", { name: subscription.name })}>
            <input
              value={deleteConfirmation}
              onChange={(event) => setDeleteConfirmation(event.target.value)}
            />
          </Field>
          <div className="dialogFooter">
            <button className="button" type="button" onClick={() => setDeleteOpen(false)}>
              {translate("common.cancel")}
            </button>
            <button
              className="button danger"
              type="button"
              disabled={busy || deleteConfirmation !== subscription.name}
              onClick={remove}
            >
              {busy ? translate("common.deleting") : translate("webhook.delete")}
            </button>
          </div>
        </Modal>
      ) : null}
      {secret ? (
        <WebhookSecretDialog secret={secret} endpoint={endpoint} onClose={() => setSecret("")} />
      ) : null}
    </div>
  );
}
