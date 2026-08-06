---
title: Execution observability and debugging
description: Current Job log, result, service log, artifact, and debugger boundaries for Core workers.
---

This document is the current human-readable contract for observing and
debugging an App executed by Windforce Core. Runtime code and coding agents
must keep the surfaces below separate.

> Trace implementation status (2026-08-06): ADR 0029 is an accepted target contract tracked by GitHub issue #128. The current release does not yet persist W3C trace context or export Core spans through OTLP. The existing log and metric behavior described below remains implemented.

## What a Worker records

The launcher process writes application output to stdout and stderr. The
Worker combines those streams in arrival order, replaces invalid UTF-8,
masks every resolved secret value, and appends the remaining text to the Job
log. The executor's configured log cap bounds what one Job can emit.

The terminal action value is not parsed from stdout. The language wrapper
writes it to `result.json`; the Worker masks it independently and completes the
leased Job. A log line and an action result therefore never share a delivery
contract.

```text
Bun/Python/Go/adapter process
  stdout + stderr
    -> Worker secret masker
      -> append-only Job log chunks
        -> plain-text read or offset SSE

language wrapper
  result.json
    -> Worker result masker
      -> terminal Job result
```

## Distributed tracing

Trace context is optional at every independently invocable boundary. When tracing is enabled, an ingress, Admission path, Worker, launcher, or SDK continues a valid ambient or W3C `traceparent`; if none exists or the carrier is malformed, that boundary starts a new root trace. Missing trace context never makes an otherwise valid request or Job fail.

Admission persists the effective W3C creation context with the Run and Job. PostgreSQL is the durable carrier across queue delay, not a reason to leave the Admission span open. A local or remote Worker restores that context when it claims the Job. A legacy, direct, or test Job without context starts a Worker root. The launcher then passes the current carrier through Core's private transport so the Core Author SDK can expose it read-only and an opaque Application SDK can continue it.

```text
instrumented gateway or adapter (optional)
  -> Core API / Admission, or new Core root
    -> PostgreSQL Run + Job + trace context
      -> Worker process span, or new Worker root
        -> launcher and Core Author SDK
          -> Application SDK / App / Action spans, or new SDK root
```

Normal single-Job processing may remain in one trace. Batch or fan-out work uses span links. Retry, replay, or a future suspend/resume that creates another execution attempt starts a new trace linked to the creation context and previous attempt; the current in-process HumanTask hold stays in its existing attempt and trace. `correlation_id` remains a business correlation value and is not a trace ID.

Core exports backend-neutral OTLP and does not depend on Tempo or another storage backend. Service logs may carry `trace_id` and `span_id` for external log-to-trace navigation. High-cardinality Run and Job identifiers stay out of metric and log-index labels, and inputs, results, credentials, tokens, and secret values never become trace attributes. The full decision and conformance matrix are recorded in [ADR 0029](../adr/0029-optional-trace-context-continuity.md).

## Read and follow logs

Use the plain endpoint to download the retained text:

```http
GET /api/w/{workspace}/jobs/{jobId}/logs?tail_bytes=65536
```

Use the SSE endpoint to follow a running Job:

```http
GET /api/w/{workspace}/jobs/{jobId}/logs/stream?offset=0&timeout_seconds=60
Accept: text/event-stream
```

An update resembles:

```json
{
  "type": "update",
  "running": true,
  "completed": false,
  "new_logs": "processing page 3\n",
  "log_offset": 137,
  "status": "running",
  "attempt": 1,
  "worker_id": "worker-browser-2"
}
```

Persist `log_offset` after consuming `new_logs`. On `timeout`, reconnect with
that offset. `ping` carries no log bytes. A terminal update has
`completed: true`, after which the server closes the response. Offsets count
UTF-8 bytes. HTTP authentication and workspace authorization are identical to
the other Control Plane Job endpoints.

The repository-local development helper performs that reconnect loop:

```bash
python tools/windforce_control.py --workspace default job-logs \
  --job-id <job-id> --follow
```

## The five surfaces

1. **Application Job logs** are masked stdout/stderr for one workspace-scoped
   Job. Use them for progress, diagnostics, and stack traces.
2. **Job results** are the terminal action value or structured failure. Use the
   result endpoint or Invocation completion contract.
3. **Worker and Core service logs** describe process health, claims, leases,
   transport, and infrastructure. Collect them from the host, container, or
   cluster logging stack; they are not copied into every Job.
4. **Job artifacts** are screenshots, Playwright traces, videos, HAR files,
   crash dumps, and other binary evidence. Core does not yet expose an Artifact
   API, so Apps must not base64-encode these objects into stdout or results.
5. **Interactive source debugging** is not available on shared workers. Core
   never exposes Bun/Node Inspector from a normal Job.

## TypeScript and browser debugging workflow

For a Bun/TypeScript App, emit structured, compact progress records to stdout
and errors with their stack to stderr. In the console, open the Job log
inspector with the Job ID and follow it through completion. Record the Job's
release commit and bundle digest. Reproduce the same `main(ctx)` entrypoint
locally against safe inputs when breakpoints or browser devtools are required.

Playwright or Puppeteer is an application dependency inside the immutable
execution bundle. Core does not need to understand that SDK to capture its
process logs. Until the Artifact contract exists, retain browser traces only in
an explicitly configured application-owned external store and log a safe
reference, never credentials or inline binary data.

## Development loop

Core deliberately has three different development and diagnosis loops:

1. **App unit development** runs the TypeScript project directly with Bun and
   an App SDK test context or mocks. Core does not need to run for pure App
   logic tests, local breakpoints, or browser devtools.
2. **Core integration development** runs `windforce-core standalone`, publishes
   an immutable execution bundle, invokes the App through the public API, and
   inspects the resulting Job through the Web Console or Control Plane API.
3. **Worker incident diagnosis** uses the exact release commit, bundle digest,
   masked Job log, terminal result, and Worker service log. The
   `--tee-job-logs` Worker option may mirror captured chunks into the local
   Worker process log during development, but it does not replace Job-log
   persistence or authorization.

`windforce-core` is the runtime executable. `tools/windforce_control.py` is a
repository-local helper for the second loop, not a supported end-user CLI and
not part of the App authoring contract.

Windmill uses the same architectural separation. Its released `wmill` CLI owns
workspace sync, metadata generation, preview, and Job inspection; Bun scripts
can also run directly with a locally supplied context. Production workers still
launch a child process, capture stdout/stderr, persist incremental logs, and
serve updates separately from the terminal result. Core adopts that separation,
not Windmill's workspace file format or CLI as a runtime dependency. See
[Windmill local development](https://www.windmill.dev/docs/advanced/local_development),
[running scripts locally](https://www.windmill.dev/docs/advanced/local_development/run_locally),
and [workers and worker groups](https://www.windmill.dev/docs/core_concepts/worker_groups).

## Maintainer and coding-agent rules

- Never bypass the Worker secret masker when adding a log path.
- Never merge stdout, Job result, Worker service logs, and artifacts into one
  schema merely because all of them help debugging.
- Keep `workspace + job ID` authorization on every read and stream.
- Treat `log_offset` as a byte cursor and make reconnects idempotent.
- Bound producer capture, storage retention, per-event size, and client memory.
- Preserve foreign-key deletion of log state when Job retention prunes a Job.
- Do not add `bun --inspect` to the normal executor. An isolated debug session
  needs a separate ADR, authentication tunnel, TTL, audit, and secret policy.

The worker ordering is defined in [Worker execution lifecycle](worker-execution.md).
The log-streaming rationale and Windmill comparison are recorded in
[ADR 0024](../adr/0024-offset-job-log-streaming.md). Distributed trace
continuity is defined by
[ADR 0029](../adr/0029-optional-trace-context-continuity.md).
