import { Plus, RefreshCw } from "lucide-react";
import { useMemo, useState } from "react";
import { Layout } from "../components/Layout";
import { SettingsNav } from "../components/SettingsNav";
import { EmptyState, ErrorNotice, Loading, Panel } from "../components/ui";
import { WebhookDeliveryStatus, WebhookSubscriptionStatus } from "../features/WebhookStatus";
import { type WebhookDeliveryDetail, type WebhookSubscription, webhookAppKeys } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatRelative, formatTime } from "../lib/format";
import { Link } from "../lib/router";
import { translate } from "../shared/i18n";

type WebhookRow = {
  subscription: WebhookSubscription;
  lastDelivery: WebhookDeliveryDetail | null;
};

export function WebhookSettingsPage() {
  const { api } = useApp();
  const [search, setSearch] = useState("");
  const [includeDeleted, setIncludeDeleted] = useState(false);
  const state = useAsync(async () => {
    const subscriptions = await api.webhookSubscriptions(includeDeleted);
    return Promise.all(
      subscriptions.map(async (subscription): Promise<WebhookRow> => {
        try {
          const deliveries = await api.webhookDeliveries(subscription.id, { limit: 1 });
          return { subscription, lastDelivery: deliveries.items[0] || null };
        } catch {
          return { subscription, lastDelivery: null };
        }
      }),
    );
  }, [api, includeDeleted]);

  const rows = useMemo(() => {
    const query = search.trim().toLowerCase();
    if (!state.data || !query) return state.data || [];
    return state.data.filter(({ subscription }) =>
      [subscription.name, subscription.endpoint_summary, ...webhookAppKeys(subscription)].some(
        (value) => value.toLowerCase().includes(query),
      ),
    );
  }, [search, state.data]);

  const enabledCount =
    state.data?.filter(({ subscription }) => subscription.enabled && !subscription.deleted_at)
      .length || 0;
  const failedCount =
    state.data?.filter(({ lastDelivery }) => lastDelivery?.delivery.state === "failed").length || 0;

  return (
    <Layout
      title={translate("webhook.webhooks")}
      subtitle={translate("webhook.settingsSubtitle")}
      actions={
        <>
          <input
            className="searchInput"
            aria-label={translate("webhook.filter")}
            placeholder={translate("webhook.filter")}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
          <button
            className="button"
            type="button"
            onClick={() => state.reload()}
            title={translate("webhook.refreshWebhooks")}
          >
            <RefreshCw size={16} aria-hidden="true" />
            {translate("common.refresh")}
          </button>
          <Link className="button primary" to="/settings/webhooks/new">
            <Plus size={16} aria-hidden="true" />
            {translate("webhook.create")}
          </Link>
        </>
      }
    >
      <SettingsNav />
      <section className="webhookSummaryBar" aria-label={translate("webhook.summary")}>
        <span>{translate("webhook.enabledCount", { count: enabledCount })}</span>
        <span className={failedCount ? "summaryCritical" : undefined}>
          {translate("webhook.failedCount", { count: failedCount })}
        </span>
        <label className="historyToggle">
          <input
            type="checkbox"
            checked={includeDeleted}
            onChange={(event) => setIncludeDeleted(event.target.checked)}
          />
          {translate("webhook.showDeleted")}
        </label>
      </section>

      {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
      {state.loading && !state.data ? (
        <Loading label={translate("webhook.loadingWebhooks")} />
      ) : null}
      {state.data ? (
        <Panel
          title={translate("webhook.subscriptions")}
          subtitle={translate("webhook.currentViewCount", { count: rows.length })}
        >
          {rows.length === 0 ? (
            <EmptyState
              title={search ? translate("webhook.noMatches") : translate("webhook.noSubscriptions")}
            >
              {!search ? (
                <Link className="button primary" to="/settings/webhooks/new">
                  {translate("webhook.create")}
                </Link>
              ) : null}
            </EmptyState>
          ) : (
            <div className="tableWrap">
              <table className="table webhookTable" id="webhookList">
                <thead>
                  <tr>
                    <th>{translate("webhook.title")}</th>
                    <th>{translate("webhook.endpoint")}</th>
                    <th>{translate("webhook.appScope")}</th>
                    <th>{translate("webhook.lastDelivery")}</th>
                    <th>{translate("common.updated")}</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map(({ subscription, lastDelivery }) => (
                    <tr key={subscription.id}>
                      <td>
                        <Link className="cellTitle" to={`/settings/webhooks/${subscription.id}`}>
                          {subscription.name}
                        </Link>
                        <span className="cellSub webhookStatusLine">
                          <WebhookSubscriptionStatus
                            enabled={subscription.enabled}
                            deleted={Boolean(subscription.deleted_at)}
                          />
                        </span>
                      </td>
                      <td>
                        <span className="mono cellTitle">{subscription.endpoint_summary}</span>
                        <span className="cellSub">{translate("webhook.endpointHidden")}</span>
                      </td>
                      <td>
                        {webhookAppKeys(subscription).length === 0 ? (
                          <span className="cellTitle">{translate("webhook.allApps")}</span>
                        ) : (
                          <>
                            <span className="cellTitle">
                              {webhookAppKeys(subscription).slice(0, 2).join(", ")}
                            </span>
                            {webhookAppKeys(subscription).length > 2 ? (
                              <span className="cellSub">
                                {translate("common.moreCount", {
                                  count: webhookAppKeys(subscription).length - 2,
                                })}
                              </span>
                            ) : null}
                          </>
                        )}
                      </td>
                      <td>
                        {lastDelivery ? (
                          <>
                            <WebhookDeliveryStatus state={lastDelivery.delivery.state} />
                            <span
                              className="cellSub"
                              title={formatTime(lastDelivery.delivery.created_at)}
                            >
                              {formatRelative(lastDelivery.delivery.created_at)}
                            </span>
                          </>
                        ) : (
                          <span className="cellSub">{translate("webhook.noDeliveriesYet")}</span>
                        )}
                      </td>
                      <td title={formatTime(subscription.updated_at)}>
                        <span className="cellTitle">{formatRelative(subscription.updated_at)}</span>
                        <span className="cellSub">{subscription.updated_by}</span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Panel>
      ) : null}
    </Layout>
  );
}
