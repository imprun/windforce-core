# ADR 0019: Fenced bulk queue-demand snapshots

## Status

Accepted (2026-08-01).

## Context

Core already owns queue ordering, route-tag and worker-label matching, maximum
concurrency, claim leases, cancellation, and completion. The existing
workspace `jobs/summary` endpoint serves a recent-activity dashboard. It does
not provide a consistent store fence, evaluate worker selectors, or expose
expired leases that can become claimable when capacity is zero.

An external capacity manager must be able to observe Core without importing
Core packages or reading its database. Hosting-product resource, entitlement,
and tenancy concepts are not Core concepts and cannot become part of this
public engine contract.

## Decision

Core provides an instance-administrator operation:

```text
POST /api/queue-demand-snapshots
```

The request contains at most 100 caller-keyed selectors. Each selector is a
workspace plus the route tags and labels that a worker would offer. Tags and
labels use the exact set semantics already defined by ADR 0009 and enforced by
the claim path. The caller key is opaque and is only echoed for correlation.

All selector results come from one authoritative read and share:

- a persistent opaque `store_epoch`;
- a monotonically increasing `snapshot_revision` inside that epoch; and
- the authoritative UTC `observed_at` used for lease-expiry decisions.

For one selector:

- `claimed` is the number of matching running jobs with an unexpired lease;
- `busy_workers` is the number of distinct lease owners in `claimed`;
- candidates are non-canceled queued jobs plus running jobs whose lease has
  expired and can be reacquired;
- `eligible` is the number repeated claims by that selector could acquire from
  the observed state, in normal priority/creation/id order and under the same
  app maximum-concurrency rules as claim;
- `queued` and `expired_reacquirable` are disjoint eligible diagnostics; and
- `oldest_eligible_at` is the oldest accepted candidate creation time.

Local state stores the epoch and revision in the atomic JSON snapshot and
increments the revision on every state write. PostgreSQL stores the fence in a
singleton table, increments the revision transactionally after every statement
that mutates `jobs`, and reads the fence plus active jobs in one repeatable-read
transaction. Wall-clock time is never used as a revision.

Revisions are comparable only when epochs match. Initializing a new store
creates a new epoch. An operator restoring an older point-in-time store must
rotate the epoch as part of the restore procedure. Consumers must treat an
epoch change as a new observation window and a revision regression inside one
epoch as unsafe.

## Consequences

- One request can evaluate overlapping selectors across multiple workspaces
  without combining observations from different revisions.
- An external capacity manager can scale from zero after a lease expires; the
  next real claim transaction remains the authority that requeues and claims
  the job.
- The operation is global and therefore never accepts workspace credentials.
- Output is bounded by selector count, while active-queue query optimization
  can evolve without changing the HTTP contract.

## Non-goals

- Hosting-product resource, autoscaling, infrastructure, or tenancy semantics.
- Reserving managed tags or issuing selector-bound worker credentials.
- Changing queue order, claim leases, or the workspace activity summary.
