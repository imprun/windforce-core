import { ExternalLink, ServerCog } from "lucide-react";
import { Layout } from "../components/Layout";
import { StatTile } from "../components/stats";
import { EmptyState, ErrorNotice, Loading, Panel } from "../components/ui";
import type {
  ExecutionDemand,
  ExecutionDemandTarget,
  WorkerGroupInventoryItem,
  WorkerGroupStatus,
} from "../lib/api";
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
  const inventoryState = useAsync(() => api.workerGroups(), [api]);
  const demandState = useAsync(() => api.executionDemand(), [api]);
  const groups = inventoryState.data?.groups || [];
  const summary = summarizeWorkerGroups(groups, demandState.data ?? undefined);
  const externalOperator = runtimeConfig?.workerGroupOperator === "external";
  const hasData = Boolean(inventoryState.data || demandState.data);
  const reload = () => {
    inventoryState.reload();
    demandState.reload();
  };

  return (
    <Layout
      title={translate("workerGroups.title")}
      subtitle={translate("workerGroups.subtitle")}
      titleLeading={<ServerCog size={22} aria-hidden="true" />}
      actions={
        <button className="button" type="button" onClick={reload}>
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

      {inventoryState.error ? (
        <ErrorNotice message={inventoryState.error} onRetry={inventoryState.reload} />
      ) : null}
      {demandState.error ? (
        <ErrorNotice message={demandState.error} onRetry={demandState.reload} />
      ) : null}
      {inventoryState.loading && demandState.loading && !hasData ? <Loading /> : null}

      {hasData ? (
        <>
          <div className="statRow" data-ui-guide="worker-group-summary">
            <StatTile
              label={translate("workerGroups.summary.queuedJobs")}
              value={summary.queuedJobs}
              tone={summary.queuedJobs > 0 ? "waiting" : "neutral"}
            />
            <StatTile
              label={translate("workerGroups.summary.oldestWait")}
              value={formatRelative(summary.oldestQueuedAt)}
              tone={summary.queuedJobs > 0 ? "waiting" : "neutral"}
            />
            <StatTile
              label={translate("workerGroups.summary.slotUsage")}
              value={translate("workerGroups.summary.slotUsageValue", {
                occupied: summary.occupiedSlots,
                total: summary.totalSlots,
              })}
              tone={summary.occupiedSlots > 0 ? "running" : "neutral"}
            />
            <StatTile
              label={translate("workerGroups.summary.availableSlots")}
              value={summary.availableSlots}
              tone={
                summary.availableSlots > 0 ? "good" : summary.queuedJobs > 0 ? "serious" : "neutral"
              }
            />
          </div>

          {demandState.data ? (
            <Panel
              title={translate("workerGroups.demand.title")}
              subtitle={translate("workerGroups.demand.hint", {
                time: formatRelative(demandState.data.observed_at),
              })}
            >
              {demandState.data.targets.length === 0 ? (
                <EmptyState title={translate("workerGroups.demand.empty")}>
                  <p>{translate("workerGroups.demand.emptyHint")}</p>
                </EmptyState>
              ) : (
                <div className="tableWrap" data-ui-guide="execution-demand">
                  <table className="table workerGroupTable">
                    <thead>
                      <tr>
                        <th>{translate("workerGroups.demand.target")}</th>
                        <th>{translate("workerGroups.demand.queue")}</th>
                        <th>{translate("workerGroups.demand.selector")}</th>
                        <th>{translate("workerGroups.demand.capacity")}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {demandState.data.targets.map((target) => (
                        <ExecutionDemandRow
                          key={executionDemandTargetKey(target)}
                          target={target}
                        />
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </Panel>
          ) : null}

          {inventoryState.data ? (
            <Panel
              title={translate("workerGroups.inventory")}
              subtitle={translate("workerGroups.inventoryHint", {
                time: formatRelative(inventoryState.data.observed_at),
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
          ) : null}
        </>
      ) : null}
    </Layout>
  );
}

function ExecutionDemandRow({ target }: { target: ExecutionDemandTarget }) {
  const eligibleGroups = target.candidates
    .filter((candidate) => candidate.eligible)
    .map((candidate) => candidate.group);
  return (
    <tr className="tableRow">
      <td>
        <span className="cellTitle">{target.app}</span>
        <span className="cellSub mono">{target.action}</span>
      </td>
      <td>
        <span className="badge badge-warning w-fit">
          {translate("workerGroups.demand.queued", { count: target.queued_jobs })}
        </span>
        <span className="cellSub">
          {translate("workerGroups.demand.oldest", {
            time: formatRelative(target.oldest_queued_at),
          })}
        </span>
      </td>
      <td>
        <SelectorLine label={translate("workerGroups.tags")} values={[target.effective_tag]} />
        <SelectorLine
          label={translate("workerGroups.labels")}
          values={target.effective_required_labels}
        />
      </td>
      <td>
        <DemandCapacity target={target} />
        {eligibleGroups.length > 0 ? (
          <span className="cellSub">
            {translate("workerGroups.demand.compatiblePools", {
              groups: eligibleGroups.join(", "),
            })}
          </span>
        ) : null}
      </td>
    </tr>
  );
}

function DemandCapacity({ target }: { target: ExecutionDemandTarget }) {
  if (target.total_slots === 0) {
    return (
      <span className="badge badge-critical w-fit">
        {translate("workerGroups.demand.noMatchingSlots")}
      </span>
    );
  }
  if (target.saturated) {
    return (
      <span className="badge badge-warning w-fit">
        {translate("workerGroups.demand.saturated", {
          occupied: target.occupied_slots,
          total: target.total_slots,
        })}
      </span>
    );
  }
  return (
    <span className="badge badge-good w-fit">
      {translate("workerGroups.demand.freeSlots", {
        available: target.available_slots,
        total: target.total_slots,
      })}
    </span>
  );
}

function executionDemandTargetKey(target: ExecutionDemandTarget): string {
  return [
    target.app,
    target.action,
    target.effective_tag,
    target.effective_required_labels.join("\u001f"),
    target.execution_profile.key,
  ].join("\u001e");
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
            occupied: group.occupied_slots,
            total: group.total_slots,
          })}
        </span>
        <span className="cellSub">
          {translate("workerGroups.capacityDetail", {
            available: group.available_slots,
            workers: group.live_workers,
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

export function summarizeWorkerGroups(
  groups: WorkerGroupInventoryItem[],
  demand?: ExecutionDemand,
) {
  const usable = groups.filter((group) => group.workspace_allowed);
  return {
    usableGroups: usable.length,
    liveWorkers: usable.reduce((total, group) => total + group.live_workers, 0),
    totalSlots: usable.reduce((total, group) => total + group.total_slots, 0),
    occupiedSlots: usable.reduce((total, group) => total + group.occupied_slots, 0),
    availableSlots: usable.reduce((total, group) => total + group.available_slots, 0),
    queuedJobs: demand?.queued_jobs || 0,
    oldestQueuedAt: demand?.oldest_queued_at,
    attentionGroups: usable.filter(
      (group) =>
        group.status === "offline" || group.status === "draining" || group.version_or_build_drift,
    ).length,
  };
}
