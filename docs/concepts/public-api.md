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

Every successful admission response includes `Location`, `X-WF-Run-Id`, `X-WF-Run-State`, and `X-WF-Idempotency-Reused`. The state header is the lowercase Run state observed for that response. The reuse header is always `true` or `false`; it is true only when the authenticated principal and canonical request exactly reused the existing Run for the supplied `Idempotency-Key`. Invocation responses never expose Job ID, lease, attempt, stored credentials, or principal internals.

## Authenticated admission probe and outcome

An external policy gateway can authenticate and bind an idempotent request without creating a Run by sending the exact request with `X-WF-Admission-Probe: true`. A probe requires `Idempotency-Key` and returns HTTP 200:

```json
{
  "admission_id": "run_3a0f7f916c7b2e50431e546a",
  "request_fingerprint": "7db48b7d8fb0...",
  "state": "ready",
  "replayed": false
}
```

The response includes matching `X-WF-Admission-Id`, `X-WF-Admission-Fingerprint`, and `X-WF-Admission-Probed: true` headers. A successful ordinary admission returns the same admission ID and fingerprint together with `X-WF-Run-Id`, forming the receipt an external gateway can commit. `state` is `ready` when no terminal outcome exists, `admitted` with `run_id` when the exact request already created a Run, or `aborted` when an administrator previously fenced an ambiguous request. A probe performs credential or signature verification and canonical request binding but is not an admission approval; the ordinary request re-evaluates all current Core policy.

If the ordinary response is lost, an administrator atomically resolves the outcome instead of treating a not-found read as proof:

```http
POST /api/w/{workspace}/admission-outcomes/{admission_id}/resolve
Authorization: Bearer <ADMIN_TOKEN>
Content-Type: application/json

{"request_fingerprint":"7db48b7d8fb0..."}
```

The operation returns terminal `admitted` with the authoritative `run_id`, or terminal `aborted` and permanently prevents that admission identity from creating a Run. A fingerprint mismatch returns 409. Only this atomic `aborted` result is safe negative evidence; an absent or unknown outcome is not. Core retains the resolving actor in its internal audit state but never returns that principal identifier from the outcome API.

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

The App description returns the pinned Release `api_version` and each visible Action's schemas, timeout, labels, and optional `public_interfaces`. Public interface declarations are bounded opaque JSON objects from the immutable Release; an API consumer applies any domain-specific meaning. Principal target policy filters Actions before their declarations are returned.

A completed wait or result request returns the raw Action result with HTTP 200. A completed wait still exposes the terminal state and idempotency reuse decision through the admission response headers. If the wait expires or the polled Run is not terminal, the API returns HTTP 202 with the current Run representation; its `state` and any `replayed` field agree with the corresponding headers. Read and cancel operations enforce read-own/cancel-own or read-any/cancel-any scope and workspace ownership.

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
| 200 | Admission probe completed, existing Run replayed, status read, cancel completed, or terminal result returned |
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
