# ADR 0055: Add a default-unmounted opaque HTTP ingress conformance handler

- Status: Accepted
- Date: 2026-08-30
- Issue: [#280](https://github.com/imprun/windforce-core/issues/280)

## Context

A trusted private gateway may need to synchronously execute an App Action without translating an opaque HTTP body into a provider-specific request inside Core. Calling the public Invocation API would add an avoidable network hop when the adapter and AdmissionService are in the same process, while using a Webhook Trigger would assign stored source lifecycle semantics that the invocation does not have.

This path must not create a second admission implementation. It must also prevent a route or credential snapshot from being resolved to one Release and admitted against another Release after a concurrent publication change. The boundary carries arbitrary bytes, so JSON re-encoding, permissive Base64 decoding, unbounded waits, or application-defined response headers would break the protocol or widen the trust boundary.

## Decision

Core provides `internal/opaquehttp.Handler` as an opt-in conformance component. Constructing it does not mount a route, open a listener, or change the primary Core server. A production host must make a separate activation decision.

The handler accepts only `POST` with an `application/json` outer media type. Its trusted internal request is `windforce.opaque-http-ingress-request/v1`. The request contains an issuer and audience, a publication reference and route generation, an immutable credential reference, canonical HTTP metadata, RFC 4648 Base64 body metadata, and received/deadline timestamps. Raw credentials and secrets are not part of this envelope.

Before consulting a Resolver or AdmissionService, the handler rejects unknown or duplicate JSON members, invalid method/media/path values, expired or excessive deadlines, non-canonical RFC 4648 Base64, decoded length or SHA-256 mismatches, and configured byte-limit violations. These failures create no Run.

The injected Resolver is one atomic trust decision. It must verify the issuer and audience; read one consistent publication, route-generation, and credential-snapshot view; confirm that every referenced snapshot is active; and return a target plus an exact workspace-scoped Service Principal. It also returns an `ActiveReleasePrecondition` containing the selected Deployment ID when available, commit, and execution bundle digest. The Resolver cannot set input, adapter, trigger, idempotency, environment, schedule, or client fields.

AdmissionService compares that precondition with the active Release immediately before reading the selected Action. A mismatch is a routing conflict and creates no Run. This second check closes the publication race between resolution and admission while preserving AdmissionService as the only Run-plus-Job creation implementation.

The handler submits exactly this App input:

```json
{
  "kind": "windforce.opaque-http-app-input/v1",
  "http": {
    "method": "POST",
    "exactEscapedPath": "/synthetic/bytes",
    "contentType": "application/octet-stream"
  },
  "body": {
    "encoding": "RFC4648-BASE64",
    "data": "AAECf4D/QUJDCg==",
    "byteLength": 10,
    "digest": "sha256:1b07ff65446a1e3d40ea19fffed12722cfc3762a0bc8f70ace978c13b1949ad1"
  }
}
```

The selected Action must publish an `inputSchema` for this exact wrapper. AdmissionService validates the wrapper before creating the Job. Domain-shaped validation and any decoding or decryption happen inside the App boundary.

The handler waits only until the trusted request deadline and the configured maximum. It does not cancel an admitted Run when the caller disconnects or the wait expires. A successful App must return `windforce.application-wire-response/v1`; the handler verifies its status, single allowlisted `content-type` header, strict Base64 body, decoded size, length, and digest before writing the exact decoded bytes. Core's generic completion path does not validate Action output against `outputSchema`, so this conformance handler performs the wire-response validation itself. A missing or invalid wire response becomes `windforce.execution-outcome/v1` with `outcome: "platformFailed"`.

`windforce.app-manifest/v2` `publicInterfaces` declarations remain opaque discovery metadata. The handler and Resolver do not select routes or interpret behavior from declaration members.

## Consequences

The conformance component removes an in-process network hop without adding a new listener or a parallel admission path. Pre-admission protocol and route-snapshot failures create no Run, and Admission pins the same Release chosen by the Resolver. Request and response bytes survive the App boundary without UTF-8 or JSON assumptions.

The current component is not production ingress. Production activation requires all of the following:

- a separately configured private listener protected by mutually authenticated TLS and an authenticated issuer/audience binding;
- an atomic, durable Resolver backed by managed route publications and immutable credential snapshots, including monotonic generation, audit, rollback, and stale-publication rejection;
- a retry and idempotency contract, plus an explicit policy for canceling or retaining an admitted Run after client disconnect or deadline expiry;
- operational limits, telemetry, and overload behavior for the selected deployment environment.

Public DNS, public TLS termination, gateway route reconciliation, external credential verification, provider codecs, and secret material remain outside this handler.

## Rejected alternatives

- **Mount the handler on the primary listener.** Rejected because the envelope is trusted internal input and has no public authentication lifecycle.
- **Use the Webhook Trigger path.** Rejected because this invocation has no stored Trigger resource or source-delivery lifecycle.
- **Call the Invocation API over loopback.** Rejected because the handler can invoke the same AdmissionService in-process without another network hop.
- **Let the Resolver return arbitrary CreateRun fields.** Rejected because it would let routing code smuggle invocation policy past the conformance boundary.
- **Interpret `publicInterfaces` inside Core.** Rejected because Core owns only opaque preservation and generic execution contracts.
