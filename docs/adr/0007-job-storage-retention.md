# ADR 0007: Job storage retention

## Status

Accepted. Extends [ADR 0005](0005-aggregate-job-observability.md): that ADR
removed per-job browsing from the Web UI; this one governs how long the
control plane keeps the per-job records themselves.

## Context

At production volume — millions of jobs — raw job records are short-lived
incident data, not history. Nobody re-reads a succeeded run's payload weeks
later, and unbounded retention makes every jobs-table scan and backup pay
for records that answer no operational question. Operational trends belong
to aggregates; the audit trail and release history are separate, long-lived
stores (the catalog) and must not be coupled to job TTLs.

## Decision

Raw job records get per-outcome TTLs, enforced by a retention loop in the
`api` and `standalone` processes:

| Data | Retention | Default |
|---|---|---|
| Succeeded run/job records (payloads included) | success TTL | 7 days |
| Failed / canceled / expired records | failure TTL | 30 days |
| Queued / running / resuming runs | until terminal; expired when stuck | stuck after 24 hours |
| Job logs (stdout/stderr tail, size-capped) | pruned with their job record | — |
| Release history and audit trail (catalog) | independent, long-lived | not pruned |

- Pruning removes the run, its jobs, job logs, run events, and human tasks
  together. `WAITING_HUMAN` runs are never pruned or expired.
- A run is "stuck" when neither it nor any of its jobs has progressed since
  the cutoff; worker heartbeats refresh job timestamps, so actively leased
  jobs are never stuck. Stuck runs transition to `EXPIRED` (jobs to
  `failed`, surfacing as failures in the Monitoring aggregates) and then age
  out on the failure TTL.
- The success TTL floor is the Monitoring dashboard's largest window (7
  days) so the by-app/by-tag aggregates keep working without extra
  infrastructure. Settled counts near the edge of a window can undercount by
  up to one pruner interval; the dashboard reads trends, not ledgers.
- Configuration (flags with environment defaults):
  - `--job-success-retention` / `WINDFORCE_LITE_JOB_SUCCESS_RETENTION_DAYS`
    (default 7 days; `0` keeps forever)
  - `--job-failure-retention` / `WINDFORCE_LITE_JOB_FAILURE_RETENTION_DAYS`
    (default 30 days; `0` keeps forever)
  - `--job-stuck-after` / `WINDFORCE_LITE_JOB_STUCK_AFTER_HOURS`
    (default 24 hours; `0` disables)
  - `--job-retention-interval` (default 10 minutes)

## Phase 2 (not implemented)

Rollup tables for long-range trends — per app/action/status/error-code and
latency, hourly for ~180 days and daily for 365+ days
(`WINDFORCE_LITE_JOB_ROLLUP_HOURLY_RETENTION_DAYS`,
`WINDFORCE_LITE_JOB_ROLLUP_DAILY_RETENTION_DAYS` are reserved for it).
Today's Monitoring UI aggregates at most 7 days, which raw retention covers;
rollups become necessary only when the dashboard grows windows beyond the
success TTL. At that point the summary endpoint should read rollups first
and raw records only inside the raw window, and PostgreSQL deployments
should consider partition-based TTLs instead of `DELETE` batches.

## Consequences

- Storage is bounded by throughput × TTL instead of lifetime job count.
- Debugging a failed run keeps a 30-day window with payloads and log tails;
  anything older exists only as aggregate counts.
- `--job-success-retention` shorter than the Monitoring window makes long
  windows undercount completed runs; deployments that change defaults own
  that trade-off.
