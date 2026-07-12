import { useEffect, useState } from "react";
import { Layout } from "../components/Layout";
import {
  DefinitionList,
  EmptyState,
  ErrorNotice,
  JsonBlock,
  Loading,
  Panel,
  StatusBadge,
} from "../components/ui";
import { errorMessage } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatDuration, formatRelative, formatTime, shortSHA } from "../lib/format";
import { Link } from "../lib/router";

export function JobDetailPage({ jobID }: { jobID: string }) {
  const { api, notify } = useApp();
  const [canceling, setCanceling] = useState(false);

  const state = useAsync(
    async () => {
      const job = await api.job(jobID);
      const [result, logs] = await Promise.all([
        api.jobResult(jobID).catch(() => null),
        api.jobLogs(jobID).catch(() => ""),
      ]);
      return { job, result, logs };
    },
    [api, jobID],
  );
  const job = state.data?.job || null;
  const result = state.data?.result || null;
  const logs = state.data?.logs || "";

  // Poll while the job has not settled.
  const reload = state.reload;
  useEffect(() => {
    if (!job || job.state === "completed") return;
    const timer = window.setTimeout(() => reload(), 2000);
    return () => window.clearTimeout(timer);
  }, [job, reload]);

  async function handleCancel() {
    const reason = window.prompt("Cancel reason (optional):", "") ?? null;
    if (reason === null) return;
    setCanceling(true);
    try {
      const outcome = await api.cancelJob(jobID, reason);
      if (outcome.already_completed) {
        notify("info", "The job had already completed.");
      } else if (outcome.completed_now || outcome.soft_canceled) {
        notify("ok", "Cancel requested.");
      }
      state.reload();
    } catch (cause) {
      notify("error", errorMessage(cause));
    } finally {
      setCanceling(false);
    }
  }

  const statusLabel = job ? job.status || job.state : "";

  return (
    <Layout
      title={job ? `${job.app_key || "job"}/${job.action_key || ""}` : "Job"}
      subtitle={`Job ${jobID}`}
      actions={
        <>
          {job ? <StatusBadge status={statusLabel} /> : null}
          <button className="button" type="button" onClick={() => state.reload()}>
            Refresh
          </button>
          {job && job.state !== "completed" ? (
            <button className="button danger" type="button" disabled={canceling} onClick={handleCancel}>
              Cancel job
            </button>
          ) : null}
          <Link className="button" to="/jobs">
            All jobs
          </Link>
        </>
      }
    >
      {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
      {state.loading && !job ? <Loading /> : null}
      {!state.loading && !job && !state.error ? <EmptyState title="Job not found." /> : null}

      {job ? (
        <>
          <Panel title="Run" subtitle="Identity, timing, and provenance of this run.">
            <DefinitionList
              items={[
                ["Status", <StatusBadge status={statusLabel} />],
                ["App / action", <span className="mono">{job.app_key}/{job.action_key}</span>],
                ["Trigger", job.trigger_kind || "api"],
                ["Route tag", <span className="mono">{job.tag || "—"}</span>],
                ["Release commit", <span className="mono">{shortSHA(job.commit_sha, 16)}</span>],
                ["Entrypoint", <span className="mono">{job.entrypoint || "—"}</span>],
                ["Created", job.created_at ? `${formatTime(job.created_at)} (${formatRelative(job.created_at)})` : "—"],
                ["Started", formatTime(job.started_at)],
                ["Completed", formatTime(job.completed_at)],
                ["Duration", job.duration_ms != null ? formatDuration(job.duration_ms) : "—"],
                ["Worker", job.worker || "—"],
                ["Actor", job.created_by || "system"],
                ...(job.canceled_by
                  ? ([
                      ["Canceled by", job.canceled_by],
                      ["Cancel reason", job.canceled_reason || "—"],
                    ] as Array<[string, string]>)
                  : []),
              ]}
            />
          </Panel>

          <Panel title="Input" subtitle="Action input recorded at enqueue time.">
            <JsonBlock value={job.input} maxHeight={320} />
          </Panel>

          <Panel title="Result" subtitle="Action output, or the failure envelope.">
            {!result || result.status === "pending" ? (
              <EmptyState title={job.state === "completed" ? "No result recorded." : "The run has not settled yet."} />
            ) : (
              <>
                <div className="resultStatus">
                  <StatusBadge status={result.status} />
                </div>
                <JsonBlock value={result.result} maxHeight={400} />
              </>
            )}
          </Panel>

          <Panel title="Logs" subtitle="stdout/stderr tail (last 64 KiB).">
            {logs ? <pre className="codeBlock logBlock">{logs}</pre> : <EmptyState title="No logs recorded." />}
          </Panel>
        </>
      ) : null}
    </Layout>
  );
}
