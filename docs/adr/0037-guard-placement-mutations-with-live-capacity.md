# ADR 0037: Guard placement mutations with live capacity

## Status

Accepted (2026-08-14). Extends ADR 0030 and ADR 0031. Tracking: issue #219.

## Context

Execution Placement Policy is stored independently from immutable Releases and is resolved only when a new Run is admitted. The existing App and Action PATCH endpoints intentionally save a policy even when no current Worker matches because the embedded Console may configure policy before capacity is deployed.

Headless reconcilers need a stricter opt-in path. Checking `GET /workers` before PATCH would duplicate Core's tag, required-label, inheritance, execution-profile, managed credential, and WorkerGroup run-state rules. It would also separate the capacity observation from the policy mutation and audit write.

The existing policy had no revision or durable operation replay result, and placement audit was appended by the HTTP handler after the policy mutation on a best-effort basis. A failed audit write could therefore leave an unaudited policy change.

## Decision

Every Execution Placement Policy mutation advances one App-scoped monotonic revision. Existing policies without a revision migrate to revision zero. The revision covers the App override and every Action override, so concurrent App and Action mutations cannot silently overwrite one another.

The existing warning-allow PATCH contract remains the default. An operator opts into fail-closed capacity by adding this object to the existing patch body:

```json
{
  "tag_override": "browser",
  "required_labels_override": ["linux", "kr"],
  "precondition": {
    "operation_id": "placement-20260814-01",
    "expected_policy_revision": 3,
    "minimum_matching_slots": 1
  }
}
```

The precondition requires a valid operation ID, the exact current revision, and a positive minimum slot count. An App patch evaluates the candidate App selector and every active Action after inheritance. An Action patch evaluates only that exact Action. Every evaluated target must meet the requested minimum.

Core derives each candidate selector with the same worker tag and required-label functions used to pin Jobs. An engine-owned execution-profile label remains required even when an operator supplies a required-label override. Eligible capacity consists of live Workers in `active` status whose current registry advertisement matches the candidate tag, labels, and execution profile. Managed Workers also require an active, unexpired credential scoped to the workspace and a WorkerGroup that accepts new work. Legacy static Worker credentials retain their historical unrestricted claim scope, so Core evaluates their current registry advertisement without a managed group fence.

`matching_slots` is total advertised compatible capacity, not currently idle capacity. A busy compatible Worker still proves that the required execution environment exists. Runtime availability after the mutation remains governed by the queue and claim contracts.

Local storage computes selector resolution, capacity counts, OCC, policy update, operation replay state, and audit append under one store lock and writes one state snapshot. PostgreSQL performs the same work in one serializable transaction and uses one database timestamp. A serialization conflict is retried before it is surfaced.

A successful response returns the applied policy revision, `checked_at`, the requested minimum, and redacted per-target effective tag, labels, execution profile, matching Worker count, and matching slot count. It never returns Worker IDs, endpoints, credential IDs, bearer values, token hashes, or request fingerprints. Capacity insufficiency returns 422 with the same redacted candidate observations and does not change policy, revision, replay state, or audit. A stale revision or conflicting operation fingerprint returns 409 with the same no-mutation guarantee.

The latest successful operation ID and request fingerprint are stored with its redacted result. An exact retry returns the original result and revision without reevaluating the Worker registry or appending another audit record. Any later placement mutation replaces that latest-operation replay fence.

The transaction snapshot is a mutation precondition, not a future-availability reservation. A Worker may disappear immediately after the serializable operation. Core does not roll the policy back; a later Run is admitted with the saved policy and remains queued until compatible capacity returns.

## Consequences

- Existing Console and Core-only callers keep warning-allow behavior when they omit `precondition`.
- Self-hosted and hosted reconcilers can fail closed without reimplementing Core placement rules.
- App and Action edits share one revision, making OCC conservative across the complete operator policy object.
- Placement audit is atomic with every post-release App or Action policy mutation in Local, PostgreSQL, and file-catalog storage.
- The API proves compatible advertised slots at one serializable snapshot; it does not reserve a Worker, promise idle capacity, autoscale a fleet, or roll back after later Worker loss.
- Cloud and other external operators still own desired replicas, rollout sequencing, commercial policy, and synchronization status.
