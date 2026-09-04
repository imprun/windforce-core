---
title: Opaque HTTP ingress conformance
description: The default-unmounted trusted HTTP-to-Admission boundary for byte-exact App protocols.
---

The opaque HTTP ingress conformance component is an internal `http.Handler` that turns one trusted private-gateway envelope into one normal AdmissionService call. It is provider-neutral and is not a Webhook Trigger, public Invocation endpoint, listener, or route manager.

The component is inactive by default. Core does not mount it on the primary listener and does not open another port.

```mermaid
flowchart LR
    GW["Trusted private gateway"] --> ENV["OpaqueHTTPInvocationV1"]
    ENV --> VALIDATE["Strict envelope and byte validation"]
    VALIDATE --> RESOLVE["Atomic publication, route generation, and credential snapshot Resolver"]
    RESOLVE --> ADMISSION["AdmissionService with active Release precondition"]
    ADMISSION --> APP["App receives opaque HTTP App input"]
    APP --> WIRE["Application wire response"]
    WIRE --> BYTES["Exact status, content type, and decoded bytes"]
```

## Trusted request boundary

The outer request must be `POST` with an `application/json` media type. The JSON body is `windforce.opaque-http-ingress-request/v1` and has exactly these top-level fields:

- `kind`
- `trustedIngress`
- `http`
- `body`
- `receivedAt`
- `deadlineAt`

`trustedIngress` carries only immutable references, routing identity, and the delivery identity: issuer, audience, publication reference, route generation, credential reference, and `deliveryId`. It never carries a raw API key, token, password, encryption key, or decrypted provider payload.

`deliveryId` is the identity the isolated ingress boundary assigns to one delivery. It is trusted boundary input: an external caller cannot supply or influence it, because the handler reads it from the trusted envelope the boundary itself constructs and never from a caller header, path, or body.

`body.data` is canonical RFC 4648 Base64. `body.byteLength` is the decoded byte count. `body.digest` is lowercase `sha256:` plus the digest of the decoded bytes. The handler checks all three values before resolution or Admission.

`http.exactEscapedPath` is an ASCII canonical escaped path. It starts with exactly one slash and rejects empty segments, trailing slashes except `/`, dot segments, wildcards, query or fragment delimiters, backslashes, lowercase or malformed escapes, encoded dot/slash/backslash/percent, and unnecessary percent encoding.

## Atomic resolution and Release fencing

The Resolver owns one atomic read of route publication and credential state. It receives a body-blind view containing trusted ingress references, HTTP method/path/content type, and decoded body length. Base64 data, decoded bytes, body digest, and caller-controlled timestamps remain outside the Resolver boundary. The Resolver context hides the envelope deadline value while retaining cancellation. For the supplied issuer, audience, publication reference, route generation, and credential reference, it either returns a fully pinned Admission request or returns a generic platform failure.

The Resolver result contains only:

- workspace, App, and Action;
- an exact Service Principal with `runs:create`, `runs:read:own`, and one allowed App/Action target;
- the expected active Deployment ID when available, commit, and bundle digest;
- provider-neutral immutable invocation pins for the publication, route generation, operation, credential, and other resolved control-plane references;
- the publication-specific response content types and maximum response bytes.

The handler supplies the input and adapter itself and marks the exact wrapper as already resolved. AdmissionService permits this mode only for a Service Principal, bypasses App/Action/client InputConfig overlays, rechecks the active Release precondition, and validates the Action input before it creates a Run. The immutable invocation pins and resolved response policy are preserved as Job metadata for worker and audit use. They are not copied into the App input. `InvocationPins` are unsigned metadata, not a capability or downstream authorization proof. Route, credential, or Release mismatches therefore create zero Runs.

## App input and result

The App always receives `windforce.opaque-http-app-input/v1`. It contains only the validated `http` and `body` values from the trusted request. An Action used by this boundary must publish the matching schema from `contracts/opaque-http/v1/opaque-http-app-input.schema.json` as its materialized `inputSchema`.

Admission validates this wire wrapper, not a decoded domain object. Decoding, decryption, defaults, and domain input validation belong inside the App or its Application SDK.

The App returns `windforce.application-wire-response/v1`. The handler accepts only one optional `content-type` header, a status from 200 through 599, and the same strict Base64/length/digest body metadata. A supplied content type must be in the resolved publication policy, a missing content type is accepted only when that policy permits it, and the decoded body must fit the resolved route limit and the 7 MiB handler-wide response ceiling. This ceiling leaves room for padded Base64 and the completion envelope under the Worker Plane's 10 MiB request limit. Status 204 or 304 cannot carry a non-empty body. The handler writes the decoded bytes exactly; it does not parse or re-encode them.

The trusted request deadline is applied to Resolver, Admission, and Run polling calls. Deadline expiry stops the synchronous wait but does not cancel a Run that Admission already created.

For every terminal Run, the handler first attempts to validate and restore an application wire response, including an App error status. Only when execution has no valid application wire response does it return a stable JSON `windforce.execution-outcome/v1` `platformFailed` envelope. An expired Run or lease-loss/Worker-shutdown interruption is then classified as `workerLost`; other terminal failures and post-Admission consistency faults stay generic. Post-Admission failures are reported as non-retryable: the Run already exists, so the boundary reconciles it by redelivering the same delivery identity rather than by retrying blindly. Failure categories are provider-neutral and do not expose Resolver or App details.

## Delivery identity and replay

Admission on this path is idempotent per delivery. The handler derives the Admission idempotency key from `deliveryId` bound to the exact trusted tuple — issuer, audience, publication reference, route generation, and credential snapshot — so the same identity presented for another route or credential is a different admission identity. AdmissionService then adds the principal scope. The raw identity is never stored, echoed in a response, written to a log, or copied into the App input; durable state keeps only a digest.

- The same delivery with the same payload resolves to the same Run. Concurrent identical deliveries converge on one Run and one first Job; the losers of the creation race read the committed Run back and replay it.
- The same delivery identity with a different payload is a conflict. The handler answers `409` with an `applicationProtocolViolation` platform failure and creates no second Run.
- Core does not retry a delivery on its own, and an intermediary must not automatically retry this `POST`. A missing response is not evidence that Admission did not commit. The boundary reconciles by redelivering the same delivery identity, which replays the committed Run instead of creating another one.
- A wait timeout or a disconnected caller does not cancel an admitted Run. The Run stays queryable, and redelivering the same identity returns it.

## Execution attestation

An App admitted through this boundary may need to call a private downstream capability service that holds material the App must not hold itself. The invocation pins cannot authorize that call: they are unsigned Job metadata, so they identify an execution without proving one.

When a deployment configures an issuer, Admission mints a signed execution attestation for every Run it admits from resolved invocation pins and stores it in the Job payload beside them. It binds exactly what Core pinned — Run reference, workspace, App and Action, publication reference and route generation, operation reference, credential snapshot reference, and the pinned Release — plus the issuer's audience, key id, and expiry. Values Core does not interpret travel in `references` as named immutable pins, verbatim from the projection.

The attestation is host-private: it never becomes an HTTP header, a public API response, a Run outcome, or an event payload, and the public job status omits it exactly as it omits the pins. Without a configured issuer nothing is minted and Runs are admitted unchanged.

[ADR 0060](../adr/0060-mint-audience-bound-execution-attestations-after-admission.md) records the decision; [`contracts/execution-attestation/v1`](../../contracts/execution-attestation/v1/README.md) holds the schemas, the canonical byte rules, and the synthetic fixture.

## Isolated listener

The handler is served on its own listener, separate from Core's primary API ([ADR 0062](../adr/0062-serve-the-opaque-ingress-on-its-own-listener.md)). `-opaque-ingress-addr`, or `WINDFORCE_CORE_OPAQUE_INGRESS_ADDR`, mounts it; with no address Core does not open it.

That listener serves exactly two paths:

| Path | Method | Purpose |
| --- | --- | --- |
| `/ingress/opaque-http/v1` | POST | admits one trusted envelope |
| `/readyz` | GET, HEAD | reports whether this boundary is serving |

Every other path answers 404 with an `applicationProtocolViolation` platform failure, and Core's primary listener does not serve the ingress path at all.

Readiness reports ready once the listener is bound with its resolver and Admission wired, and unready from the moment a drain starts, so a gateway stops delivering into a listener that is going away. It does not probe the projection store: storage faults surface as platform failures on the delivery itself.

The listener admits a bounded number of concurrent deliveries, 64 by default with a ceiling of 4096, because each one holds a synchronous wait on a Run. A delivery waits at most 50 milliseconds for a slot, with a ceiling of one second, before the listener answers 503 with a retryable `capacityUnavailable`. Both bounds are flags: `-opaque-ingress-max-concurrent` and `-opaque-ingress-acquire-wait`. Byte limits, the synchronous wait and the poll interval are configured alongside them.

Core fails closed on this path. An unbindable address, an unusable limit, or an unusable execution attestation key stops the process before it serves anything, and a listener that dies while the process runs drains the primary listener and exits non-zero.

The listener terminates no TLS and verifies no certificate. Which mechanism proves the peer is a deployment property ([ADR 0061](../adr/0061-accept-any-authenticated-peer-boundary-for-opaque-ingress.md)), and Core neither implements nor detects it.

## Remaining activation gates

Mounting the listener does not by itself make the path production-ready. Activation is gated on:

1. A boundary in front of that listener which only authenticated allowed peers can reach, with issuer and audience asserted by the boundary rather than accepted from an untrusted caller. The mechanism is a deployment choice: mutually authenticated TLS with the peer identity taken from the verified client certificate, or a network boundary whose listener carries no other traffic and whose bidirectional policy allowlists the calling namespace, pod, and port. A network-boundary deployment has no per-request cryptographic peer proof, so its activation evidence must show that the network layer actually enforces the allowlist and that the listener is unreachable from anywhere else ([ADR 0061](../adr/0061-accept-any-authenticated-peer-boundary-for-opaque-ingress.md)).
2. [Issue #283](https://github.com/imprun/windforce-core/issues/283): a durable publication/projection lifecycle and atomic Resolver with immutable revisions, monotonic generation, credential snapshot status, audit, rollback, and stale-reference rejection.
3. A configured execution attestation issuer, if an App on this boundary calls a private downstream capability service. Unsigned `InvocationPins` must not be used for that purpose. The key file, key id, audience and lifetime are configured with the listener, and a partial configuration stops the process rather than falling back to unsigned pins.
4. A deadline and cancellation policy. A wait timeout or disconnected caller does not cancel a Run that Admission already created.
5. Deployment-specific byte limits, concurrency bounds, metrics, traces, and failure-rate alerts. The defaults are a starting point, not a capacity model.

Public gateway authentication, TLS termination, route publication, provider request/response codecs, and secrets remain outside Core's opaque handler.

## Contracts

The JSON Schemas and synthetic byte fixtures are in [`contracts/opaque-http/v1`](../../contracts/opaque-http/v1/README.md). [ADR 0055](../adr/0055-add-default-unmounted-opaque-http-ingress-conformance.md) records the decision and production gates. [Run admission architecture](run-admission.md) defines the shared AdmissionService boundary.
