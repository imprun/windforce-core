# ADR 0039: Project WorkerGroup placement observations

## Status

Accepted (2026-08-15). Extends ADR 0036, ADR 0037, and ADR 0038. Tracking: issue #229.

## Context

The embedded Console currently reads the instance-wide Worker registry and counts matching Workers in the browser. That calculation considers liveness, tags, labels, and execution profiles, but it cannot apply managed Worker credential generation, credential status, workspace scope, or WorkerGroup drain state. The displayed ready count can therefore disagree with the Worker that Core would allow to claim the Job or with the capacity precondition from ADR 0037.

The raw registry also contains physical Worker IDs, build metadata, and groups that a workspace operator does not need. A read model must explain placement without turning the workspace control API into a Worker identity or credential inventory.

Self-hosters need to observe which generic execution groups can run an App. Managed fleet creation, scaling, rollout, desired build selection, and Kubernetes resources remain outside Core.

## Decision

Core provides two workspace-scoped, read-only projections:

```text
GET /api/w/{workspace}/worker-groups
GET /api/w/{workspace}/apps/{app}/placement-candidates
GET /api/w/{workspace}/apps/{app}/actions/{action}/placement-candidates
```

The WorkerGroup inventory returns redacted group-level observed state, live and unmanaged Worker counts, available slots, active leases and Jobs, advertised tags, labels and execution profiles, last heartbeat, active credential count, and a self-reported engine/build drift summary. An active workspace-scoped credential keeps an offline group visible even when no Worker is live. A registered static Worker is workspace-compatible capacity and remains visible as unmanaged capacity because ADR 0037 and the claim path allow it without a managed credential fence.

Placement candidates use the active Deployment after persistent placement policy has been applied. The App endpoint evaluates the App and every Action, while the Action endpoint evaluates only that Action. Effective selectors, total matching Worker and advertised slot counts, and every group breakdown come from the same State Store snapshot and the same eligibility and selector helpers used by ADR 0037. Matching slots remain compatible advertised capacity, not an idle-capacity reservation.

Stable exclusion reasons are `workspace_not_allowed`, `draining`, `no_live_capacity`, `missing_tag`, `missing_label`, and `execution_profile_mismatch`. A workspace principal receives only groups usable by that workspace. An instance administrator may request the same workspace path and receives all observed groups with `workspace_allowed`; only that administrator can receive `workspace_not_allowed`. The projections never return Worker IDs, endpoints, credential IDs, generations tied to identities, bearers, token hashes, or request fingerprints.

Engine version and build revision are self-reported diagnostic values. The projection reports distinct observed values and whether a group contains drift, but drift does not make a Worker ineligible. Desired build selection and enforcement require a separate compatibility decision.

Local storage computes each response under one store lock from one state snapshot. PostgreSQL uses one read-only repeatable-read transaction and one database timestamp. The inventory and candidate endpoints do not mutate registry expiry, policy, audit, or WorkerGroup state.

The existing raw `GET /api/w/{workspace}/workers` endpoint becomes instance-admin-only. Workspace clients use the redacted inventory and placement-candidate projections instead.

This decision does not add a WorkerGroup override to Execution Placement Policy. Tag, required-label, and execution-profile matching remain the scheduling contract. Pinning an App to a named group would change claim semantics and requires a separate ADR.

## Consequences

- The Console no longer duplicates placement authorization and no longer overstates capacity from another workspace credential scope.
- Headless self-hosted operators can inspect the same redacted explanation without exposing physical Worker identities.
- Offline managed groups, draining groups, and registered static capacity remain distinguishable.
- A group can be draining while a static Worker with the same group name remains claim-compatible; the candidate breakdown reflects the static Worker and the inventory reports unmanaged capacity.
- Build drift is visible as diagnostics but does not silently introduce a new claim rejection rule.
- Cloud and external WorkerOps continue to own provisioning, scaling, rollout, credentials, and desired build policy through Core's public HTTP contracts.
