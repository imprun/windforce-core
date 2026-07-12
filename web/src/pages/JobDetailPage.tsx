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
import type { JobDetail, JobResult } from "../lib/api";
import { useApp } from "../lib/app-context";
import { formatDuration, formatRelative, formatTime, shortSHA } from "../lib/format";
import { Link } from "../lib/router";

export function JobDetailPage({ jobID }: { jobID: string }) {
  const { api, notify } = useApp();
  const [job, setJob] = useState<JobDetail | null>(null);
  const [result, setResult] = useState<JobResult | null>(null);
  const [logs, setLogs] = useState<string>("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [canceling, setCanceling] = useState(false);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    let canceled = false;
    async function load() {
      try {
        const nextJob = await api.job(jobID);
        if (canceled) return;
        setJob(nextJob);
        setError("");
        const [nextResult, nextLogs] = await Promise.all([
          api.jobResult(jobID).catch(() => null),
          api.jobLogs(jobID).catch(() => ""),
        ]);
        if (canceled) return;
        setResult(nextResult);
        setLogs(nextLogs);
      } catch (cause) {
        if (!canceled) setError(cause instanceof Error ? cause.message : String(cause));
      } finally {
        if (!canceled) setLoading(false);
      }
    }
    void load();
    return () => {
      canceled = true;
    };
  }, [api, jobID, tick]);

  // Poll while the job has not settled.
  useEffect(() => {
    if (!job || job.state === "completed") return;
    const timer = window.setTimeout(() => setTick((current) => current + 1), 2000);
    return () => window.clearTimeout(timer);
  }, [job, tick]);

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
      setTick((current) => current + 1);
    } catch (cause) {
      notify("error", cause instanceof Error ? cause.message : String(cause));
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
          <button className="button" type="button" onClick={() => setTick((current) => current + 1)}>
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
      {error ? <ErrorNotice message={error} onRetry={() => setTick((current) => current + 1)} /> : null}
      {loading && !job ? <Loading /> : null}
      {!loading && !job && !error ? <EmptyState title="Job not found." /> : null}

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
