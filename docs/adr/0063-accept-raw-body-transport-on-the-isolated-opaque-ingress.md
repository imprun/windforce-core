# ADR 0063: Accept raw-body transport on the isolated opaque HTTP ingress

- Status: Accepted — maintainer approved merge on 2026-09-09; production activation of the opt-in transport remains a separate deployment decision.
- Date: 2026-09-09
- Issue: [#296](https://github.com/imprun/windforce-core/issues/296)
- Amends: [ADR 0055](0055-add-default-unmounted-opaque-http-ingress-conformance.md) and [ADR 0059](0059-bind-opaque-ingress-delivery-identity-to-admission-idempotency.md) for transport, not trust or Admission semantics.

## Context

The opaque HTTP ingress accepts a JSON envelope whose body is Base64 plus decoded length and SHA-256 metadata. A trusted boundary must construct that envelope, but the contract never required a separate envelope-building service. Some gateways can supply verified routing metadata and forward bytes without having a general body-transformation facility. Requiring those deployments to install a buffering transformation plugin or an additional service only to construct Core's envelope adds an avoidable hop and another byte-handling implementation.

The trusted peer requirement does not depend on the body encoding. The existing unkeyed body digest detects disagreement between envelope metadata and bytes; it neither authenticates the peer nor proves which bytes arrived at a public gateway before an intermediary transformed them. A raw transport can preserve the bytes received by Core and share the existing execution contract without weakening the isolated-listener boundary.

## Decision

### Explicit startup transport, unchanged listener surface

`--opaque-ingress-transport` (or `WINDFORCE_CORE_OPAQUE_INGRESS_TRANSPORT`) selects `envelope-v1` (default) or `raw-body-v1` for the isolated listener. When an ingress address enables the listener, any other value fails startup; settings for an unmounted listener are ignored. Choosing a transport does not mount the listener: an explicit ingress address remains necessary. This is one mode per listener, not automatic detection, a per-request switch, or a new public endpoint.

Both transports use exactly `POST /ingress/opaque-http/v1`; `/readyz` remains the only other path, with its existing GET/HEAD readiness behavior. No arbitrary original request path or method becomes routable on this listener. No read path, authorization endpoint, or primary-listener mount is added.

### Raw wire contract

`raw-body-v1` requires exactly one outer `Content-Type: application/octet-stream` without parameters and exactly one `X-Windforce-Opaque-Context` header. That header is at most 16 KiB of printable ASCII containing one JSON object with exactly:

- `kind: "windforce.opaque-http-ingress-context/v1"`;
- `trustedIngress`;
- `http`;
- `receivedAt`;
- `deadlineAt`.

The nested field definitions and timestamp validation are the same as the existing invocation envelope. The context has no `body` member. Serialize it on one line, using JSON escapes when needed; duplicate or unknown JSON members, repeated context headers, and unknown `X-Windforce-Opaque-*` headers are rejected before resolution. Ordinary HTTP transport headers are not a source of trusted values. The context header is forbidden in `envelope-v1`; neither mode guesses another encoding from a body or falls back after a parse failure.

`http` describes the original method, canonical escaped path, and content type. The outer request still has the fixed ingress method, path, and transport content type. The existing method, media-type, and canonical path rules apply unchanged, including the prohibition on query and fragment delimiters. Raw transport does not introduce transparent forwarding of arbitrary public HTTP semantics.

The HTTP body contains the original application bytes as observed at Core. The handler does not decompress, interpret text, parse JSON, or transform those bytes. Any `Content-Encoding` or trailer declaration, including actual received trailers, is rejected. HTTP transfer framing is not application data. Core bounds the bytes it reads and constructs canonical Base64, decoded byte length, and SHA-256 metadata from those exact bytes. It then enters the existing invocation validation, body-blind Resolver, AdmissionService, App input, and response path. An empty body is valid transport input; only the pinned Action decides whether its content is meaningful.

The context is validated before the body is read. The trusted deadline bounds body reading as well as resolution, Admission, and synchronous waiting, and the handler rejects a deadline exhausted while reading before consulting the Resolver. The raw transport requires a server response writer that supports setting the request read deadline; an embedding wrapper must expose the underlying writer through `Unwrap`. Lack of deadline support is an error rather than an unbounded-read fallback. Existing concurrency, byte, wait, disconnect, and drain limits remain applicable.

### One trusted boundary and one delivery identity

The authenticated allowed peer remains the only authority for the context. The boundary must strip all externally supplied reserved ingress headers and assert the context from its own verified state; copying an arbitrary caller header is not authentication. The header must contain references, never bearer tokens, key material, or provider payload. Core does not log or persist the raw header, and forwarding/access-log configuration must keep it out of logs. [ADR 0061](0061-accept-any-authenticated-peer-boundary-for-opaque-ingress.md) network or mutual-TLS activation evidence is unchanged.

`issuer` and `audience` are stable deployment-chosen names, not necessarily URLs or OIDC values. For the store-backed Resolver they are 1–160 printable ASCII characters without surrounding whitespace and must exactly match both publication and credential projections. These labels assert the already-proven boundary; their syntax does not prove identity.

`trustedIngress.credentialRef` and `trustedIngress.deliveryId` remain required. A synthetic delivery does not gain a credential-less bypass. The boundary continues to assign delivery identities under [ADR 0059](0059-bind-opaque-ingress-delivery-identity-to-admission-idempotency.md); recovering a lost response is deliberate replay of the same logical identity, not blind intermediary POST retries.

The raw context kind is a wire discriminator only. Before deriving the Admission idempotency key, Core normalizes raw input to the existing `windforce.opaque-http-ingress-request/v1` invocation kind. Equivalent trusted tuples and bytes therefore retain the same principal-scoped delivery identity across a transport migration and do not create a second Run. A different payload under the same identity remains a `409` `windforce.execution-outcome/v1` platform failure with category `applicationProtocolViolation`; an App's normal `409` result is not an Admission conflict. Timestamps remain outside the delivery key and payload fingerprint as before, allowing a deliberate replay to provide a usable new wait deadline without changing its identity. Current projection and Release checks still apply to replay.

## Consequences

- Existing `envelope-v1` clients and exact App input/response schemas remain compatible. Core does not require a new SDK, App manifest, database migration, or worker behavior.
- A boundary that can forward bytes and produce the bounded trusted context needs no body-transform service. It still owns original HTTP metadata capture, timestamp and identity assignment, verified projection references, reserved-header sanitation, and authenticated reachability.
- This provides a neutral transport, not a general-purpose gateway envelope builder or a provider-specific configuration generator. No such separately shipped builder is promised by this decision.
- Raw mode proves byte preservation from Core's receiving boundary onward. Deployments requiring evidence of bytes before any upstream transformations need a separate authenticated integrity contract; Core must not describe its computed digest as that proof.
- Header size limits apply at every intermediary. An operator must configure the allowed peer path to carry the bounded context without truncation or joining and prove duplicate/spoofed contexts are rejected.

## Acceptance criteria

- Default envelope mode and configured raw mode retain the exact two-path isolation, and invalid startup mode selection fails closed when the listener is enabled. Settings for an unmounted listener do not activate it.
- A non-text raw body including NUL and invalid UTF-8 produces the exact same App input as the equivalent envelope; empty and maximum permitted bodies have explicit tests.
- Wrong mode/media type, duplicate or missing context, unknown reserved headers, duplicate/unknown JSON members, oversized or non-ASCII header bytes, content encodings, trailers, and noncanonical metadata create zero Runs.
- A stalled body reader is bounded by the trusted deadline; a deadline exhausted during reading never reaches the Resolver. Limits reject oversized bodies even without `Content-Length`.
- Resolver requests remain body-blind, projection/Release fencing is unchanged, and no trusted context or raw delivery identity leaks into App input, responses, durable payloads, or logs.
- Equivalent envelope/raw requests and concurrent identical deliveries converge on one Run; changing bytes under the same identity conflicts without creating a second Run. Existing App error-response restoration remains unchanged.
- Published context schema and fixture agree with runtime validation and reuse the invocation contract's nested constraints.

## Alternatives

| Alternative | Why not |
| --- | --- |
| Require a separate envelope-builder service | The deployment may need only header construction and byte forwarding; adding another hop is not an execution requirement. Existing envelope clients remain supported. |
| Automatically select a mode from headers or body | Ambiguous fallback creates a parser/trust-confusion surface and an implicit migration. Startup selection is explicit and observable. |
| Route arbitrary original methods and paths on the isolated listener | Expands the exact two-path activation surface and confuses transport routing with the published route metadata. |
| Accept raw data with caller-provided authorization headers on the primary listener | Removes the isolation that makes the trusted context admissible and would create a separate authentication/admission path. |
| Treat a SHA-256 checksum as request authentication | Anyone who can replace bytes can recompute an unkeyed checksum. Peer authentication and projection validation supply trust. |

## References

- [ADR 0055](0055-add-default-unmounted-opaque-http-ingress-conformance.md) — existing strict envelope and shared Admission path.
- [ADR 0059](0059-bind-opaque-ingress-delivery-identity-to-admission-idempotency.md) — boundary-owned delivery identity and replay.
- [ADR 0061](0061-accept-any-authenticated-peer-boundary-for-opaque-ingress.md) and [ADR 0062](0062-serve-the-opaque-ingress-on-its-own-listener.md) — authenticated peer and exact isolated listener.
- [Opaque HTTP contracts](../../contracts/opaque-http/v1/README.md) — schemas, fixtures, and wire instructions.
