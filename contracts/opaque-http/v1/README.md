# Opaque HTTP v1 contracts

This directory contains the provider-neutral machine contracts for Core's default-unmounted opaque HTTP ingress conformance handler.

| File | Purpose |
| --- | --- |
| `opaque-http-invocation.schema.json` | Trusted internal gateway-to-handler envelope |
| `opaque-http-app-input.schema.json` | Exact input admitted to the pinned App Action |
| `application-wire-response.schema.json` | Exact JSON result the App returns to the handler |
| `execution-outcome.schema.json` | Stable completed or provider-neutral platform-failure representation |
| `opaque-http-invocation.example.json` | Synthetic valid trusted envelope with non-text bytes |
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

JSON Schema validates the immutable object shape, lexical bounds, and Base64/digest syntax. The Go conformance validator additionally proves decoded length and digest equality, parses media types, and enforces the canonical escaped-path algorithm. An Action's materialized `inputSchema` must use `opaque-http-app-input.schema.json`; a domain-shaped schema does not match this Admission boundary.

## Trust boundary

`opaque-http-invocation.schema.json` is a trusted internal envelope, not a public API contract. `trustedIngress.credentialRef` is an immutable snapshot reference and never a raw token or secret. A production Resolver must atomically validate issuer, audience, publication revision and generation, and credential snapshot before returning a scoped Service Principal and active Release precondition.

The conformance handler accepts application wire statuses from 200 through 599. The schema retains the complete HTTP status integer range for cross-implementation contract compatibility; informational statuses are rejected by this handler because they cannot be the final synchronous response.

See [Opaque HTTP ingress conformance](../../../docs/concepts/opaque-http-ingress.md) for runtime behavior and production activation gates.
