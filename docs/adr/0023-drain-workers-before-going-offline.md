# ADR 0023: Drain Workers before going offline

## Status

Accepted (2026-08-02).

## Context

Standalone already received process signals, but the independent local and remote Worker commands ran with `context.Background()`. When a cancellation context did reach `Processor.RunLoop`, it propagated directly into the active launcher. SIGTERM therefore canceled a claimed Job immediately, while the registry exposed no distinction between a Worker accepting work and one finishing its last Job.

A Worker must stop admitting new work promptly during rollout while giving a leased Job a bounded opportunity to finish. It must also complete or fail the Job before it disappears, and local and remote Worker modes must preserve the same lifecycle.

## Decision

1. Worker registry records expose `active` and `draining`. `offline` is represented by record absence after deregistration.
2. Standalone and independent Worker commands install interrupt and SIGTERM handling and pass the resulting context to the same `Processor.RunLoop`.
3. On cancellation, the Processor stops making new claims and upserts the Worker as `draining`; registry and Job lease heartbeats continue during the bounded drain.
4. A claimed Job runs on a context detached from the process signal for `--drain-timeout`, which defaults to 30 seconds. Its lease heartbeat continues during that period.
5. When the drain deadline expires, the Processor cancels the active launcher. Job finalization uses a shutdown-independent context so the failure or terminal result can still be written with lease fencing.
6. The Processor deregisters only after the active Job has completed or timed out. Local and remote backends carry the same status and completion semantics.
7. A normal signal-driven drain returns success from `RunLoop`; operational execution failures before shutdown retain the existing retry behavior.

## Consequences

- Rollouts can observe `active -> draining -> offline` and avoid assigning new work to a terminating Worker.
- Short Jobs finish normally during shutdown; long Jobs receive a deterministic upper bound.
- Completion remains explicit instead of abandoning a lease when the process signal fires.
- PostgreSQL and local registry storage, the Worker Plane request, and the remote client all carry the status field.
- Fleet orchestration remains outside Core; Core only exposes and enforces the lifecycle of one Worker process.

## Rejected alternatives

- **Cancel the launcher immediately on SIGTERM.** Rejected because routine rollouts unnecessarily fail in-flight work.
- **Wait forever for the active Job.** Rejected because a stuck App could prevent deployment and process termination indefinitely.
- **Deregister before the active Job completes.** Rejected because the registry would report offline while a Worker still owns and heartbeats a lease.
- **Retain an offline row.** Rejected because the live Worker registry already defines offline as absence and has separate expiry behavior for crashed Workers.
