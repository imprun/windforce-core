import { ErrorNotice, Loading, Panel } from "../components/ui";
import { useApp, useAsync } from "../lib/app-context";
import { translate } from "../shared/i18n";
import { AuditEventTable } from "./AuditEventTable";

export function WebhookAudit({ subscriptionID }: { subscriptionID: string }) {
  const { api } = useApp();
  const state = useAsync(
    () => api.auditEvents({ category: "webhook", limit: 250 }),
    [api, subscriptionID],
  );
  const events =
    state.data?.filter((event) => event.webhook_subscription_id === subscriptionID) || [];

  return (
    <Panel
      title={translate("webhook.audit")}
      subtitle={translate("webhook.auditHint")}
      actions={
        <button className="button" type="button" onClick={state.reload}>
          {translate("common.refresh")}
        </button>
      }
    >
      {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
      {state.loading && !state.data ? <Loading label={translate("audit.loading")} /> : null}
      {state.data ? (
        <AuditEventTable events={events} emptyTitle={translate("webhook.noAuditEvents")} />
      ) : null}
    </Panel>
  );
}
