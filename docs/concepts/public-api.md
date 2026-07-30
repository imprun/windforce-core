---
title: Invocation API
description: Admit and observe Runs with scoped system credentials.
---

The Invocation API is Windforce Core's canonical system-to-system boundary.
Operator tools, hosted gateways, product backends, existing external clients,
and out-of-process trigger adapters use the same `/api/v1` routes. The
authenticated principal determines permissions; the request body cannot assert
identity.

[Run admission architecture](run-admission.md) explains how this HTTP adapter differs from the in-process AdmissionService, an Invocation SDK, a built-in Trigger and an external Gateway adapter.

The machine-readable specification and examples are stored with the server at
`internal/server/invocation/v1/`. A running Core serves the same OpenAPI source
of truth from `GET /api/v1/openapi.json`.

## Principals

The Invocation API accepts four credential forms:

- the instance admin token, as an operator principal;
- a `wfw_` workspace token, as an operator principal for that workspace;
- a `wfk_` Client Registry token, with create/read-own/cancel-own/app-read
  permissions;
- a `wfs_` Service Principal token, with explicitly configured scopes and an
  optional app or app/action allowlist.

Service Principals are managed under
`/api/w/{workspace}/service-principals`. Create and token-rotation responses
return the raw `wfs_` token once. Core stores only its SHA-256 hash. Revoke the
active token before deleting the principal.

The available scopes are:

```text
runs:create
runs:read:own
runs:read:any
runs:cancel:own
runs:cancel:any
apps:read
```

## Admit a Run

```http
POST /api/v1/workspaces/{workspace}/runs
Authorization: Bearer <OPERATOR_CLIENT_OR_SERVICE_TOKEN>
Idempotency-Key: source-delivery-123
Content-Type: application/json

{
  "app": "orders",
  "action": "ingest",
  "input": {
    "order_id": "order-20260727-0001"
  },
  "correlation_id": "gateway-request-018"
}
```

A new admission returns HTTP 201. Replaying the same `Idempotency-Key` with the
same authenticated principal and request returns HTTP 200 and the original
Run. Reusing the key with different app, action, input, or correlation data
returns HTTP 409.

```json
{
  "run_id": "f861604b-f013-46f3-80bb-b9dcdfd9b35c",
  "state": "queued",
  "app": "orders",
  "action": "ingest",
  "correlation_id": "gateway-request-018",
  "created_at": "2026-07-27T08:00:00Z",
  "updated_at": "2026-07-27T08:00:00Z"
}
```

`Location` points to the Run status URL and `X-WF-Run-Id` contains the same Run
ID. Invocation responses never expose Job ID, lease, attempt, stored
credentials, or principal internals.

`client_id`, `created_by`, `permissioned_as`, `env`, adapter, and trigger
identity fields are not accepted. Client and service identity comes from the
credential. Operator settings use InputConfig, Variable, or Resource; action
data uses `input`.

## Wait, status, result, and cancel

```text
POST /api/v1/workspaces/{workspace}/runs/wait?timeout=30s
GET  /api/v1/workspaces/{workspace}/runs/{run_id}
GET  /api/v1/workspaces/{workspace}/runs/{run_id}/result
POST /api/v1/workspaces/{workspace}/runs/{run_id}/cancel
GET  /api/v1/workspaces/{workspace}/apps/{app}
```

A completed wait or result request returns the raw Action result with HTTP 200.
If the wait expires or the polled Run is not terminal, the API returns HTTP 202
with the current Run representation. Read and cancel operations enforce
read-own/cancel-own or read-any/cancel-any scope and workspace ownership.

## Input settings

Input settings are applied from least specific to most specific:

1. app defaults for all clients;
2. action settings for all clients;
3. app settings for the authenticated Client Registry entry;
4. action settings for that client;
5. caller input fields that are not locked.

Service and operator principals do not impersonate a Client Registry entry, so
client-specific InputConfig layers are not applied. All principals use the same
active release resolution, schema validation, release pinning, and atomic
Run-plus-first-Job admission.

## HTTP status policy

| Status | Meaning |
| --- | --- |
| 200 | Existing Run replayed, status read, cancel completed, or terminal result returned |
| 201 | New asynchronous Run admitted |
| 202 | Wait timeout expired or result is not terminal |
| 400 | Request shape, app/action key, input, schema, or locked-key validation failed |
| 401 | Authentication is missing, invalid, rotated, or revoked |
| 403 | Principal lacks scope, ownership, workspace, or target permission |
| 404 | Workspace, App, Action, or Run does not exist |
| 409 | Workspace is archived, idempotency request differs, or admission conflicts |
| 429 | Instance Invocation API rate limit exceeded |
| 503 | Admission is unavailable |

The v0.3 server exposes no legacy Public or Execution admission routes. All
new and migrated integrations use the canonical Run routes above.
