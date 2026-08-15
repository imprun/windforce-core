# ADR 0040: Project pinned execution demand against slot capacity

## Status

Proposed (2026-08-15). Extends ADR 0036 through ADR 0039. Tracking: issue #231.

## Context

ADR 0039 explains which current WorkerGroups can run the active App and Action targets, but advertised matching slots do not tell an operator whether queued work has free capacity. The existing `POST /api/queue-demand-snapshots` contract answers a different Worker-side question: for caller-supplied tag and label selectors, how much work could a polling Worker claim under queue concurrency limits? It does not discover the selectors already pinned into queued Jobs and it does not join them to the redacted WorkerGroup inventory.

A queued Job keeps the App, Action, tag, required labels, and execution profile selected at admission. A newer Deployment or placement policy can differ while that Job waits. Recomputing demand from the current Deployment would hide this drift and could claim that old work has capacity when it does not.

Queued work can also match more than one WorkerGroup, and one Worker can be compatible with more than one queued target. Assigning a Job to every candidate group would multiply backlog counts. Summing compatible capacity across different targets would likewise count shared slots more than once.

CPU, memory, Pod, node, cost, desired replicas, rollout, and autoscaling signals are not authoritative Core state. They remain Cloud or external WorkerOps responsibilities.

## Decision

Core adds three workspace-scoped read-only projections:

```text
GET /api/w/{workspace}/execution-demand
GET /api/w/{workspace}/apps/{app}/execution-demand
GET /api/w/{workspace}/apps/{app}/actions/{action}/execution-demand
```

Every currently queued Job contributes exactly once to a target keyed by its admission-pinned workspace, App, Action, effective tag, effective required labels, and execution profile. The projection reports target and response-level queued counts plus `oldest_queued_at`. The timestamp is the Job's current queued-state `updated_at`; initial enqueue therefore begins at creation time, while a later requeue begins a new wait interval.

For each pinned target, Core evaluates current Workers with the same liveness, managed credential generation and status, workspace allowlist, WorkerGroup draining, static Worker compatibility, tag, label, and execution-profile rules used by placement admission. It reports matching Workers and advertised slots, active non-expired leases owned by those exact matching Worker IDs, remaining slots, saturation, and the redacted group breakdown. A structurally compatible group remains `eligible` when full; `saturated` records the temporary lack of a free slot. No matching slots is distinct from saturation.

The ADR 0039 WorkerGroup inventory is extended with total, occupied, and available slots for Workers currently eligible for new claims. Occupancy is calculated from active non-expired leases owned by those exact Worker IDs, including the static compatibility pool; immutable group lease attribution remains separately available for drain and current-work observations.

The projection does not assign queued counts to WorkerGroups. Candidate groups explain possible service capacity only. Capacity values are valid within one target; clients must not sum capacity across targets because compatible Worker sets can overlap. Workspace totals expose backlog and oldest wait, while overall fleet slot usage continues to come from the WorkerGroup inventory where each group is represented once.

Workspace credentials receive only workspace-allowed groups. Instance administrators can receive excluded groups with redacted reason codes. Responses never expose Job or Run IDs, physical Worker IDs, lease owners, credential IDs, bearer tokens, hashes, or request fingerprints.

Local storage computes the observation under one lock from one snapshot. PostgreSQL uses a read-only repeatable-read transaction and one database timestamp. No database migration is required because Jobs already persist the pinned selector and lease owner.

The Worker-side queue-demand snapshot remains unchanged. It continues to apply concurrency gating to caller-supplied selectors for polling decisions. The execution-demand projection reports all queued Jobs and selector-compatible slot pressure for operations; it does not promise that every queued Job is immediately claimable under App or keyed concurrency limits.

## Consequences

- Operators can distinguish no matching capacity, available compatible capacity, and fully occupied compatible capacity without exposing Worker identities.
- Old and current placement targets for the same App or Action remain visible as separate queued demand records.
- Queued Jobs are not duplicated when several groups could execute them.
- Static Workers, managed draining fences, credential scope, and Local/PostgreSQL behavior remain aligned with claim admission.
- Core does not become an infrastructure metrics, autoscaling, or billing system. Cloud and WorkerOps can combine this neutral demand signal with their own CPU, memory, Pod, release, and cost observations.
