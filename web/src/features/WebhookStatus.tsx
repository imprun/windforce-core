import type { WebhookDeliveryState } from "../lib/api";
import { translate } from "../shared/i18n";

function deliveryLabel(state: WebhookDeliveryState): string {
  return translate(`webhook.state.${state}` as Parameters<typeof translate>[0]);
}

export function WebhookSubscriptionStatus({
  enabled,
  deleted,
}: {
  enabled: boolean;
  deleted?: boolean;
}) {
  if (deleted) {
    return <span className="badge badge-neutral">{translate("common.deleted")}</span>;
  }
  if (!enabled) {
    return <span className="badge badge-warning">{translate("common.disabled")}</span>;
  }
  return <span className="badge badge-good">{translate("common.enabled")}</span>;
}

export function WebhookDeliveryStatus({ state }: { state: WebhookDeliveryState }) {
  const tone =
    state === "succeeded"
      ? "good"
      : state === "failed"
        ? "critical"
        : state === "canceled"
          ? "neutral"
          : state === "retrying"
            ? "warning"
            : "running";
  return <span className={`badge badge-${tone}`}>{deliveryLabel(state)}</span>;
}

export function webhookEventLabel(type: string): string {
  if (type === "windforce.release.published") return translate("webhook.event.releasePublished");
  if (type === "windforce.release.rolled_back") {
    return translate("webhook.event.releaseRolledBack");
  }
  if (type === "windforce.webhook.test") return translate("webhook.event.testDelivery");
  return type;
}
