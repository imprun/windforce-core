# ADR 0033: Pin and enforce opaque-key execution concurrency

- Status: Accepted
- Date: 2026-08-11
- Issue: [#212](https://github.com/imprun/windforce-core/issues/212)

## Context

`maxConcurrent` atomically caps running Jobs for one workspace and App. It
cannot represent a shared external resource such as an account, egress
identity, device, or another App-defined key. Implementing that cap independently
inside every App duplicates distributed locking and can drift across Workers.

Core must provide the scheduling primitive without learning the domain meaning
of a key, storing sensitive key material, or calling an external policy service
from the claim transaction. Concurrency and rate are separate contracts:
concurrency measures leased executions now, while rate consumes attempts over
time.

## Decision

### Release declaration

The canonical App manifest may declare bounded keyed concurrency limits at App
or Action scope:

```json
{
  "executionLimits": {
    "concurrency": [
      {
        "id": "account-egress",
        "maxConcurrent": 2,
        "inputPointers": ["/account_id", "/egress/id"]
      }
    ]
  }
}
```

- An App declaration is shared by every Action in that App.
- An Action declaration applies only to that Action.
- App and Action declarations are cumulative. The existing App-wide
  `maxConcurrent` cap also remains cumulative.
- A scope may define at most eight limits. A limit may contain at most eight
  non-empty RFC 6901 JSON Pointers.
- Every pointed value must exist in Admission's resolved input and be a
  non-null string, number, or boolean. Objects and arrays are rejected.
- Policy IDs are stable release-author identifiers. Changing the identifier
  creates a new namespace; changing the limit body creates a new policy
  revision.

The declaration belongs to the immutable Release. Core v1 does not add an
operator override API for it. A future operator policy may override numeric
limits without changing the release-owned key shape, but must be designed as a
separate persistent control object like Execution Placement Policy.

### Admission pin

Admission evaluates the pointers only after client/App/Action InputConfig has
produced the effective input and after schema validation. It canonicalizes each
scalar with an explicit type marker and length framing. Numerically equivalent
JSON numbers resolve to the same component.

Core derives a digest with HMAC-SHA256 using the stable workspace data-encryption
key and a namespace containing the App, scope, Action when applicable, and
policy ID. The stored Run and Job pin contains only:

- policy ID and App or Action scope;
- SHA-256 policy revision;
- HMAC key digest;
- maximum concurrent executions.

Raw component values and JSON Pointers are not copied into the Job payload.
The Run retains the safe pins so retry and suspend-resume create a new Job with
the original Admission decision. Workspace key-encryption-key rotation does not
change the workspace data-encryption key and therefore does not split active
limit buckets.

The HMAC protects low-entropy identifiers from offline guessing in persisted
pins and diagnostics. It does not make caller-controlled input authoritative.
For an abuse-resistant quota, the declaring system must ensure that the pointed
field is supplied by trusted Admission context or locked operator input rather
than freely chosen by the caller.

### Claim enforcement

All configured limits must have available capacity before a Job is claimed.
The existing priority, creation time, and Job ID ordering remains unchanged; a
blocked candidate is skipped so another eligible key may proceed.

Local state performs the check while holding its existing snapshot mutation
lock. PostgreSQL performs it in the claim transaction:

1. Reap or requeue expired leases as today.
2. Sort all App and opaque-key advisory-lock identities and acquire them in that
   order.
3. Count running Jobs for every pinned key using the indexed safe Job pins.
4. Claim the candidate only when every App and keyed limit is below its pinned
   maximum.

The App advisory-lock identity remains unchanged so a rolling upgrade stays
coordinated with older Core versions. Keyed locks include workspace, scope,
policy ID, and digest. Stable sorting prevents deadlocks for Jobs that hold more
than one keyed limit.

A tightening applies to newly admitted Jobs and blocks them against already
running matching pins. A relaxation allows newly admitted Jobs up to the new
maximum. Policy revision is diagnostic and is deliberately not part of bucket
identity, so a numeric policy update cannot create parallel buckets.

### Lifecycle and ownership

- A running Job consumes capacity. Terminal completion or lease expiry followed
  by recovery releases it.
- A HumanTask hold keeps its process and lease, so it continues to consume
  concurrency. Suspend releases the old Job and the resumed Job reuses the Run's
  pins.
- Failure and retry do not create a new key decision.
- Queue-demand observation applies the same limits so blocked work does not
  falsely request more Workers.
- One Core Cell and its PostgreSQL database are the atomicity boundary. Imprun
  Cloud or another deployment control plane owns cross-Cell quota aggregation,
  WorkerPool desired state, and operator UI.
- The calling domain owns success-rate, target health, and whether to submit work
  now. Those decisions do not run inside Core's claim transaction.

## Deferred rate contract

Rate limiting is not implemented by this ADR. It requires a separate schema and
state machine covering attempt-consumption time, retry charging, clocks,
refill/window behavior, persistence, and safe remaining-budget observations.
It must not reuse the running-Job counter or refund failed attempts.

## Consequences

- Apps gain one reusable multi-Worker concurrency primitive without Redis or a
  domain-specific Core extension.
- Sensitive key material is absent from limiter pins, logs, and indexed claim
  state.
- Local and PostgreSQL stores preserve the same observable scheduling behavior.
- Manifest authors must provide stable scalar key fields in effective input.
- Global limits across independent Core Cells remain outside this primitive.
