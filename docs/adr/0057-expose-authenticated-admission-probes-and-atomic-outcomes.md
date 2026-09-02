# ADR 0057: Expose authenticated admission probes and atomic outcomes

## Status

Accepted

## Context

An external Gateway may need to apply environment policy before it lets Core create a Run. Applying that policy before Core authenticates a bearer credential or Webhook signature leaks policy state to unauthenticated callers and lets invalid callers consume temporary reservations. Applying it only after the ordinary invocation response cannot prevent an over-limit Run.

Transport failure creates a second problem. A missing HTTP response does not prove that Core did not commit the Run. A later `GET` returning not found is also not terminal evidence because an in-flight creation transaction can still commit after that read.

Environment-specific admission policy belongs to the external policy system, while the authentication boundary and authoritative Run outcome belong to Core. The contract therefore needs a neutral coordination primitive that is useful to a self-hosted reverse proxy or policy gateway without embedding policy-specific vocabulary.

## Decision

Core supports an authenticated, non-mutating admission probe on canonical Run and built-in Webhook ingress requests. The caller sends the exact request with `X-WF-Admission-Probe: true` and a stable idempotency identity. Core verifies the credential or Webhook signature, normalizes the target and request, and returns:

- an opaque `admission_id`;
- a canonical `request_fingerprint` that contains no raw credential, signature, idempotency key, or request body;
- the current outcome `ready`, `admitted`, or `aborted`;
- the authoritative Run ID only when already admitted.

A probe creates no Run, Job, or TriggerDelivery. It is not an admission approval: ordinary admission re-evaluates lifecycle, release, schema, runtime configuration, execution limits, and current principal policy.

Core persists only terminal outcomes. Run creation and the `admitted` outcome are written in the same State Store transaction. Terminal outcomes are retained independently from Run, Job, log, event, and human-task retention; after a Run is pruned, its admitted outcome remains a tombstone that prevents the identity from being reused or later resolved as aborted. An administrator may atomically resolve an ambiguous admission through `POST /api/w/{workspace}/admission-outcomes/{admission_id}/resolve` with the exact request fingerprint. If the Run already exists, the operation returns `admitted`; otherwise it writes `aborted`. Both resolution and Run creation use the same per-identity lock. Once either terminal state wins, every later creation for that identity fails deterministically.

Absence means `unknown`; it is never safe negative evidence. Only a successful atomic resolve returning `aborted` proves that an external reservation may be released. Terminal outcomes never change state, and a fingerprint mismatch is a conflict.

The built-in Webhook success contract also returns the same `Location`, `X-WF-Run-Id`, `X-WF-Run-State`, and `X-WF-Idempotency-Reused` signals as canonical Run admission. Synchronous Webhook wait preserves the actual replay flag.

## Security boundary

The probe request is authenticated by the same Cell-owned credential or source signature as the actual request. The administrator-only outcome endpoint does not accept caller-owned bearer or Webhook credentials as authority. Responses expose no principal identifier, credential digest, raw header, body, signature, provider error, or idempotency key. The State Store retains the resolving administrator actor for audit, but the outcome API omits it.

An external policy system may store the opaque admission ID and fingerprint. It must not reproduce Core's principal-scoping or fingerprint algorithms, treat a probe as permission to create a Run, or release an ambiguous reservation from a non-authoritative not-found observation.

## Consequences

- External policy can run after Cell authentication and before Run creation without becoming a second AdmissionService.
- A process crash, timeout, or missing response can be reconciled without guessing whether the Run committed.
- Idempotency is required for the probed path because a stable admission identity is necessary for fencing and reconciliation.
- Local and PostgreSQL State Stores carry the same terminal outcome semantics.
- The protocol adds one preflight request for policy-governed invocation. Callers that do not need external policy continue using the ordinary API unchanged.
- Environment-specific admission rules and their business semantics remain outside Core.
