# ADR 0031: Separate operator execution placement from releases

- Status: Accepted
- Date: 2026-08-10
- Issue: [#209](https://github.com/imprun/windforce-core/issues/209)

## Context

The canonical App manifest supplies `tag` and `runsOn` defaults. Some App
repositories commit the configured manifest file directly. Other authoring
pipelines generate it in a deployment snapshot (for example, from an SDK
`--describe` operation) before Core synchronizes that snapshot. Core must treat
both forms as the same completed release input and must not run author code to
discover execution-placement metadata.

The first implementation wrote worker-tag overrides into the active Deployment
JSON. Publishing or rolling back a release replaces that JSON, so an operator
decision could disappear or an old override could return with release history.
Required-label overrides were not supported.

## Decision

Workspace-scoped Execution Placement Policy is an operator-owned object stored separately
from immutable release history. It contains optional App and Action overrides
for worker tag and required labels.

- `null` means inherit from the next lower-precedence source.
- An empty required-label array means explicitly require no labels.
- Existing embedded route overrides are migrated into Execution Placement Policy and
  removed from the active release representation.
- Publishing, rollback, and restart do not replace Execution Placement Policy.
- Existing queued Jobs are not rerouted. Admission resolves the current active
  release plus Execution Placement Policy and pins the effective worker tag and labels onto
  each new Job.

Worker-tag precedence is:

1. Action operator override
2. App operator override
3. Action manifest tag
4. App manifest tag
5. `default`

Required-label precedence is:

1. Action operator override
2. App operator override
3. normalized union of App and Action manifest `runsOn` values

The control-plane App and Action PATCH endpoints accept partial
`tag_override` and `required_labels_override` updates. GET views expose the
release default, operator override, and effective value independently.
These existing wire fields retain their route-tag names for compatibility; the
human-facing product term is execution placement and worker tag.

Git-source probe responses expose a placement-only preview of the configured
canonical manifest. Registration may include an initial App policy only after
that probe establishes the App identity. The policy is stored independently of
the Git source and applies when the first Release is published.

## Consequences

- Release history remains an immutable description of the released artifact.
- Operators can tune worker placement without changing or rebuilding App
  source.
- The Console can warn when no live worker matches without blocking policy
  storage.
- The Web Console is optional. Headless installations may reconcile the same
  policy through the canonical API and keep `/ui` outside their ingress.
- Hosted portals or self-hosted deployment automation own WorkerGroup creation,
  credentials, scaling, and selector vocabulary. Core exposes neutral worker
  management, observation, and placement APIs without duplicating that control
  plane in its Console.
- Local JSON state and PostgreSQL use the same policy semantics. PostgreSQL adds
  a dedicated `control_routing_policy` table.
- A separately generated canonical manifest remains a release input; Core does
  not assume that `windforce.json` exists in the author repository.
