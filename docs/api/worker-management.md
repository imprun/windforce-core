---
title: Worker management API
description: Instance-admin credential, rotation, revocation, group drain, and activity observation contracts for remote workers.
---

# Worker management API

These endpoints manage Core-owned remote-worker authority. They require the
instance-admin bearer. A workspace, client, service-principal, Job, or remote
worker bearer is not instance-admin authority.

## Create or rotate a credential

```http
POST /api/worker-groups/{group}/credentials
Authorization: Bearer <instance-admin-token>
Content-Type: application/json

{
  "operation_id": "op_create_01",
  "expected_generation": 0,
  "workspace_ids": ["workspace-a"],
  "labels": ["sys/arch/arm64", "browser"],
  "expires_at": "2026-08-10T00:00:00Z"
}
```

The first credential uses expected generation `0`; rotation uses the current
generation. A new response is `201` and contains `worker_token` exactly once.
An exact operation replay is `200`, sets `replayed: true`, and omits
`worker_token`. Reusing an operation ID with different input or sending a stale
generation returns `409`.

```json
{
  "credential": {
    "id": "worker_credential_...",
    "group": "group-a",
    "generation": 1,
    "workspace_ids": ["workspace-a"],
    "labels": ["browser", "sys/arch/arm64"],
    "status": "active",
    "created_by": "operator:admin",
    "created_at": "2026-08-03T00:00:00Z",
    "updated_at": "2026-08-03T00:00:00Z"
  },
  "worker_token": "wfr_...",
  "replayed": false
}
```

`GET /api/worker-groups/{group}/credentials` lists sanitized generations and
never returns a bearer, token hash, or request fingerprint.

## Revoke a generation

```http
POST /api/worker-groups/{group}/credentials/{credential_id}/revoke

{
  "operation_id": "op_revoke_01",
  "drain_deadline_at": "2026-08-03T00:10:00Z"
}
```

Revocation immediately blocks registration of new active workers and new
claims. Until the drain deadline, already registered workers may keep their
registry and Job lease heartbeats, append logs, complete their fenced leases,
and fetch the pinned execution artifact for that owned lease. The remote client
adds `job_id`, `workspace`, and `worker_id` to artifact fetches; Core rejects a
missing, cross-workspace, wrong-worker, or wrong-digest lease context. Exact
revocation retries are idempotent.

## Drain or resume a group

```http
PUT /api/worker-groups/{group}/run-state

{
  "operation_id": "op_drain_01",
  "expected_revision": 0,
  "state": "draining",
  "deadline_at": "2026-08-03T00:10:00Z"
}
```

The implicit initial state is `running` revision `0`. A successful change
increments the revision. Exact retries return the same revision with
`replayed: true`; stale revisions and conflicting reuse of an operation ID
return `409`. `GET /api/worker-groups/{group}/run-state` returns the current
state.

Draining only fences new managed claims. It does not cancel, revoke, kill, or
automatically resume anything at the deadline. Resume explicitly:

```json
{
  "operation_id": "op_resume_01",
  "expected_revision": 1,
  "state": "running"
}
```

## Observe group activity and drain readiness

```http
GET /api/worker-groups/{group}/observation
Authorization: Bearer <instance-admin-token>
```

The response is computed from one Local store lock/snapshot or one PostgreSQL repeatable-read transaction. It is the Core-owned gate for credential-generation rollout and `draining -> quiescent -> replica reduction`:

```json
{
  "group": "group-a",
  "run_state": "draining",
  "run_state_revision": 1,
  "deadline_at": "2026-08-03T00:10:00Z",
  "observed_at": "2026-08-03T00:01:00Z",
  "live_workers": 2,
  "unmanaged_live_workers": 0,
  "available_slots": 0,
  "active_leases": 1,
  "running_jobs": 1,
  "unattributed_active_leases": 0,
  "unattributed_running_jobs": 0,
  "active_workers_by_generation": [
    {"generation": 1, "workers": 1},
    {"generation": 2, "workers": 1}
  ],
  "quiescent": false
}
```

`active_workers_by_generation` counts live registry activity and includes Workers whose per-Worker status is already `draining`; generation `0` represents the legacy static Worker Plane credential. `available_slots` totals live active Worker slots, subtracts the group's immutable claim-time active leases, clamps at zero, and is always zero while the group run state is `draining`.

`quiescent` becomes true only while the group is `draining`, no live generation-zero Worker can bypass that managed claim fence, and its active lease and running Job counts are zero. Live idle managed Workers may remain registered so an external controller can observe quiescence before reducing replicas. Running Job attribution comes from the immutable group and credential generation pinned when a registered Worker claimed the attempt. A Job without that identity is reported in the unattributed counts and conservatively keeps every group observation non-quiescent until it settles or is requeued. Deregistration or reuse of the same Worker ID cannot rewrite attribution. The endpoint never returns a Worker identity, endpoint, credential ID, bearer, token hash, or request fingerprint.

## Worker command

```sh
export WINDFORCE_WORKER_TOKEN='wfr_...'
windforce-core worker \
  --api-url https://core.example.test \
  --worker-token-env WINDFORCE_WORKER_TOKEN \
  --worker-group group-a \
  --labels sys/arch/arm64,browser
```

The process reads the token from the named environment variable. Do not put the
bearer directly in arguments, manifests, logs, or registry metadata.
Re-registering a worker ID is allowed only for the same credential ID and
generation; another generation or the legacy static token cannot take it over.

## Observe live Workers

```http
GET /api/w/{workspace}/workers
Authorization: Bearer <workspace-or-instance-admin-token>
```

The response lists the live registry. `engine_version` and `build_revision`
identify the Core build reported by each Worker:

```json
{
  "workers": [
    {
      "id": "worker-a",
      "group": "group-a",
      "engine_version": "v0.9.2",
      "build_revision": "abcdef123456",
      "tags": ["default"],
      "labels": ["browser"],
      "execution_profiles": [],
      "slots": 1,
      "live": true,
      "started_at": "2026-08-11T00:00:00Z",
      "last_heartbeat_at": "2026-08-11T00:00:10Z"
    }
  ]
}
```

The fields are optional for compatibility, self-reported, and diagnostic only.
They are not credential authority or signed provenance. Core records observed
state; a hosted portal or self-hosted deployment controller owns the desired
Worker image/version, drift comparison, rollout, and replacement policy.
