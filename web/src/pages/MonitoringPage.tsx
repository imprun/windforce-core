import { useState } from "react";
import { Layout } from "../components/Layout";
import { StatTile, WindowSelector, windowLabel } from "../components/stats";
import { EmptyState, ErrorNotice, Loading, Panel } from "../components/ui";
import type { JobStatusCounts } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatRelative } from "../lib/format";
import { Link } from "../lib/router";
import { translate } from "../shared/i18n";

export function MonitoringPage({ legacyJobID }: { legacyJobID?: string } = {}) {
  const { api } = useApp();
  const [windowSeconds, setWindowSeconds] = useState<number>(86400);

  const state = useAsync(async () => {
    const [summary, apps] = await Promise.all([api.jobsSummary(windowSeconds), api.apps()]);
    return { summary, apps: apps.apps || [] };
  }, [api, windowSeconds]);
  const summary = state.data?.summary || null;
  const label = windowLabel(windowSeconds);
  const sourceByApp = new Map(
    (state.data?.apps || []).map((app) => [app.app_key, app.git_source_id]),
  );

  return (
    <Layout
      title={translate("navigation.monitoring")}
      subtitle={translate("monitoring.subtitle")}
      actions={
        <>
          <WindowSelector value={windowSeconds} onChange={setWindowSeconds} />
          <button className="button" type="button" onClick={() => state.reload()}>
            {translate("common.refresh")}
          </button>
        </>
      }
    >
      {legacyJobID ? (
        <div className="inlineNotice">
          {translate("monitoring.legacyRunPrefix")} <span className="mono">{legacyJobID}</span>{" "}
          {translate("monitoring.legacyRunSuffix")}
        </div>
      ) : null}
      {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
      {state.loading && !summary ? <Loading /> : null}

      {summary ? (
        <>
          <div className="statRow" id="jobSummary">
            <StatTile
              label={translate("monitoring.queued")}
              value={summary.queued_count}
              tone="waiting"
            />
            <StatTile
              label={translate("monitoring.running")}
              value={summary.running_count}
              tone="running"
            />
            <StatTile
              label={translate("monitoring.completedWindow", { window: label })}
              value={summary.completed_count_recent}
              tone="good"
            />
            <StatTile
              label={translate("monitoring.failedWindow", { window: label })}
              value={summary.failed_count_recent}
              tone="critical"
            />
            <StatTile
              label={translate("monitoring.canceledWindow", { window: label })}
              value={summary.canceled_count_recent}
              tone="serious"
            />
          </div>

          {summary.oldest_queued_at ? (
            <div className="inlineNotice">
              {translate("monitoring.oldestQueued", {
                time: formatRelative(summary.oldest_queued_at),
              })}
            </div>
          ) : null}

          <Panel
            title={translate("monitoring.byApp")}
            subtitle={translate("monitoring.byAppHint", { window: label })}
          >
            <BreakdownTable
              id="jobsByApp"
              nameHeader={translate("apps.column.app")}
              rows={(summary.by_app || []).map((item) => ({
                key: item.app_key,
                name: item.app_key,
                sourceID: sourceByApp.get(item.app_key),
                counts: item,
              }))}
            />
          </Panel>

          <Panel
            title={translate("monitoring.byRouteTag")}
            subtitle={translate("monitoring.byRouteTagHint", { window: label })}
          >
            <BreakdownTable
              id="jobsByTag"
              nameHeader={translate("apps.column.routeTag")}
              rows={(summary.by_tag || []).map((item) => ({
                key: item.tag,
                name: item.tag,
                counts: item,
              }))}
            />
          </Panel>
        </>
      ) : null}
    </Layout>
  );
}

type BreakdownRow = {
  key: string;
  name: string;
  sourceID?: number;
  counts: JobStatusCounts;
};

function BreakdownTable({
  id,
  nameHeader,
  rows,
}: {
  id: string;
  nameHeader: string;
  rows: BreakdownRow[];
}) {
  if (rows.length === 0) {
    return <EmptyState title={translate("monitoring.empty")} />;
  }
  return (
    <div className="tableWrap">
      <table className="table" id={id}>
        <thead>
          <tr>
            <th>{nameHeader}</th>
            <th className="numCell">{translate("monitoring.queued")}</th>
            <th className="numCell">{translate("monitoring.running")}</th>
            <th className="numCell">{translate("monitoring.completed")}</th>
            <th className="numCell">{translate("monitoring.failed")}</th>
            <th className="numCell">{translate("monitoring.canceled")}</th>
            <th className="numCell">{translate("monitoring.failureRate")}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.key} className="tableRow">
              <td>
                {row.sourceID ? (
                  <Link to={`/apps/${row.sourceID}`} className="cellTitle mono">
                    {row.name}
                  </Link>
                ) : (
                  <span className="cellTitle mono">{row.name}</span>
                )}
              </td>
              <td className="numCell">{row.counts.queued_count}</td>
              <td className="numCell">{row.counts.running_count}</td>
              <td className="numCell">{row.counts.completed_count_recent}</td>
              <td className="numCell">{row.counts.failed_count_recent}</td>
              <td className="numCell">{row.counts.canceled_count_recent}</td>
              <td className="numCell">
                <FailureRate counts={row.counts} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function FailureRate({ counts }: { counts: JobStatusCounts }) {
  const settled = counts.completed_count_recent + counts.failed_count_recent;
  if (settled === 0) return <span>—</span>;
  const rate = (counts.failed_count_recent / settled) * 100;
  const label = `${rate.toFixed(rate > 0 && rate < 1 ? 1 : 0)}%`;
  return <span className={rate > 0 ? "failureRate bad" : "failureRate"}>{label}</span>;
}
