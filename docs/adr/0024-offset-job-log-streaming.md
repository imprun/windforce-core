# ADR 0024: Offset-based Job log streaming and bounded debugging

## Status

Accepted (2026-08-03). Amends the per-Job inspection boundary in
[ADR 0005](0005-aggregate-job-observability.md) without restoring a browsable
Job ledger.

## Context

Windforce Core already captures the launched process's combined stdout and
stderr, masks resolved secrets in the Worker, appends the masked bytes to the
state store, and exposes a bounded log document through the Control Plane API.
That is sufficient after a Job completes, but it is not sufficient for a long
running Bun/TypeScript action: an operator cannot follow progress without
repeatedly downloading the growing document. PostgreSQL also rewrites that
whole document on every append.

Windmill separates several concerns that are easy to conflate:

- its released `wmill` CLI handles App authoring, workspace synchronization,
  preview, and Job inspection rather than becoming part of the Worker runtime;
- Job log updates are delivered as offset-based SSE events and can reconnect
  from a known offset.
- streamed action results are a different channel from process logs;
- large logs can leave the hot database path for object or filesystem storage;
- worker service logs are observed independently from application Job logs;
- source debugging is performed in an explicitly controlled development
  context, not by exposing a debugger port on an arbitrary shared worker.

References inspected for this decision are the current
[Jobs](https://www.windmill.dev/docs/core_concepts/jobs),
[Streaming](https://www.windmill.dev/docs/core_concepts/streaming),
[service logs](https://www.windmill.dev/docs/core_concepts/service_logs), and
[local development](https://www.windmill.dev/docs/advanced/local_development/run_locally)
documentation, plus Windmill's
[SSE JobUpdate implementation](https://github.com/windmill-labs/windmill/blob/2105540cca7cc7bdced9e06ef3ad1ed54732feef/backend/windmill-api/src/jobs.rs#L9475-L9546),
[child process output handling](https://github.com/windmill-labs/windmill/blob/2105540cca7cc7bdced9e06ef3ad1ed54732feef/backend/windmill-worker/src/handle_child.rs#L94-L117),
and
[worker Job logger](https://github.com/windmill-labs/windmill/blob/2105540cca7cc7bdced9e06ef3ad1ed54732feef/backend/windmill-worker/src/job_logger.rs#L32-L63)
at upstream commit `2105540` (2026-08-02).

## Decision

### Application Job log contract

The Worker remains the only writer of application Job logs. It combines the
launched process's stdout and stderr in arrival order, masks resolved secret
values before transport, and appends valid UTF-8 chunks. Logs remain scoped by
both workspace and Job ID.

PostgreSQL stores new bytes as append-only `job_log_chunks` with absolute byte
offsets and a small `job_log_state` cursor. Existing `job_logs.logs` rows remain
readable during migration, but upgraded writers no longer rewrite that growing
text value. Job retention deletes the chunk and cursor rows through the Job
foreign key.

The Control Plane adds:

```text
GET /api/w/{workspace}/jobs/{jobId}/logs/stream
  ?offset=<non-negative byte offset>
  &timeout_seconds=<1..300>
```

The response is `text/event-stream`. JSON events have `type` equal to
`update`, `ping`, `timeout`, `error`, or `notfound`. An `update` carries new
masked bytes, the next byte offset, current Job status, attempt, worker ID, and
running/completed flags. A client reconnects with the last received offset;
updates are limited to 256 KiB so a slow reader does not require an unbounded
response allocation. Terminal Jobs send their final update and close.

The existing plain-text logs endpoint remains the simple completed-Job download
surface for API clients and the repository-local development helper. Log offsets
are byte offsets, not Unicode character counts.

### Console boundary

The Monitoring page remains aggregate-first. It may open a focused Job log
inspector only when the operator supplies or arrives with a Job ID. It does not
list or page through Jobs and does not expose Job input. The inspector shows
status metadata, follows masked logs, supports reconnection by offset, and
links terminal result retrieval to the existing API contract.

### Distinct observability surfaces

The following remain separate contracts:

| Surface | Owner | This decision |
| --- | --- | --- |
| Application stdout/stderr | Worker and Job log store | Implement offset streaming |
| Terminal action value | Job result contract | Keep separate from logs |
| Worker/Core process logs | Process/container logging stack | Do not copy into Job logs |
| Screenshots, Playwright traces, videos, dumps | Future Job Artifact contract | Do not encode as log text |
| Interactive Bun Inspector | Future isolated development session | Do not expose on shared workers |

### Debugging policy

The normal debugging path is masked live logs, stack traces, terminal result,
the exact release commit and bundle digest, then local reproduction using the
same App runtime interface. Core must not start Bun with `--inspect`, allocate a
public inspector port, or hold a normal queue lease waiting for an interactive
debugger.

If remote interactive debugging is added, it must be a separate, explicitly
requested development session with an isolated worker pool, single-session
lease, short TTL, authenticated tunnel, audit record, disabled production
secrets by default, and no public Node inspector socket. That future capability
requires its own ADR and threat model.

Browser automation artifacts are likewise a follow-up contract. They need
content-addressed upload, workspace/Job authorization, media metadata, quotas,
retention, and secret review. The current log API must not become an implicit
binary transport.

## Consequences

- Bun, Python, Go, and adapter-launched Apps use one SDK-neutral log contract.
- Following a long Job transfers only new bytes and survives HTTP reconnects.
- PostgreSQL write amplification no longer grows with the full accumulated log.
- The console gains targeted incident inspection without reintroducing a
  million-row Job browser.
- Inspector sessions and browser artifacts remain deliberately unavailable
  until their isolation, security, storage, and retention contracts exist.
