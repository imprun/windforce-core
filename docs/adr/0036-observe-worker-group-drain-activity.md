# ADR 0036: Observe WorkerGroup drain activity from one store snapshot

## Status

Accepted (2026-08-14). Extends ADR 0019, ADR 0023, and ADR 0025. Tracking: issue #219.

## Context

ADR 0025 lets an instance administrator fence new managed claims for one WorkerGroup, but Core exposes the group run state, Worker registry, and running Jobs through separate reads. An external controller cannot prove from those independently changing views that a draining group has no active leases or running Jobs. The sanitized credential list also does not prove which credential generation live Workers registered with.

The operation is useful to self-hosters as well as a hosted control plane: credential rotation must observe the new generation before revoking the old one, and replica reduction must not interrupt admitted work. Core therefore owns the neutral observation contract while fleet rollout, autoscaling, and desired-state reconciliation remain outside the engine.

## Decision

Core provides an instance-admin endpoint:

`GET /api/worker-groups/{group}/observation`

Local storage computes the response under one store lock from one state snapshot. PostgreSQL uses one read-only repeatable-read transaction and one database observation time. The response contains the group run state and revision, deadline, observation time, live Worker and available slot counts, active lease and running Job counts, a redacted live Worker count by credential generation, and an explicit `quiescent` result.

Credential generation zero identifies registrations that used the legacy static Worker Plane credential. The generation aggregate intentionally omits Worker IDs, credential IDs, endpoints, bearer values, token hashes, and request fingerprints.

Available slots are the total slots of live Workers in `active` status minus
the group's pinned active leases, clamped at zero. A draining group reports zero
available slots because new managed claims are fenced even while idle Workers
remain registered.

Quiescence means the group run state is `draining`, no live generation-zero
Worker can bypass that managed claim fence, and no attributed active lease or
running Job remains. It does not require live managed Worker registrations to
reach zero: the controller first observes quiescence, then reduces replicas.
Attribution comes only from the immutable claim-time identity in [ADR
0038](0038-bind-registered-worker-claims-to-immutable-lease-identity.md), never
from joining `lease_owner` to the current registry. A running Job without that
identity, including an existing row migrated while running or an unregistered
legacy claim, is counted as unattributed. Any unattributed active lease or
running Job keeps every group observation non-quiescent.

The endpoint is read-only. It does not requeue expired leases, delete stale registry records, change run state, revoke credentials, scale Workers, or promise future capacity after the returned observation time.

## Consequences

- A self-hosted controller can gate generation rotation and drain-driven replica reduction without reading Core storage or reconstructing mutable state from workspace-scoped endpoints.
- Local and PostgreSQL counts have an explicit within-response consistency boundary.
- Unattributed running work can conservatively delay an unrelated group
  rollout. This is preferable to falsely declaring quiescence; all new
  registered claims now persist immutable group attribution.
- Core still does not own desired replicas, Kubernetes rollout, autoscaling, tenant lifecycle, or hosted product policy.
