import { RotateCw, Search, Square } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { DefinitionList, ErrorNotice, Field, Sheet } from "../components/ui";
import {
  errorMessage,
  type JobLogStreamEvent,
  type JobResultResponse,
  type JobStatus,
} from "../lib/api";
import { useApp } from "../lib/app-context";
import { formatTime, shortSHA } from "../lib/format";
import { translate } from "../shared/i18n";

const maxClientLogCharacters = 2 * 1024 * 1024;

export function JobLogInspector({
  initialJobID = "",
  onClose,
}: {
  initialJobID?: string;
  onClose: () => void;
}) {
  const { api } = useApp();
  const [draftJobID, setDraftJobID] = useState(initialJobID);
  const [activeJobID, setActiveJobID] = useState("");
  const [job, setJob] = useState<JobStatus | null>(null);
  const [terminalResult, setTerminalResult] = useState<JobResultResponse | null>(null);
  const [logs, setLogs] = useState("");
  const [offset, setOffset] = useState(0);
  const [attempt, setAttempt] = useState<number | null>(null);
  const [workerID, setWorkerID] = useState("");
  const [streamStatus, setStreamStatus] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [truncated, setTruncated] = useState(false);
  const [error, setError] = useState("");
  const abortRef = useRef<AbortController | null>(null);
  const logsRef = useRef("");

  const stop = useCallback(() => {
    abortRef.current?.abort();
    abortRef.current = null;
    setStreaming(false);
  }, []);

  const connect = useCallback(
    async (requestedJobID: string) => {
      const jobID = requestedJobID.trim();
      if (!jobID) {
        setError(translate("monitoring.logInspector.jobRequired"));
        return;
      }
      abortRef.current?.abort();
      const controller = new AbortController();
      abortRef.current = controller;
      setActiveJobID(jobID);
      setDraftJobID(jobID);
      setJob(null);
      setTerminalResult(null);
      setLogs("");
      logsRef.current = "";
      setOffset(0);
      setAttempt(null);
      setWorkerID("");
      setStreamStatus("");
      setTruncated(false);
      setError("");
      setStreaming(true);

      try {
        const initial = await api.job(jobID);
        if (controller.signal.aborted) return;
        setJob(initial);
        setStreamStatus(initial.status || initial.state);
        setWorkerID(initial.worker || "");

        let cursor = 0;
        let completed = false;
        while (!controller.signal.aborted && !completed) {
          const result = await api.streamJobLogs(jobID, {
            offset: cursor,
            timeoutSeconds: 60,
            signal: controller.signal,
            onEvent: (event) => {
              if (controller.signal.aborted) return;
              applyEvent(event, {
                appendLogs: (chunk) => {
                  let next = logsRef.current + chunk;
                  if (next.length > maxClientLogCharacters) {
                    next = next.slice(-maxClientLogCharacters);
                    setTruncated(true);
                  }
                  logsRef.current = next;
                  setLogs(next);
                },
                setOffset,
                setAttempt,
                setWorkerID,
                setStreamStatus,
              });
            },
          });
          cursor = result.offset;
          completed = result.completed;
        }
        if (!controller.signal.aborted && completed) {
          const result = await api.jobResult(jobID);
          if (!controller.signal.aborted) setTerminalResult(result);
        }
      } catch (cause: unknown) {
        if (!controller.signal.aborted) setError(errorMessage(cause));
      } finally {
        if (abortRef.current === controller) {
          abortRef.current = null;
          setStreaming(false);
        }
      }
    },
    [api],
  );

  useEffect(() => {
    if (initialJobID) void connect(initialJobID);
    return () => abortRef.current?.abort();
  }, [connect, initialJobID]);

  const close = () => {
    stop();
    onClose();
  };
  const status = streamStatus || job?.status || job?.state || "";
  const humanTaskExpired = isHumanTaskDeadlineResult(terminalResult);
  const executionLimitPins = [
    ...(job?.execution_limits?.concurrency || []).map((limit) => ({
      kind: "concurrency" as const,
      limit,
    })),
    ...(job?.execution_limits?.rate || []).map((limit) => ({
      kind: "rate" as const,
      limit,
    })),
  ];

  return (
    <Sheet
      id="jobLogInspector"
      title={translate("monitoring.logInspector.title")}
      subtitle={activeJobID || translate("monitoring.logInspector.subtitle")}
      onClose={close}
      actions={
        <>
          <span className="fieldHint mono">
            {activeJobID
              ? translate("monitoring.logInspector.offset", { offset })
              : translate("monitoring.logInspector.maskedHint")}
          </span>
          <div className="jobLogFooterActions">
            {streaming ? (
              <button className="button" type="button" onClick={stop}>
                <Square size={14} aria-hidden="true" />
                {translate("monitoring.logInspector.stop")}
              </button>
            ) : null}
            <button className="button" type="button" onClick={close}>
              {translate("common.close")}
            </button>
          </div>
        </>
      }
    >
      <div className="jobLogInspector">
        <form
          className="jobLogToolbar"
          onSubmit={(event) => {
            event.preventDefault();
            void connect(draftJobID);
          }}
        >
          <Field label={translate("monitoring.logInspector.jobID")}>
            <input
              className="mono"
              value={draftJobID}
              autoComplete="off"
              spellCheck={false}
              placeholder={translate("monitoring.logInspector.jobPlaceholder")}
              onChange={(event) => setDraftJobID(event.target.value)}
            />
          </Field>
          <button className="button primary" id="connectJobLogs" type="submit" disabled={streaming}>
            {activeJobID && !streaming ? (
              <RotateCw size={16} aria-hidden="true" />
            ) : (
              <Search size={16} aria-hidden="true" />
            )}
            {activeJobID && !streaming
              ? translate("monitoring.logInspector.reconnect")
              : translate("monitoring.logInspector.connect")}
          </button>
        </form>

        {error ? <ErrorNotice message={error} onRetry={() => void connect(activeJobID)} /> : null}

        {job ? (
          <DefinitionList
            className="jobLogFacts"
            items={[
              [
                translate("monitoring.logInspector.status"),
                <JobLogStatus key="status" status={status} />,
              ],
              [
                translate("monitoring.logInspector.appAction"),
                `${job.app_key || "—"} / ${job.action_key || "—"}`,
              ],
              [translate("monitoring.logInspector.worker"), workerID || job.worker || "—"],
              [translate("monitoring.logInspector.attempt"), attempt ?? "—"],
              [
                translate("monitoring.logInspector.commit"),
                <span className="mono" key="commit">
                  {shortSHA(job.commit_sha)}
                </span>,
              ],
              [translate("monitoring.logInspector.started"), formatTime(job.started_at)],
            ]}
          />
        ) : null}

        {humanTaskExpired ? (
          <div className="inlineNotice error" role="status">
            {translate("monitoring.logInspector.humanTaskExpired")}
          </div>
        ) : null}

        {executionLimitPins.length ? (
          <section className="jobExecutionLimits" aria-labelledby="jobExecutionLimitsTitle">
            <div className="jobLogSectionHeading">
              <div>
                <h3 id="jobExecutionLimitsTitle">
                  {translate("monitoring.logInspector.executionLimits")}
                </h3>
                <p>{translate("monitoring.logInspector.executionLimitsHint")}</p>
              </div>
              <span className="badge badge-neutral">
                {translate("executionLimits.policyCount", {
                  count: executionLimitPins.length,
                })}
              </span>
            </div>
            <div className="jobExecutionLimitList">
              {executionLimitPins.map(({ kind, limit }) => (
                <article
                  className="jobExecutionLimit"
                  key={`${kind}:${limit.scope}:${limit.policy_id}:${limit.key_digest}`}
                >
                  <div className="jobExecutionLimitIdentity">
                    <strong className="mono">{limit.policy_id}</strong>
                    <span className="badge badge-neutral">
                      {translate(`executionLimits.type.${kind}`)}
                    </span>
                    <span className="badge badge-neutral">
                      {executionLimitScopeLabel(limit.scope)}
                    </span>
                  </div>
                  <dl>
                    <div>
                      <dt>{translate("executionLimits.capacity")}</dt>
                      <dd className="mono">
                        {kind === "concurrency"
                          ? limit.max_concurrent
                          : translate("executionLimits.rateBudgetValue", {
                              attempts: limit.max_attempts,
                              seconds: limit.window_seconds,
                            })}
                      </dd>
                    </div>
                    <div>
                      <dt>{translate("executionLimits.revision")}</dt>
                      <dd className="mono" title={limit.policy_revision}>
                        {shortOpaqueDigest(limit.policy_revision)}
                      </dd>
                    </div>
                    <div>
                      <dt>{translate("executionLimits.keyDigest")}</dt>
                      <dd className="mono" title={limit.key_digest}>
                        {shortOpaqueDigest(limit.key_digest)}
                      </dd>
                    </div>
                  </dl>
                </article>
              ))}
            </div>
          </section>
        ) : null}

        <section className="jobLogSection" aria-labelledby="jobLogOutputTitle">
          <div className="jobLogSectionHeading">
            <div>
              <h3 id="jobLogOutputTitle">{translate("monitoring.logInspector.output")}</h3>
              <p>{translate("monitoring.logInspector.outputHint")}</p>
            </div>
            {streaming ? (
              <span className="jobLogLive">
                <span aria-hidden="true" />
                {translate("monitoring.logInspector.live")}
              </span>
            ) : null}
          </div>
          {truncated ? (
            <div className="inlineNotice">{translate("monitoring.logInspector.truncated")}</div>
          ) : null}
          <pre className="jobLogOutput" role="log" aria-live="polite" aria-atomic="false">
            {logs || translate("monitoring.logInspector.empty")}
          </pre>
        </section>
      </div>
    </Sheet>
  );
}

function executionLimitScopeLabel(scope: string): string {
  return scope === "action"
    ? translate("executionLimits.scope.action")
    : translate("executionLimits.scope.app");
}

export function shortOpaqueDigest(value: string, length = 12): string {
  const separator = value.indexOf(":");
  if (separator < 0) return shortSHA(value, length);
  return `${value.slice(0, separator + 1)}${value.slice(separator + 1, separator + 1 + length)}`;
}

export function isHumanTaskDeadlineResult(result: JobResultResponse | null): boolean {
  if (result?.status !== "failure" || typeof result.result !== "object") return false;
  if (result.result === null || Array.isArray(result.result)) return false;
  return (result.result as Record<string, unknown>).code === "human_task_deadline";
}

function JobLogStatus({ status }: { status: string }) {
  const normalized = status.toLowerCase();
  const tone =
    normalized === "running"
      ? "badge-running"
      : normalized === "success" || normalized === "succeeded"
        ? "badge-good"
        : normalized === "failure" || normalized === "failed" || normalized === "canceled"
          ? "badge-critical"
          : "badge-neutral";
  return (
    <span className={`badge ${tone}`}>
      {status || translate("monitoring.logInspector.unknown")}
    </span>
  );
}

function applyEvent(
  event: JobLogStreamEvent,
  setters: {
    appendLogs: (chunk: string) => void;
    setOffset: (offset: number) => void;
    setAttempt: (attempt: number | null) => void;
    setWorkerID: (workerID: string) => void;
    setStreamStatus: (status: string) => void;
  },
) {
  if (event.type !== "update") return;
  if (event.new_logs) setters.appendLogs(event.new_logs);
  if (event.log_offset !== undefined) setters.setOffset(event.log_offset);
  if (event.attempt !== undefined) setters.setAttempt(event.attempt);
  if (event.worker_id !== undefined) setters.setWorkerID(event.worker_id);
  if (event.status) setters.setStreamStatus(event.status);
}
