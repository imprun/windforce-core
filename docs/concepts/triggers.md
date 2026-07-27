---
title: Triggers
description: Configure built-in event sources or write an external trigger against the canonical Invocation API.
---

Triggers convert protocol deliveries or scheduled occurrences into canonical
Run admissions. They do not bypass release resolution, schema validation,
InputConfig, authorization, idempotency, or the durable Run-plus-first-Job
transaction.

## Built-in and external triggers

Built-in `webhook`, `schedule`, and `rabbitmq` adapters run inside the `server`
and `standalone` roles. They implement one lifecycle:

```text
Initialize -> Start -> Stop
```

Each adapter creates a TriggerEvent with a stable `delivery_id`, target,
correlation ID, JSON input, and bounded safe metadata. The TriggerSubmitter
calls AdmissionService directly in-process. It never calls Core's HTTP listener
and it acknowledges a broker delivery only after durable admission succeeds.

An external trigger is a separate process. Give it a `wfs_` Service Principal
with `runs:create`, `runs:read:own`, and an exact app/action allowlist, then call
the canonical `/api/v1` Invocation API. Do not give it an admin token and do not
send caller-supplied identity or `env`.

## Manage definitions

Trigger definitions are workspace Control API resources:

```text
GET    /api/w/{workspace}/triggers
POST   /api/w/{workspace}/triggers
GET    /api/w/{workspace}/triggers/{trigger_id}
PUT    /api/w/{workspace}/triggers/{trigger_id}
DELETE /api/w/{workspace}/triggers/{trigger_id}
POST   /api/w/{workspace}/triggers/{trigger_id}/enable
POST   /api/w/{workspace}/triggers/{trigger_id}/disable
GET    /api/w/{workspace}/triggers/{trigger_id}/audit
GET    /api/w/{workspace}/triggers/{trigger_id}/deliveries
```

Create definitions disabled, verify the target and delivery policy, and enable
them explicitly. `secret_config` is write-only and encrypted at rest. A
representation exposes only `has_secret`; audit details and metrics never
contain secret material or payload values.

## Configured webhook

```json
{
  "name": "partner-orders",
  "kind": "webhook",
  "app": "orders",
  "action": "ingest",
  "config": {
    "signature_header": "X-WF-Signature-256",
    "delivery_id_header": "X-WF-Delivery-Id",
    "correlation_header": "X-WF-Correlation-Id",
    "input_mode": "json"
  },
  "secret_config": {
    "secret": "replace-with-a-random-secret"
  }
}
```

Deliver to:

```http
POST /api/v1/workspaces/{workspace}/triggers/{trigger_id}/events
X-WF-Signature-256: sha256=<hex HMAC-SHA256 of the exact request body>
X-WF-Delivery-Id: partner-delivery-123
Content-Type: application/json
```

The configured HMAC is the ingress credential; this exact route does not use a
Control or Invocation bearer token. Missing or invalid signatures return 401.
Admission returns 202 with `run_id` and `replayed`. The request body is bounded
to 1 MiB. Authorization, cookie, and signature headers are excluded from safe
metadata.

Use `input_mode: raw` only when the Action expects a JSON envelope containing
`raw_base64` and `content_type`. If no delivery header is present, Core derives
a stable body digest. Send a source delivery ID whenever identical payloads may
represent distinct events.

This resource is inbound execution admission. It is separate from the
Control-plane Webhook subscription resource, which sends release events out of
Core.

## Public HTTP Route Bindings

The configured webhook path above is the canonical ingress and remains
available in every deployment mode. When an external Router Provider is
configured, an operator can attach one or more friendly public URLs without
changing the Trigger definition:

```http
POST /api/w/{workspace}/triggers/{trigger_id}/routes
Content-Type: application/json

{
  "hostname": "hooks.example.com",
  "path": "/orders/events",
  "visibility": "public",
  "provider": "auto"
}
```

The response begins with `state: pending`. The Router Provider reconciles the
route and reports `ready`, `error`, or deletion completion with the desired
`generation`. Only a current generation can become `ready`.

Route Binding is provider-neutral desired and observed state. Core does not
store Kubernetes `HTTPRoute`, Ingress, listener, certificate, DNS, namespace,
or hosted tenant fields. A self-hosted Kubernetes controller may translate the
binding into Gateway API resources. Imprun Cloud instead registers the alias
inside its existing wildcard Cloud Gateway and rewrites to the cell's canonical
ingress; it does not create one cluster `HTTPRoute` per Trigger.

Deleting a public route returns a `deleting` tombstone. The Trigger and
canonical ingress remain active while the provider removes the external route.
Provider reconciliation uses:

```text
GET /api/w/{workspace}/http-route-bindings?include_deleted=true
PUT /api/w/{workspace}/http-route-bindings/{binding_id}/status
```

The Web UI shows Public routes only when system info advertises a configured
provider. Standalone without a provider shows only the canonical ingress.
See [ADR 0015](../adr/0015-provider-neutral-http-route-bindings.md).

## Schedule

```json
{
  "name": "daily-reconciliation",
  "kind": "schedule",
  "app": "orders",
  "action": "reconcile",
  "config": {
    "cron": "0 9 * * *",
    "timezone": "Asia/Seoul",
    "input": {
      "scope": "daily"
    }
  }
}
```

Schedules use the standard five-field cron format and require an IANA timezone.
Core skips occurrences missed while the trigger is stopped. The scheduled UTC
instant forms the occurrence delivery ID, so multiple replicas and restarts
converge on one admission through idempotency. Actions receive the same instant
as `WF_SCHEDULED_FOR`.

Disabling a schedule prevents later occurrences. Archiving its workspace stops
the adapter. A retryable admission error is retried with the same occurrence
ID; no catch-up burst is generated.

## RabbitMQ

```json
{
  "name": "order-queue",
  "kind": "rabbitmq",
  "app": "orders",
  "action": "ingest",
  "config": {
    "queue": "orders.windforce",
    "prefetch": 8,
    "concurrency": 4,
    "delivery_id_header": "x-source-delivery-id",
    "input_mode": "json"
  },
  "secret_config": {
    "url": "amqps://user:password@rabbitmq.example.test/vhost"
  }
}
```

The queue must already exist. Core reconnects with bounded exponential delay,
uses manual acknowledgement and QoS, and drains in-flight deliveries during
shutdown:

| Admission result | RabbitMQ action |
| --- | --- |
| admitted or idempotent replay | ACK |
| unavailable or internal error | NACK with requeue |
| invalid, forbidden, missing target, or conflict | Reject without requeue |

Configure the queue's dead-letter exchange for terminal rejects. Prefer AMQP
`message_id` or the configured header as the stable delivery ID; the body
digest is only a fallback.

## External trigger request flow

1. Receive the source event and preserve its stable delivery identifier.
2. Validate and normalize it into Action input.
3. Call `POST /api/v1/workspaces/{workspace}/runs` with a scoped `wfs_` token
   and `Idempotency-Key: <source delivery id>`.
4. Treat 201 as new admission and 200 as a successful replay.
5. Reconcile an uncertain timeout with the same idempotency key or the returned
   Run ID; never mint a new key for the same source delivery.
6. Retry 429 and 503 with bounded backoff. Treat 400, 403, 404, and a
   mismatched-idempotency 409 as terminal until configuration changes.
7. Acknowledge the source only after Core has admitted the Run.

See [Invocation API](public-api.md) for request and response fields.
