import { RefreshCw } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { EmptyState, ErrorNotice, Loading, Panel, SelectControl } from "../components/ui";
import type { WebhookDeliveryDetail, WebhookDeliveryState, WebhookSubscription } from "../lib/api";
import { errorMessage } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatRelative, formatTime } from "../lib/format";
import { translate } from "../shared/i18n";
import { WebhookDeliverySheet } from "./WebhookDeliverySheet";
import { WebhookDeliveryStatus, webhookEventLabel } from "./WebhookStatus";

export function WebhookDeliveries({ subscription }: { subscription: WebhookSubscription }) {
  const { api, notify } = useApp();
  const [filter, setFilter] = useState<WebhookDeliveryState | "">("");
  const [revision, setRevision] = useState(0);
  const [additional, setAdditional] = useState<WebhookDeliveryDetail[]>([]);
  const [nextCursor, setNextCursor] = useState("");
  const [loadingMore, setLoadingMore] = useState(false);
  const [selected, setSelected] = useState<WebhookDeliveryDetail | null>(null);
  const [retrying, setRetrying] = useState(false);
  const [actionError, setActionError] = useState("");

  const state = useAsync(
    () => api.webhookDeliveries(subscription.id, { state: filter, limit: 25 }),
    [api, subscription.id, filter, revision],
  );

  useEffect(() => {
    if (!state.data) return;
    setAdditional([]);
    setNextCursor(state.data.next_cursor || "");
  }, [state.data]);

  const deliveries = useMemo(
    () => [...(state.data?.items || []), ...additional],
    [additional, state.data],
  );

  async function loadMore() {
    if (!nextCursor) return;
    setLoadingMore(true);
    setActionError("");
    try {
      const page = await api.webhookDeliveries(subscription.id, {
        state: filter,
        limit: 25,
        cursor: nextCursor,
      });
      setAdditional((current) => [...current, ...page.items]);
      setNextCursor(page.next_cursor || "");
    } catch (cause) {
      setActionError(errorMessage(cause));
    } finally {
      setLoadingMore(false);
    }
  }

  async function retry() {
    if (!selected) return;
    setRetrying(true);
    setActionError("");
    try {
      const result = await api.retryWebhookDelivery(selected.delivery.id);
      setSelected(result);
      setRevision((current) => current + 1);
      notify("ok", translate("webhook.retryQueued"));
    } catch (cause) {
      setActionError(errorMessage(cause));
    } finally {
      setRetrying(false);
    }
  }

  return (
    <Panel
      title={translate("webhook.deliveryHistory")}
      subtitle={translate("webhook.deliveryHistoryHint")}
      actions={
        <button
          className="button"
          type="button"
          onClick={() => setRevision((current) => current + 1)}
        >
          <RefreshCw size={16} aria-hidden="true" />
          {translate("common.refresh")}
        </button>
      }
    >
      <div className="deliveryToolbar">
        <label className="filterField">
          <span>{translate("common.status")}</span>
          <SelectControl
            value={filter}
            onChange={setFilter}
            ariaLabel={translate("webhook.deliveryStatus")}
            options={[
              { value: "", label: translate("webhook.status.all") },
              { value: "pending", label: translate("webhook.status.pending") },
              { value: "delivering", label: translate("webhook.status.delivering") },
              { value: "retrying", label: translate("webhook.status.retrying") },
              { value: "succeeded", label: translate("webhook.status.delivered") },
              { value: "failed", label: translate("webhook.status.failed") },
              { value: "canceled", label: translate("webhook.status.canceled") },
            ]}
          />
        </label>
        <span className="fieldHint">
          {translate("webhook.deliveryRecordsLoaded", { count: deliveries.length })}
        </span>
      </div>
      {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
      {actionError ? <ErrorNotice message={actionError} /> : null}
      {state.loading && !state.data ? (
        <Loading label={translate("webhook.loadingDeliveries")} />
      ) : null}
      {state.data && deliveries.length === 0 ? (
        <EmptyState title={translate("webhook.noDeliveriesMatch")} />
      ) : null}
      {deliveries.length ? (
        <div className="tableWrap">
          <table className="table webhookDeliveryTable" id="webhookDeliveries">
            <thead>
              <tr>
                <th>{translate("common.status")}</th>
                <th>{translate("webhook.event")}</th>
                <th>{translate("webhook.attempt")}</th>
                <th>{translate("common.result")}</th>
                <th>{translate("common.created")}</th>
              </tr>
            </thead>
            <tbody>
              {deliveries.map((detail) => {
                const delivery = detail.delivery;
                return (
                  <tr key={delivery.id}>
                    <td>
                      <WebhookDeliveryStatus state={delivery.state} />
                    </td>
                    <td>
                      <button
                        className="tableLink"
                        type="button"
                        onClick={() => setSelected(detail)}
                      >
                        {webhookEventLabel(detail.event.type)}
                      </button>
                      <span className="cellSub mono">{delivery.id}</span>
                    </td>
                    <td>
                      <span className="cellTitle">{delivery.attempt}</span>
                    </td>
                    <td>
                      <span className="cellTitle">
                        {delivery.response_status ? `HTTP ${delivery.response_status}` : "—"}
                      </span>
                      {delivery.error_summary ? (
                        <span className="cellSub deliveryErrorSummary">
                          {delivery.error_summary}
                        </span>
                      ) : null}
                    </td>
                    <td title={formatTime(delivery.created_at)}>
                      <span className="cellTitle">{formatRelative(delivery.created_at)}</span>
                      <span className="cellSub">{formatTime(delivery.created_at)}</span>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      ) : null}
      {nextCursor ? (
        <div className="tableFooter">
          <button className="button" type="button" disabled={loadingMore} onClick={loadMore}>
            {loadingMore ? translate("common.loading") : translate("common.loadMore")}
          </button>
        </div>
      ) : null}
      {selected ? (
        <WebhookDeliverySheet
          detail={selected}
          subscriptionActive={!subscription.deleted_at && subscription.enabled}
          retrying={retrying}
          onRetry={retry}
          onClose={() => setSelected(null)}
        />
      ) : null}
    </Panel>
  );
}
