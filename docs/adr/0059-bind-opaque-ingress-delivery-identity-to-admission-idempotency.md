# ADR 0059: Bind the opaque ingress delivery identity to Admission idempotency

## Status

Accepted

## Context

The default-unmounted opaque HTTP ingress handler turned one trusted envelope into one AdmissionService call without an idempotency identity. Every delivery therefore created a new Run, and the boundary had no safe way to recover from a lost response.

That is the problem a trusted boundary cannot solve alone. A missing HTTP response does not prove that Admission did not commit, so an isolated ingress that redelivers after a timeout, a disconnect, or its own restart would create a second Run for the same external request. Suppressing redelivery instead is not an option: it converts every transport failure into a silently dropped invocation.

The envelope already carried the routing identity and immutable references the Resolver needs, but nothing identified the delivery itself. AdmissionService, meanwhile, already had the machinery: a principal-scoped idempotency key derives a deterministic Run ID, a replay returns the committed Run, and a request fingerprint mismatch is a conflict. Only the opaque ingress path did not use it.

An idempotency identity supplied by an external caller would be a spoofing surface: two callers could then collide deliberately, or one could alias another's Run.

## Decision

`trustedIngress.deliveryId` is a required envelope field. It is the identity the isolated ingress boundary assigns to one delivery, and it is trusted for the same reason the rest of `trustedIngress` is: the boundary constructs the envelope, and the handler never reads the identity from a caller header, path, or body.

The handler derives the Admission idempotency key from the delivery identity bound to the exact trusted tuple — kind, issuer, audience, publication reference, route generation, and credential snapshot — and passes only that derived key to AdmissionService, which adds the principal scope. The same identity presented for another publication, route generation, or credential snapshot is therefore a different admission identity, and no cross-route aliasing is possible even when one Service Principal serves several routes.

The existing Admission semantics then apply unchanged:

- the same delivery with the same payload resolves to the same Run, and concurrent identical deliveries converge on one Run and one first Job;
- the same delivery identity with a different payload is a conflict, answered as `409` with an `applicationProtocolViolation` platform failure and no second Run;
- an admitted Run stays queryable after a wait timeout or a disconnected caller, and redelivering the same identity returns it.

Core does not retry a delivery itself, and the contract states that an intermediary must not automatically retry this `POST`. Reconciliation is a deliberate redelivery of the same delivery identity.

The raw identity never reaches durable state, a response, a log, or the App input. Core stores only the digest AdmissionService already recorded, and the App input remains the validated `http` and `body` values.

## Consequences

- A private gateway can recover from a lost response without guessing whether the Run committed, and without a second Run.
- The envelope is stricter: a delivery without a usable identity is rejected before the Resolver runs, so a boundary that does not assign one cannot admit at all. This is a breaking change to the pre-1.0 trusted envelope contract; the handler remains default-unmounted, so no deployed route is affected.
- The delivery identity is bounded like the other trusted strings: non-empty, no surrounding whitespace, no control characters, at most 200 bytes.
- Post-Admission failures stay non-retryable in the platform-failure envelope. The Run already exists; the boundary reconciles by redelivering the same identity rather than by retrying blindly.
- Admission gains no provider-specific field. The identity is an ordinary idempotency key by the time it reaches AdmissionService, so the canonical Run, built-in Webhook, and opaque ingress paths keep one idempotency implementation.

## Alternatives

| Alternative | Why not |
| --- | --- |
| Derive the key from the request bytes | Two deliberate identical requests would collapse into one Run, which the caller never asked for; and a byte-level key cannot express "this is the same delivery" across a payload the boundary re-encodes. |
| Accept a caller-supplied idempotency header | An external caller could alias or collide with another caller's Run. The isolated boundary is the only party whose identity assignment is trustworthy here. |
| Make the field optional | A boundary that omitted it would silently lose idempotency, which is the failure this decision exists to prevent. |
| Add a provider-specific delivery field to Admission | The engine would grow adapter vocabulary. The derived key keeps one neutral idempotency contract for every admission path. |

## References

- [ADR 0055](0055-add-default-unmounted-opaque-http-ingress-conformance.md) — the default-unmounted conformance handler and its production gates.
- [ADR 0056](0056-persist-opaque-http-projections-and-resolve-atomically.md) — the atomic Resolver this path admits through.
- [ADR 0057](0057-expose-authenticated-admission-probes-and-atomic-outcomes.md) — authenticated admission probes and terminal outcomes for the canonical Run and built-in Webhook paths.
- [Opaque HTTP ingress conformance](../concepts/opaque-http-ingress.md) — the current description of the boundary.
