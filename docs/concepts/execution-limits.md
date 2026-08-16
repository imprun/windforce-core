---
title: Execution limits
description: App-wide, opaque-key concurrency, and opaque-key fixed-window rate limits enforced atomically across Workers in one Core Cell.
---

Windforce Core has three cumulative execution-limit layers:

- `maxConcurrent` caps all running Jobs for one workspace and App.
- `executionLimits.concurrency` caps running Jobs that resolve to the same
  App-defined opaque key.
- `executionLimits.rate` caps claimed attempts for the same App-defined opaque
  key in a UTC epoch-aligned fixed window.

All are enforced when a Worker claims a Job. A blocked Job remains queued; no
Worker slot is consumed while it waits.

## Release ceilings and operating allowances

Release declarations are immutable safety ceilings. Core also stores a mutable
`ExecutionLimitPolicy` for an operator who needs to lower the capacity of one
Cell without publishing another Release. The console calls this value the
**operating allowance**. Claim enforces the lower compatible value:

```text
effective limit = min(pinned Release ceiling, current operating allowance)
```

An allowance must be a positive integer. Zero is not a pause command. Removing
an allowance returns to the Release ceiling; it never disables a Release safety
limit. A Release ceiling can be lower than a stored allowance, in which case the
ceiling remains effective without rewriting the operator setting.

App-wide concurrency is the one v1 exception that may have an allowance when a
Release omits `maxConcurrent`. Its key is always workspace plus App, so Core can
give it a stable implicit shape. Keyed concurrency and rate allowances require
an exactly matching Release shape. Keyless App-wide rate, Action-wide keyless
limits, billing quotas, and cross-Cell global quotas are outside this contract.

Policy shape includes workspace, App, optional Action, scope, policy ID, kind,
ordered input pointers, and the fixed rate window. Numeric ceilings are not part
of the shape. Core publishes one versioned SHA-256 shape fingerprint to Release
preflight errors, stored policies, Run/Job pins, read-back, and conflicts. It
does not expose the raw pointer values through the operator API or console.

## Declare a keyed limit

Place `executionLimits` at App scope to share capacity across the App's Actions:

```json
{
  "app": "orders",
  "entrypoint": "main.ts",
  "executionLimits": {
    "concurrency": [
      {
        "id": "account-egress",
        "maxConcurrent": 2,
        "inputPointers": ["/account_id", "/egress/id"]
      }
    ],
    "rate": [
      {
        "id": "account-egress",
        "maxAttempts": 120,
        "windowSeconds": 60,
        "inputPointers": ["/account_id", "/egress/id"]
      }
    ]
  },
  "actions": {
    "collect": {}
  }
}
```

Place either declaration inside an Action to limit only that Action. App and
Action limits are cumulative, and `maxConcurrent` still applies when present.

`inputPointers` are RFC 6901 JSON Pointers into the effective input after Core
has merged workspace, App, Action, and client InputConfig. Every selected value
must be a string, number, or boolean. Missing, null, object, or array values
reject Admission so Jobs never fall into an accidental shared bucket.

## Request and claim flow

```mermaid
flowchart TD
    A["Invocation request"] --> B["Admission resolves InputConfig"]
    B --> C["Validate Action input schema"]
    C --> D["Read declared JSON Pointer values"]
    D --> E["Canonicalize scalar components"]
    E --> F["Workspace-key HMAC"]
    F --> G["Pin policy ID, shape fingerprint, digest, ceiling, and window on Run and Job"]
    G --> H["Local or PostgreSQL queue"]
    H --> I["Lock candidate policy identities and read current allowances"]
    I --> L["Atomic claim checks effective concurrency and rate budget"]
    L -->|"all limits allow"| J["Consume rate attempt, create Worker lease, execute"]
    L -->|"a limit blocks"| K["Job remains queued"]
```

Core stores and indexes only the HMAC digest. It does not persist the selected
raw values in the limiter pin. The effective Run input follows the normal
encrypted-at-rest input contract.

Authorized Job status responses expose the safe `execution_limits` pins with
policy ID, revision, scope, digest, maximum, and rate window for diagnosis. They
never expose the selected raw key components.

## Choosing the key

Use a stable field that identifies the resource whose simultaneous use must be
bounded, for example an account ID plus an egress identity. The policy ID is a
namespace, so keep it stable across Releases when old and new Jobs must share
capacity.

The HMAC hides low-entropy values in persisted limiter state, but it does not
stop a caller from changing a caller-controlled field. When the limit is also a
quota or abuse boundary, supply the field from trusted Admission context or
lock it through operator InputConfig.

## Lifecycle behavior

- A running lease consumes capacity.
- Completion or lease-expiry recovery releases capacity.
- A successful claim consumes one rate attempt. Completion, failure,
  cancellation, lease expiry, retry, and recovery do not refund it.
- A retry or recovered Job consumes another rate attempt when it is claimed.
- A HumanTask hold continues to consume capacity because the process and lease
  stay alive.
- Suspend and retry reuse the safe pins stored on the Run.
- Queued work and the next claim after retry or resume use the current compatible
  operating allowance. Tightening does not interrupt a running Job.
- Queue-demand observation excludes Jobs currently blocked by concurrency and
  does not count attempts beyond the remaining current-window rate budget.

## Release transitions and read-back

A forward publication is rejected when it would remove or change the shape of a
stored keyed allowance. A ceiling-only numeric change keeps the same fingerprint
and is allowed. Rollback is never blocked: an incompatible allowance becomes a
derived dormant view for the active Release, remains stored, and still applies
to queued Jobs pinned to its older shape.

The Control API separates three views:

- `desired`: stored operator policies with revision, operation ID, compatibility,
  and audit provenance;
- `observed`: shapes and ceilings in the active Release;
- `enforced`: active-Release effective values plus fingerprint and pinned-ceiling
  cohorts for queued and running Jobs from earlier Releases.

Each enforced active or residual cohort includes `over_allowance_drain`. It is
true only when the matching opaque key currently has more running Jobs than the
effective concurrency limit; different opaque keys are never summed into a
false drain signal. Running Jobs are allowed to finish while new matching claims
wait.

Mutations use optimistic revision checks and an `operation_id`. Replaying the
same operation and payload returns the original revision without another audit
record. Reusing the operation ID with another payload is a conflict. Provisioning
export includes policies, and policy resources are compatibility-preflighted and
applied as one batch; deletion is always explicit.

Reconcilers discover this contract through
`/api/w/{workspace}/system/info`: `capabilities.execution_limit_policy` and
`capabilities.execution_limit_shape` are both `v1`. A missing capability on an
older Core is not equivalent to an empty policy collection.

## Fixed-window behavior

For `windowSeconds: 60`, Core groups attempts into UTC epoch-aligned one-minute
windows. The budget resets at the next boundary; Core does not use a timer tied
to one server process. Local mode uses the Store's injected clock and
PostgreSQL mode uses the database clock inside the claim transaction.

A fixed window can allow a burst approaching twice the configured maximum
across a boundary. This is an explicit v1 tradeoff. Sliding-window and token
bucket semantics are not implied by this contract.

## Boundaries

The atomic boundary is one Core Cell and its database. A hosted control plane
owns cross-Cell global quotas and WorkerPool operations. Domain services own
success-rate and target-health decisions.

Rate limits use durable attempt buckets and never reuse or refund concurrency
counters. Rate is an execution safety primitive, not a commercial billing or
cross-Cell quota system.

The `/metrics` counter `windforce_execution_rate_claims_total`
reports only the Store backend and `consumed` or `blocked` outcome. It does not
label workspace, App, policy, or opaque key identities.

See [ADR 0033](../adr/0033-pin-and-enforce-opaque-key-concurrency.md) for keyed
concurrency and [ADR 0041](../adr/0041-pin-and-enforce-opaque-key-fixed-window-rate.md)
for fixed-window rate semantics and failure behavior. See
[ADR 0042](../adr/0042-enforce-operator-execution-limit-policies-at-claim.md)
for operator policy, rollback, fingerprint, and read-back decisions.
