import { ArrowLeft, Search } from "lucide-react";
import { type FormEvent, useMemo, useState } from "react";
import { Layout } from "../components/Layout";
import { SettingsNav } from "../components/SettingsNav";
import { ErrorNotice, Field, Loading, Panel } from "../components/ui";
import { DEFAULT_WEBHOOK_EVENT_TYPES, WebhookEventPicker } from "../features/WebhookEventPicker";
import { WebhookSecretDialog } from "../features/WebhookSecretDialog";
import { errorMessage, type WebhookSubscriptionMutation } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { Link, useRouter } from "../lib/router";
import { translate } from "../shared/i18n";

export function WebhookCreatePage() {
  const { api, notify } = useApp();
  const { navigate } = useRouter();
  const apps = useAsync(() => api.apps(), [api]);
  const [name, setName] = useState("");
  const [endpoint, setEndpoint] = useState("");
  const [eventTypes, setEventTypes] = useState<string[]>(DEFAULT_WEBHOOK_EVENT_TYPES);
  const [scope, setScope] = useState<"all" | "selected">("all");
  const [selectedApps, setSelectedApps] = useState<string[]>([]);
  const [search, setSearch] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [created, setCreated] = useState<WebhookSubscriptionMutation | null>(null);

  const visibleApps = useMemo(() => {
    const query = search.trim().toLowerCase();
    const rows = apps.data?.apps || [];
    return query ? rows.filter((app) => app.app_key.toLowerCase().includes(query)) : rows;
  }, [apps.data, search]);

  function toggleApp(appKey: string) {
    setSelectedApps((current) =>
      current.includes(appKey)
        ? current.filter((key) => key !== appKey)
        : [...current, appKey].sort(),
    );
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    const normalizedName = name.trim();
    const normalizedEndpoint = endpoint.trim();
    if (!normalizedName || !normalizedEndpoint) {
      setError(translate("webhook.validation.nameEndpoint"));
      return;
    }
    if (scope === "selected" && selectedApps.length === 0) {
      setError(translate("webhook.validation.appScope"));
      return;
    }
    if (eventTypes.length === 0) {
      setError(translate("webhook.validation.eventTypes"));
      return;
    }
    setBusy(true);
    setError("");
    try {
      const result = await api.createWebhookSubscription({
        name: normalizedName,
        endpoint: normalizedEndpoint,
        event_types: eventTypes,
        app_keys: scope === "all" ? [] : selectedApps,
        enabled: true,
      });
      setCreated(result);
      notify("ok", translate("webhook.created", { name: result.subscription.name }));
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  function finish() {
    if (created) navigate(`/settings/webhooks/${created.subscription.id}`);
  }

  return (
    <Layout
      title={translate("webhook.create")}
      subtitle={translate("webhook.createHint")}
      actions={
        <Link className="button" to="/settings/webhooks">
          <ArrowLeft size={16} aria-hidden="true" />
          {translate("webhook.back")}
        </Link>
      }
    >
      <SettingsNav />
      <form className="webhookFormLayout" onSubmit={submit}>
        <Panel title={translate("webhook.receiver")} subtitle={translate("webhook.receiverHint")}>
          <div className="formGrid">
            <Field label={translate("common.name")} hint={translate("webhook.nameHint")}>
              <input
                id="webhookName"
                maxLength={200}
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder={translate("webhook.namePlaceholder")}
              />
            </Field>
            <Field
              label={translate("webhook.endpointURL")}
              hint={translate("webhook.endpointHint")}
            >
              <input
                id="webhookEndpoint"
                type="url"
                value={endpoint}
                onChange={(event) => setEndpoint(event.target.value)}
                placeholder="https://hooks.example.com/windforce"
                spellCheck={false}
              />
            </Field>
          </div>
        </Panel>

        <Panel
          title={translate("webhook.eventSelection")}
          subtitle={translate("webhook.eventSelectionHint")}
        >
          <WebhookEventPicker selected={eventTypes} onChange={setEventTypes} disabled={busy} />
        </Panel>

        <Panel title={translate("webhook.appScope")} subtitle={translate("webhook.appScopeHint")}>
          <fieldset className="segmented webhookScopeMode">
            <legend className="sr-only">{translate("webhook.appScope")}</legend>
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
          </fieldset>
          {scope === "selected" ? (
            <div className="appScopePicker">
              <label className="scopeSearch">
                <Search size={16} aria-hidden="true" />
                <input
                  aria-label={translate("webhook.filterApps")}
                  placeholder={translate("webhook.filterApps")}
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                />
              </label>
              {apps.error ? <ErrorNotice message={apps.error} onRetry={apps.reload} /> : null}
              {apps.loading && !apps.data ? <Loading label={translate("apps.loading")} /> : null}
              {apps.data ? (
                <div className="appScopeList" id="webhookAppScope">
                  {visibleApps.map((app) => (
                    <label className="appScopeOption" key={`${app.git_source_id}-${app.app_key}`}>
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
                  {visibleApps.length === 0 ? (
                    <p className="fieldHint">{translate("webhook.noAppsMatch")}</p>
                  ) : null}
                </div>
              ) : null}
            </div>
          ) : (
            <p className="fieldHint">{translate("webhook.allAppsDeliveryHint")}</p>
          )}
        </Panel>

        {error ? <ErrorNotice message={error} /> : null}
        <div className="formActions webhookFormActions">
          <Link className="button" to="/settings/webhooks">
            {translate("common.cancel")}
          </Link>
          <button className="button primary" type="submit" disabled={busy} id="createWebhookButton">
            {busy ? translate("common.creating") : translate("webhook.create")}
          </button>
        </div>
      </form>

      {created?.signing_secret ? (
        <WebhookSecretDialog secret={created.signing_secret} endpoint={endpoint} onClose={finish} />
      ) : null}
    </Layout>
  );
}
