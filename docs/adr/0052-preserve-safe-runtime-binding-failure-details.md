# ADR 0052: Preserve safe runtime-binding failure details

- Status: Accepted
- Date: 2026-08-19
- Amends: [ADR 0034](0034-bind-worker-local-capability-gateways.md)

## Context

ADR 0034 lets a Worker open a Job-scoped run on a neutral capability gateway before starting an App. A failed run-open request previously became the same `RuntimeBindingError` as every other binding failure. The public result retained only the generic `name` and `message`; it discarded whether the failure happened while opening the capability run, which safe gateway reason applied, and whether retrying later might succeed.

Core must not persist a raw gateway response, provider message, URL, credential, resource identifier, or other free-form detail. It also must not add provider policy or an automatic retry loop to the Worker. A newer gateway and an older Core or the reverse must continue to interoperate.

## Decision

### Bounded gateway error envelope

For a non-`201` response to `POST /v1/runs`, a gateway may return this additive JSON envelope:

```json
{
  "error": "capability_unavailable",
  "phase": "provider_selection",
  "reason": "capacity_unavailable",
  "retryable": true
}
```

`error` is the legacy reason field. `phase` and `reason` are optional lower-case snake-case machine codes of at most 64 bytes. `retryable` is an optional boolean hint. Core reads at most 4 KiB, ignores unknown JSON fields, and never includes the raw body or decoder error in the returned error, persisted result, event, or log.

If `phase` is absent or invalid, Core uses `capability_run_open`. If `reason` is absent or invalid, Core accepts a valid legacy `error`; otherwise it uses `gateway_rejected`. Empty, non-JSON, oversized, or unreadable bodies use those fallbacks. Transport failures use `gateway_timeout` or `gateway_unreachable`, while a canceled binding context uses `binding_canceled`. Without an explicit gateway hint, HTTP 408, 425, 429, and 5xx responses are retryable; other statuses and cancellation are not.

The `201` success response remains the strict `runRef`, `runToken`, and `expiresInSeconds` schema. Error-envelope fields on a successful response remain invalid, so the credential boundary does not broaden accidentally.

### Additive public execution error

The existing public failure code, message, exit code, and Job error remain unchanged:

```json
{
  "name": "RuntimeBindingError",
  "message": "could not apply runtime bindings",
  "phase": "capability_run_open",
  "reason": "capacity_unavailable",
  "retryable": true
}
```

`phase`, `reason`, and `retryable` are additive fields on the existing error result. Older consumers can continue to use only `name` and `message`. Local and remote Workers persist and return the same JSON through Job and canonical Run result APIs. The Invocation OpenAPI includes the generic execution-error alternative in addition to each App's declared success output.

An unclassified binding failure uses `phase=runtime_binding`, `reason=binding_failed`, and `retryable=false`. `retryable` is descriptive only: it does not requeue the Job, refund the Attempt, bypass lease fencing, or admit a new Run. Retry policy remains a caller or higher-level workflow decision.

### Persistence and protocol compatibility

No database migration or Worker Plane version field is required. The failure metadata is part of the existing JSON result payload, which Local state, PostgreSQL `runs.result`, and remote Worker completion already transport. New Core accepts legacy `error`-only gateway responses, and old Core safely ignores the body of a new non-`201` response. New fields must never be added to the successful run credential response.

## Consequences

- Operators and callers can distinguish a retryable capability allocation failure from a generic binding defect without exposing gateway internals.
- Core remains neutral about providers, capacity policy, artifacts, and external resource identity.
- The existing `RuntimeBindingError` contract and terminal Job semantics remain backward compatible.
- A future generic top-level `JobResult.failure` would require a separate decision because current Job and Invocation result APIs intentionally return the Action or execution-error JSON value rather than the complete internal `JobResult`.

## Rejected alternatives

- **Persist the raw gateway body or message.** Rejected because a gateway response may contain credentials, endpoints, resource identifiers, or provider-native details.
- **Add provider-specific error enums to Core.** Rejected because the gateway owns provider classification and Core only carries bounded opaque codes.
- **Automatically retry a retryable failure inside the Worker.** Rejected because the Job Attempt has already been claimed and lease-fenced; changing attempt accounting or requeue semantics needs a separate execution decision.
- **Add a top-level `JobResult.failure` in this change.** Rejected for the minimal compatible slice because existing public result handlers return `JobResult.Output`; a new top-level field would require a wider response-envelope migration.
