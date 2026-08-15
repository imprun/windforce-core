# ADR 0041: Pin and enforce opaque-key fixed-window execution rate

- Status: Accepted
- Date: 2026-08-15
- Issue: [#212](https://github.com/imprun/windforce-core/issues/212)

## Context

ADR 0033 introduced opaque-key concurrency for shared resources that must cap simultaneous running Jobs. It deliberately did not model rate because rate consumes attempts over time, does not release capacity when an attempt finishes, and needs persistent window state and an explicit clock contract.

Core needs a general execution primitive that Apps can use for account, egress, device, vendor, or other App-defined rate keys without teaching Core those domain meanings. The primitive must behave atomically across Workers in one Cell, preserve Local and PostgreSQL parity, avoid persisting raw key material, and keep claim transactions independent from Cloud or domain policy services.

## Decision

### Release declaration

The canonical App manifest may declare bounded keyed rate limits at App or Action scope:

```json
{
  "executionLimits": {
    "rate": [
      {
        "id": "account-egress",
        "maxAttempts": 120,
        "windowSeconds": 60,
        "inputPointers": ["/account_id", "/egress/id"]
      }
    ]
  }
}
```

- An App declaration is shared by every Action in that App.
- An Action declaration applies only to that Action.
- App rate, Action rate, App keyed concurrency, Action keyed concurrency, and the existing App-wide `maxConcurrent` cap are cumulative.
- A scope may define at most eight rate limits. A limit may contain at most eight non-empty RFC 6901 JSON Pointers.
- `maxAttempts` must be positive. `windowSeconds` must be between 1 and 86,400 inclusive.
- Every pointed value must exist in Admission's resolved input and be a non-null string, number, or boolean. Objects and arrays are rejected.
- Policy IDs are stable release-author identifiers. Changing the identifier creates a new namespace; changing the limit body creates a new policy revision.

The declaration belongs to the immutable Release. Core v1 does not add an operator override or mutable quota API.

### Admission pin

Admission resolves key material after client/App/Action InputConfig and schema validation, using the same typed scalar canonicalization as keyed concurrency. It derives an HMAC-SHA256 digest with the stable workspace data-encryption key and a distinct `rate/v1` namespace containing the App, scope, Action when applicable, and policy ID.

The stored Run and Job pin contains only the policy ID, scope, policy revision, HMAC key digest, maximum attempts, and window duration. Raw component values and JSON Pointers are not copied into the Job payload. Retry and suspend-resume reuse the original Run pins, so a new attempt cannot choose a different limiter key.

Policy revision is diagnostic and is not part of bucket identity. Jobs admitted before and after a numeric policy change therefore share the same policy namespace and key bucket while each claim observes the maximum and duration pinned on its own Job. Changing `windowSeconds` creates a distinct bucket identity because it changes the window partition.

### Fixed-window semantics

Rate v1 uses UTC epoch-aligned fixed windows. For a duration `D`, the current window is `[floor(now / D) * D, floor(now / D) * D + D)`. A successful claim consumes one attempt from every pinned rate policy before the Worker lease is returned.

Completion, failure, cancellation, lease expiry, retry, and recovery never refund a consumed attempt. A retry or recovered Job consumes another attempt if it is claimed again. An idempotent invocation replay that returns the already admitted Run does not create or consume an additional attempt until another Job claim actually occurs.

A fixed window can permit a burst approaching twice the configured maximum across a boundary. This is an explicit v1 tradeoff for a small, durable, auditable state machine. Sliding windows and token buckets are deferred alternatives and must not be introduced as silent semantic changes.

### Atomic claim enforcement

All pinned concurrency and rate limits must allow a candidate before it is claimed. A blocked candidate remains queued and is skipped so another eligible key may proceed.

Local state checks and consumes rate buckets while holding its existing snapshot mutation lock and uses the Store's injected clock. PostgreSQL uses `CURRENT_TIMESTAMP` from the database inside the claim transaction, locks the sorted concurrency and rate identities, checks every current bucket, and only then increments every applicable bucket and creates the lease in the same transaction. A crash or transaction rollback before commit consumes nothing; a committed claim remains charged even if the Worker never completes the Job.

The PostgreSQL migration creates persistent bucket state keyed by workspace, scope, policy ID, HMAC digest, and window duration. A later window overwrites that key's prior bucket atomically; stale rows are ignored outside their window and may be pruned independently without making unrelated claim transactions delete each other's state. Neither Store uses a process-local timer as the source of truth.

### Observation and ownership

Worker queue-demand snapshots simulate the current remaining budget and exclude queued attempts that cannot be claimed in the current rate window. Authorized Job status exposes only safe pinned fields. The Prometheus counter `windforce_execution_rate_claims_total` labels only the Store backend and `consumed` or `blocked` outcome; workspace, App, policy, and digest identities are deliberately excluded. Core does not expose raw key values or turn low remaining budget into a domain health judgment.

One Core Cell and its database are the atomicity boundary. Imprun Cloud or another deployment control plane owns cross-Cell or commercial quotas, product packaging, and operator synchronization. Domain services own success-rate, target-health, cooldown, and submission decisions. This ADR does not add automatic Worker scaling, an operator mutation UI, or a global quota service.

## Compatibility

`executionLimits.rate` is optional. Existing Releases, Runs, Jobs, provisioning archives, and API callers that omit it retain their previous behavior. Safe rate pins use additive canonical and OpenAPI fields. Local snapshots initialize an empty bucket map, and PostgreSQL applies an idempotent additive migration.

Every Core process that can claim Jobs from the same database must be upgraded before a Release using `executionLimits.rate` is published. Older claimers do not understand the additive rate pin and therefore cannot enforce it during a mixed-version rollout.

## Consequences

- Apps gain an opaque-key attempt-rate primitive with no Redis or domain-specific Core extension.
- A failed or cancelled attempt still consumes budget, preventing retry storms from bypassing the limit.
- Local and PostgreSQL Stores use different physical clock sources but preserve the same externally observable fixed-window contract.
- Fixed-window boundary bursts are accepted and documented; callers needing smoother traffic must pace submissions or wait for a future explicit contract.
- Global quotas across independent Core Cells remain outside this primitive.
