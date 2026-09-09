# Opaque HTTP v1 contracts

This directory contains the provider-neutral machine contracts for Core's default-unmounted opaque HTTP ingress conformance handler.

| File | Purpose |
| --- | --- |
| `opaque-http-invocation.schema.json` | Trusted internal gateway-to-handler envelope |
| `opaque-http-ingress-context.schema.json` | Trusted context header object for opt-in raw-body transport; reuses invocation nested definitions |
| `opaque-http-app-input.schema.json` | Exact input admitted to the pinned App Action |
| `application-wire-response.schema.json` | Exact JSON result the App returns to the handler |
| `execution-outcome.schema.json` | Stable completed or provider-neutral platform-failure representation |
| `opaque-http-invocation.example.json` | Synthetic valid trusted envelope with non-text bytes |
| `opaque-http-ingress-context.example.json` | Equivalent raw-transport context; compact onto one line before setting the header |
| `opaque-http-app-input.example.json` | Expected App input derived from the valid envelope |
| `application-wire-response.example.json` | Synthetic valid App response with non-text bytes |
| `platform-failed.example.json` | Stable provider-neutral platform-failure result |
| `opaque-http-invocation.invalid-length.example.json` | Negative fixture whose decoded length does not match `byteLength` |

The examples contain no credential material, customer identifier, provider field, or real target data.

## Byte metadata

Every body uses:

```json
{
  "encoding": "RFC4648-BASE64",
  "data": "AAECf4D/QUJDCg==",
  "byteLength": 10,
  "digest": "sha256:1b07ff65446a1e3d40ea19fffed12722cfc3762a0bc8f70ace978c13b1949ad1"
}
```

`data` must be canonical padded RFC 4648 Base64. `byteLength` and `digest` are computed over the decoded bytes, never over the Base64 text. SHA-256 uses lowercase hexadecimal with the `sha256:` prefix. A configured handler limit may be lower than the schema ceiling of 16 MiB.

JSON Schema validates the immutable object shape, lexical bounds, and Base64/digest syntax. The Go conformance validator additionally proves decoded length and digest equality, parses media types, and enforces the canonical escaped-path algorithm. An Action's materialized `inputSchema` must use `opaque-http-app-input.schema.json`; a domain-shaped schema does not match this Admission boundary. The schema ceiling is 16 MiB for cross-implementation compatibility. Core's handler limits decoded responses to 7 MiB so the Base64 response and Job completion envelope remain below the authenticated Worker Plane's 10 MiB request limit.

## Raw-body-v1 wire format

The isolated listener selects `envelope-v1` by default. Set `--opaque-ingress-transport raw-body-v1` or `WINDFORCE_CORE_OPAQUE_INGRESS_TRANSPORT=raw-body-v1` explicitly to select raw transport. This option does not mount a listener without an ingress address. The listener still serves only `POST /ingress/opaque-http/v1` and GET/HEAD `/readyz`; changing the original HTTP method or path in trusted metadata adds no routes.

In raw mode send:

- exactly one `Content-Type: application/octet-stream`, without parameters;
- exactly one `X-Windforce-Opaque-Context`, containing a single-line printable-ASCII JSON serialization of `opaque-http-ingress-context.schema.json`, at most 16,384 bytes;
- the exact application bytes as the HTTP request body, without Base64 wrapping or JSON conversion.

The context's exact members are `kind: "windforce.opaque-http-ingress-context/v1"`, `trustedIngress`, `http`, `receivedAt`, and `deadlineAt`; it has no `body` field. Each nested constraint is a reference to the existing invocation schema, so a standalone validator must load that sibling schema too. Header multiplicity, printable-ASCII and byte bounds, duplicate JSON members, and deadline/path/media-type semantics are additionally enforced at runtime. Use JSON escapes rather than raw non-ASCII header bytes, and do not send the pretty-printed fixture directly as a multiline header.

The context fixture describes the same delivery as `opaque-http-invocation.example.json`. Compact its JSON into the context header and send the ten bytes `00 01 02 7f 80 ff 41 42 43 0a` as the body. Core constructs the exact `opaque-http-app-input.example.json` input. The fixture timestamps are synthetic; a real request requires current `receivedAt` and an allowed future `deadlineAt`. The trusted deadline bounds reading the raw bytes, resolution, Admission, and the synchronous wait; deadline expiry after Admission does not cancel an existing Run.

`http.method`, `http.exactEscapedPath`, and `http.contentType` describe the original request, not the outer POST, fixed ingress path, and octet-stream media type. All existing canonical metadata restrictions remain, including the prohibition on query/fragment delimiters. Core rejects any `Content-Encoding` and any declared or received trailers. Transfer framing is not part of the preserved application bytes. Core computes byte length, canonical Base64, and SHA-256 from the received bytes without decompression, text decoding, or application JSON parsing.

Unknown or repeated reserved `X-Windforce-Opaque-*` headers fail closed. Envelope mode rejects these headers rather than taking trusted values from both the header and envelope. A trusted boundary must strip caller-supplied reserved headers before asserting its own context and must keep that context out of access logs. Ordinary transport headers are not interpreted as trusted context. No automatic mode selection, public authentication shortcut, or additional read endpoint is provided.

Both transports normalize to the same invocation kind for Admission idempotency: the same trusted tuple, original HTTP metadata, and bytes replay the same Run even after a transport migration. Changing bytes under the same identity returns a `409` `windforce.execution-outcome/v1` platform failure with category `applicationProtocolViolation`, without creating a second Run. Identify that conflict by the envelope, not by status alone, because the App may itself return `409`. A replay can refresh timestamps to obtain a new bounded wait, but must retain the same logical delivery identity and still pass current projection and Release checks.

The digest is an unkeyed byte-consistency check, not peer authentication or proof of pre-transformation bytes at an upstream public listener. Raw transport preserves the body observed at Core; the same authenticated allowed-peer boundary is mandatory. Core's response writer must support request read deadlines, and custom wrappers must provide `Unwrap` to retain that support.

## Trust boundary

`opaque-http-invocation.schema.json` and `opaque-http-ingress-context.schema.json` are trusted internal contracts, not public APIs. `trustedIngress.credentialRef` is an immutable snapshot reference and never a raw token or secret; it remains required for synthetic deliveries. `trustedIngress.deliveryId` is the identity the isolated boundary assigns to one delivery; it is that boundary's own value, never an arbitrary caller-supplied authority, and it is the only source of Admission idempotency on this path. Redelivering the same identity with the same payload replays the committed Run; the same identity with a different payload is a conflict. A production Resolver receives only the trusted ingress references, HTTP route metadata, and decoded body length; it does not receive `body.data`, decoded bytes, body digest, caller timestamps, or the envelope deadline through `context.Deadline`. Cancellation still propagates. It must atomically validate issuer, audience, publication revision and generation, and credential snapshot before returning a scoped Service Principal and active Release precondition.

For the store-backed Resolver, `issuer` and `audience` are deployment-chosen printable-ASCII strings of 1–160 characters without surrounding whitespace. They need not be URLs or OIDC identifiers, but must exactly match both immutable projections. The boundary owns their assertion after proving the caller; the string values alone are not authentication. Envelope/context construction is a boundary responsibility, not a requirement for a separate service. Raw mode avoids mandatory body transformation; no general-purpose gateway builder is shipped by this contract.

The conformance handler accepts application wire statuses from 200 through 599. The schema retains the complete HTTP status integer range for cross-implementation contract compatibility; informational statuses are rejected by this handler because they cannot be the final synchronous response.

See [Opaque HTTP ingress conformance](../../../docs/concepts/opaque-http-ingress.md) for runtime behavior and production activation gates, and [ADR 0063](../../../docs/adr/0063-accept-raw-body-transport-on-the-isolated-opaque-ingress.md) for raw-transport rationale and acceptance criteria.
