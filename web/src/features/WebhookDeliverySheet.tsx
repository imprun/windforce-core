import { RotateCcw } from "lucide-react";
import { DefinitionList, JsonBlock, Sheet } from "../components/ui";
import type { WebhookDeliveryDetail } from "../lib/api";
import { formatTime } from "../lib/format";
import { translate } from "../shared/i18n";
import { WebhookDeliveryStatus, webhookEventLabel } from "./WebhookStatus";

export function WebhookDeliverySheet({
  detail,
  subscriptionActive,
  retrying,
  onRetry,
  onClose,
}: {
  detail: WebhookDeliveryDetail;
  subscriptionActive: boolean;
  retrying: boolean;
  onRetry: () => void;
  onClose: () => void;
}) {
  const { delivery, event } = detail;
  const canRetry = delivery.state === "failed" && subscriptionActive;
  return (
    <Sheet
      title={translate("webhook.deliveryDetail")}
      subtitle={delivery.id}
      onClose={onClose}
      id="webhookDeliverySheet"
      actions={
        canRetry ? (
          <button className="button primary" type="button" disabled={retrying} onClick={onRetry}>
            <RotateCcw size={16} aria-hidden="true" />
            {retrying ? translate("webhook.queuingRetry") : translate("webhook.retryDelivery")}
          </button>
        ) : (
          <span className="fieldHint">
            {delivery.state === "failed"
              ? translate("webhook.enableBeforeRetry")
              : translate("webhook.failedOnlyRetry")}
          </span>
        )
      }
    >
      <div className="sheetSection deliveryIdentity">
        <WebhookDeliveryStatus state={delivery.state} />
        <strong>{webhookEventLabel(event.type)}</strong>
      </div>
      <div className="sheetSection">
        <h3>{translate("webhook.attempt")}</h3>
        <DefinitionList
          className="sheetFacts"
          items={[
            [translate("webhook.attempts"), delivery.attempt],
            [translate("webhook.httpResponse"), delivery.response_status ?? "—"],
            [
              translate("webhook.latency"),
              delivery.latency_ms != null ? `${delivery.latency_ms} ms` : "—",
            ],
            [translate("common.created"), formatTime(delivery.created_at)],
            [
              translate("webhook.completed"),
              delivery.completed_at ? formatTime(delivery.completed_at) : "—",
            ],
            [
              translate("webhook.nextAttempt"),
              delivery.state === "retrying" || delivery.state === "pending"
                ? formatTime(delivery.next_attempt_at)
                : "—",
            ],
          ]}
        />
        {delivery.error_summary ? (
          <div className="inlineNotice error">{delivery.error_summary}</div>
        ) : null}
      </div>
      <div className="sheetSection">
        <h3>{translate("webhook.event")}</h3>
        <DefinitionList
          className="sheetFacts"
          items={[
            [translate("webhook.eventID"), <span className="mono">{event.id}</span>],
            [translate("common.type"), <span className="mono">{event.type}</span>],
            [translate("webhook.occurred"), formatTime(event.time)],
          ]}
        />
        <JsonBlock value={event.data} maxHeight={280} />
      </div>
    </Sheet>
  );
}
