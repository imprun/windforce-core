import { EmptyState } from "../components/ui";
import type { AuditChanges, AuditEvent } from "../lib/api";
import { formatRelative, formatTime } from "../lib/format";
import { Link } from "../lib/router";
import { translate } from "../shared/i18n";

const changeKeys: Array<keyof AuditChanges> = ["added", "updated", "removed", "locked", "unlocked"];

export function auditChangeGroups(
  changes?: AuditChanges,
): Array<{ label: string; keys: string[] }> {
  if (!changes) return [];
  return changeKeys.flatMap((key) => {
    const keys = changes[key] || [];
    return keys.length ? [{ label: auditChangeLabel(key), keys }] : [];
  });
}

function AuditScope({ event }: { event: AuditEvent }) {
  return (
    <div className="auditScope">
      {event.app_key ? (
        event.git_source_id ? (
          <Link className="cellTitle mono" to={`/apps/${event.git_source_id}/audit`}>
            {event.app_key}
          </Link>
        ) : (
          <span className="cellTitle mono">{event.app_key}</span>
        )
      ) : null}
      {event.client_id ? (
        <Link
          className={event.app_key ? "cellSub" : "cellTitle"}
          to={`/clients/${event.client_id}`}
        >
          {event.client_name || translate("audit.registeredClient")}
        </Link>
      ) : null}
      {event.action_key ? (
        <span className="cellSub mono">
          {translate("audit.actionNamed", { action: event.action_key })}
        </span>
      ) : null}
      {event.webhook_subscription_id ? (
        <Link
          className={event.app_key || event.client_id ? "cellSub" : "cellTitle"}
          to={`/settings/webhooks/${event.webhook_subscription_id}/audit`}
        >
          {translate("audit.webhookNamed", {
            id: `${event.webhook_subscription_id.slice(0, 12)}…`,
          })}
        </Link>
      ) : null}
      {!event.app_key &&
      !event.client_id &&
      !event.action_key &&
      !event.webhook_subscription_id &&
      event.git_source_id ? (
        <span className="cellTitle">
          {translate("audit.repositorySourceNamed", { id: event.git_source_id })}
        </span>
      ) : null}
      {!event.app_key &&
      !event.client_id &&
      !event.action_key &&
      !event.git_source_id &&
      !event.webhook_subscription_id ? (
        <span className="cellSub">{translate("settingsNav.workspace")}</span>
      ) : null}
    </div>
  );
}

function AuditDetail({ event }: { event: AuditEvent }) {
  const groups = auditChangeGroups(event.changes);
  if (groups.length) {
    return (
      <div className="auditChanges">
        {groups.map((group) => (
          <span key={group.label}>
            <strong>{group.label}</strong> <code>{group.keys.join(", ")}</code>
          </span>
        ))}
      </div>
    );
  }
  return (
    <span className={event.detail ? "auditDetail" : "cellSub"}>
      {event.detail || translate("trigger.audit.noDetail")}
    </span>
  );
}

export function AuditEventTable({
  events,
  emptyTitle,
}: {
  events: AuditEvent[];
  emptyTitle?: string;
}) {
  if (events.length === 0) {
    return <EmptyState title={emptyTitle || translate("audit.empty")} />;
  }

  return (
    <div className="tableWrap auditTableWrap">
      <table className="table auditTable" id="auditEvents">
        <thead>
          <tr>
            <th>{translate("trigger.delivery.when")}</th>
            <th>{translate("settings.actor")}</th>
            <th>{translate("audit.category")}</th>
            <th>{translate("audit.change")}</th>
            <th>{translate("audit.scope")}</th>
            <th>{translate("audit.detail")}</th>
          </tr>
        </thead>
        <tbody>
          {events.map((event) => (
            <tr key={`${event.category}-${event.id}`}>
              <td title={formatTime(event.created_at)}>
                <span className="cellTitle">{formatRelative(event.created_at)}</span>
                <span className="cellSub">{formatTime(event.created_at)}</span>
              </td>
              <td>{event.actor || "system"}</td>
              <td>
                <span className="badge auditCategory">{auditCategoryLabel(event.category)}</span>
              </td>
              <td>
                <span className="cellTitle">{event.summary}</span>
                <span className="cellSub mono">{event.kind}</span>
              </td>
              <td>
                <AuditScope event={event} />
              </td>
              <td>
                <AuditDetail event={event} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function auditCategoryLabel(category: string): string {
  if (category === "repository") return translate("audit.repository");
  if (category === "release") return translate("audit.release");
  if (category === "client") return translate("navigation.clientRegistry");
  if (category === "input_settings") return translate("audit.inputSettings");
  if (category === "webhook") return translate("settingsNav.webhooks");
  if (category === "workspace") return translate("settingsNav.workspace");
  return category;
}

function auditChangeLabel(change: keyof AuditChanges): string {
  if (change === "added") return translate("audit.change.added");
  if (change === "updated") return translate("audit.change.updated");
  if (change === "removed") return translate("audit.change.removed");
  if (change === "locked") return translate("audit.change.locked");
  return translate("audit.change.unlocked");
}
