import { ExternalLink, ServerCog } from "lucide-react";
import { Layout } from "../components/Layout";
import { StatTile } from "../components/stats";
import { EmptyState, ErrorNotice, Loading, Panel } from "../components/ui";
import type { WorkerGroupInventoryItem, WorkerGroupStatus } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatRelative } from "../lib/format";
import { type TranslationKey, translate } from "../shared/i18n";

const workerGroupStatusKeys: Record<WorkerGroupStatus, TranslationKey> = {
  ready: "workerGroups.status.ready",
  degraded: "workerGroups.status.degraded",
  offline: "workerGroups.status.offline",
  draining: "workerGroups.status.draining",
};

export function WorkerGroupsPage() {
  const { api, runtimeConfig } = useApp();
  const state = useAsync(() => api.workerGroups(), [api]);
  const groups = state.data?.groups || [];
  const summary = summarizeWorkerGroups(groups);
  const externalOperator = runtimeConfig?.workerGroupOperator === "external";

  return (
    <Layout
      title={translate("workerGroups.title")}
      subtitle={translate("workerGroups.subtitle")}
      titleLeading={<ServerCog size={22} aria-hidden="true" />}
      actions={
        <button className="button" type="button" onClick={() => state.reload()}>
          {translate("common.refresh")}
        </button>
      }
    >
      {externalOperator ? (
        <div className="inlineNotice">
          <span>{translate("workerGroups.externalOperatorHint")}</span>
          {runtimeConfig?.hostConsole ? (
            <a
              className="button secondary small ml-auto shrink-0 no-underline"
              href={runtimeConfig.hostConsole.url}
              target="_top"
            >
              {runtimeConfig.hostConsole.label}
              <ExternalLink size={14} aria-hidden="true" />
            </a>
          ) : null}
        </div>
      ) : (
        <div className="inlineNotice">{translate("workerGroups.coreOperatorHint")}</div>
      )}

      {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
      {state.loading && !state.data ? <Loading /> : null}

      {state.data ? (
        <>
          <div className="statRow" data-ui-guide="worker-group-summary">
            <StatTile
              label={translate("workerGroups.summary.usablePools")}
              value={summary.usableGroups}
              tone="good"
            />
            <StatTile
              label={translate("workerGroups.summary.liveWorkers")}
              value={summary.liveWorkers}
              tone="running"
            />
            <StatTile
              label={translate("workerGroups.summary.availableSlots")}
              value={summary.availableSlots}
              tone="good"
            />
            <StatTile
              label={translate("workerGroups.summary.attention")}
              value={summary.attentionGroups}
              tone={summary.attentionGroups > 0 ? "serious" : "neutral"}
            />
          </div>

          <Panel
            title={translate("workerGroups.inventory")}
            subtitle={translate("workerGroups.inventoryHint", {
              time: formatRelative(state.data.observed_at),
            })}
          >
            {groups.length === 0 ? (
              <EmptyState title={translate("workerGroups.empty")}>
                <p>{translate("workerGroups.emptyHint")}</p>
              </EmptyState>
            ) : (
              <div className="tableWrap" data-ui-guide="worker-group-inventory">
                <table className="table workerGroupTable">
                  <thead>
                    <tr>
                      <th>{translate("workerGroups.column.pool")}</th>
                      <th>{translate("common.status")}</th>
                      <th>{translate("workerGroups.column.capacity")}</th>
                      <th>{translate("workerGroups.column.work")}</th>
                      <th>{translate("workerGroups.column.selectors")}</th>
                      <th>{translate("workerGroups.column.build")}</th>
                      <th>{translate("workerGroups.column.heartbeat")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {groups.map((group) => (
                      <WorkerGroupRow group={group} key={group.group} />
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Panel>
        </>
      ) : null}
    </Layout>
  );
}

function WorkerGroupRow({ group }: { group: WorkerGroupInventoryItem }) {
  return (
    <tr className="tableRow">
      <td>
        <span className="cellTitle mono">{group.group}</span>
        <span className="cellSub">
          {translate(group.managed ? "workerGroups.managed" : "workerGroups.static")}
        </span>
        {!group.workspace_allowed ? (
          <span className="badge badge-critical mt-1 w-fit">
            {translate("workerGroups.notWorkspaceAllowed")}
          </span>
        ) : null}
      </td>
      <td>
        <WorkerGroupStatusBadge status={group.status} />
        {group.version_or_build_drift ? (
          <span className="badge badge-warning mt-1 w-fit">
            {translate("workerGroups.buildDrift")}
          </span>
        ) : null}
      </td>
      <td>
        <span className="cellTitle">
          {translate("workerGroups.capacityValue", {
            workers: group.live_workers,
            slots: group.available_slots,
          })}
        </span>
        {group.unmanaged_live_workers > 0 ? (
          <span className="cellSub">
            {translate("workerGroups.staticWorkers", { count: group.unmanaged_live_workers })}
          </span>
        ) : null}
      </td>
      <td>
        <span className="cellTitle">
          {translate("workerGroups.workValue", {
            jobs: group.running_jobs,
            leases: group.active_leases,
          })}
        </span>
        {group.run_state === "draining" ? (
          <span className="cellSub">
            {group.quiescent
              ? translate("workerGroups.quiescent")
              : translate("workerGroups.drainInProgress")}
          </span>
        ) : null}
      </td>
      <td>
        <SelectorLine label={translate("workerGroups.tags")} values={group.tags} />
        <SelectorLine label={translate("workerGroups.labels")} values={group.labels} />
        <SelectorLine
          label={translate("workerGroups.profiles")}
          values={group.execution_profiles.map((profile) => profile.id || profile.key)}
        />
      </td>
      <td>
        <SelectorLine
          label={translate("workerGroups.engineVersions")}
          values={group.engine_versions}
        />
        <SelectorLine label={translate("workerGroups.revisions")} values={group.build_revisions} />
      </td>
      <td>
        <span title={group.last_heartbeat_at || undefined}>
          {formatRelative(group.last_heartbeat_at)}
        </span>
      </td>
    </tr>
  );
}

function WorkerGroupStatusBadge({ status }: { status: WorkerGroupStatus }) {
  const tone =
    status === "ready" ? "badge-good" : status === "offline" ? "badge-neutral" : "badge-warning";
  return <span className={`badge ${tone}`}>{translate(workerGroupStatusKeys[status])}</span>;
}

function SelectorLine({ label, values }: { label: string; values: string[] }) {
  const value = values.length > 0 ? values.join(", ") : translate("common.notConfigured");
  return (
    <span className="cellSub block max-w-64 truncate" title={value}>
      {label}: {value}
    </span>
  );
}

export function summarizeWorkerGroups(groups: WorkerGroupInventoryItem[]) {
  const usable = groups.filter((group) => group.workspace_allowed);
  return {
    usableGroups: usable.length,
    liveWorkers: usable.reduce((total, group) => total + group.live_workers, 0),
    availableSlots: usable.reduce((total, group) => total + group.available_slots, 0),
    attentionGroups: usable.filter(
      (group) =>
        group.status === "offline" || group.status === "draining" || group.version_or_build_drift,
    ).length,
  };
}
