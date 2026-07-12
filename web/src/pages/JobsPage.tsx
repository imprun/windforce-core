import { useEffect, useState } from "react";
import { Layout } from "../components/Layout";
import { EmptyState, ErrorNotice, Loading, StatusBadge } from "../components/ui";
import type { JobListItem } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatDuration, formatRelative, formatTime } from "../lib/format";
import { useRouter } from "../lib/router";

const statusFilters = ["all", "queued", "running", "success", "failure", "canceled"] as const;

export function JobsPage() {
  const { api } = useApp();
  const { navigate } = useRouter();
  const [status, setStatus] = useState<(typeof statusFilters)[number]>("all");
  const [appFilter, setAppFilter] = useState("");
  const [pages, setPages] = useState<JobListItem[]>([]);
  const [cursor, setCursor] = useState<string | undefined>();

  const summary = useAsync(() => api.jobsSummary(), [api]);
  const list = useAsync(
    () => api.jobs({ status, app: appFilter.trim() || undefined, limit: 50 }),
    [api, status, appFilter],
  );

  // Reset accumulated pages whenever the first page reloads.
  useEffect(() => {
    setPages([]);
    setCursor(undefined);
  }, [list.data]);

  const items = [...(list.data?.items || []), ...pages];
  const hasMore = cursor !== undefined ? Boolean(cursor) : Boolean(list.data?.pagination.has_more);
  const nextCursor = cursor ?? list.data?.pagination.next_cursor;

  async function loadMore() {
    if (!nextCursor) return;
    const response = await api.jobs({ status, app: appFilter.trim() || undefined, limit: 50, cursor: nextCursor });
    setPages((current) => [...current, ...response.items]);
    setCursor(response.pagination.has_more ? response.pagination.next_cursor : "");
  }

  return (
    <Layout
      title="Jobs"
      subtitle="Run status across the workspace. Open a job to inspect its input, result, and logs."
      actions={
        <button
          className="button"
          type="button"
          onClick={() => {
            summary.reload();
            list.reload();
          }}
        >
          Refresh
        </button>
      }
    >
      <div className="statRow" id="jobSummary">
        <StatTile label="Queued" value={summary.data?.queued_count} tone="waiting" />
        <StatTile label="Running" value={summary.data?.running_count} tone="running" />
        <StatTile label="Completed · 24h" value={summary.data?.completed_count_recent} tone="good" />
        <StatTile label="Failed · 24h" value={summary.data?.failed_count_recent} tone="critical" />
        <StatTile label="Canceled · 24h" value={summary.data?.canceled_count_recent} tone="serious" />
      </div>

      <div className="filterRow">
        <div className="segmented" role="group" aria-label="Status filter">
          {statusFilters.map((item) => (
            <button
              key={item}
              type="button"
              className={item === status ? "segment active" : "segment"}
              onClick={() => setStatus(item)}
            >
              {item}
            </button>
          ))}
        </div>
        <input
          className="searchInput"
          placeholder="Filter by app key…"
          value={appFilter}
          onChange={(event) => setAppFilter(event.target.value)}
          aria-label="Filter by app key"
        />
      </div>

      {list.error ? <ErrorNotice message={list.error} onRetry={list.reload} /> : null}
      {list.loading && !list.data ? <Loading /> : null}
      {list.data && items.length === 0 ? <EmptyState title="No jobs match the current filter." /> : null}

      {items.length > 0 ? (
        <div className="tableWrap">
          <table className="table" id="jobList">
            <thead>
              <tr>
                <th>Job</th>
                <th>Status</th>
                <th>Trigger</th>
                <th>Created</th>
                <th>Duration</th>
                <th>Actor</th>
              </tr>
            </thead>
            <tbody>
              {items.map((job) => (
                <tr key={job.id} className="tableRow clickable" onClick={() => navigate(`/jobs/${job.id}`)}>
                  <td>
                    <span className="cellTitle mono">
                      {job.app_key}/{job.action_key}
                    </span>
                    <span className="cellSub mono">{job.id}</span>
                  </td>
                  <td>
                    <StatusBadge status={job.status} />
                    {job.error_snippet ? <span className="cellSub errorSnippet">{job.error_snippet}</span> : null}
                  </td>
                  <td>{job.trigger_kind}</td>
                  <td>
                    <span className="cellTitle">{formatRelative(job.created_at)}</span>
                    <span className="cellSub">{formatTime(job.created_at)}</span>
                  </td>
                  <td>{job.completed ? formatDuration(job.duration_ms) : "—"}</td>
                  <td>{job.created_by || "system"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}

      {hasMore && nextCursor ? (
        <div className="loadMoreRow">
          <button className="button" type="button" onClick={() => void loadMore()}>
            Load more
          </button>
        </div>
      ) : null}
    </Layout>
  );
}

function StatTile({
  label,
  value,
  tone,
}: {
  label: string;
  value: number | undefined;
  tone: "waiting" | "running" | "good" | "critical" | "serious";
}) {
  return (
    <div className="statTile">
      <span className={`statDot dot-${tone}`} aria-hidden="true" />
      <div>
        <p className="statValue">{value ?? "—"}</p>
        <p className="statLabel">{label}</p>
      </div>
    </div>
  );
}
