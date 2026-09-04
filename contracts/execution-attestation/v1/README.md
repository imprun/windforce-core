# Execution attestation v1 contracts

This directory contains the provider-neutral machine contract for the signed execution attestation Windforce Core mints after Admission accepts an invocation resolved from immutable references.

| File | Purpose |
| --- | --- |
| `execution-attestation.schema.json` | The signed document: binding, binding digest, algorithm, signature |
| `execution-attestation-binding.schema.json` | The canonical value the signature covers |
| `execution-attestation-binding.example.json` | Synthetic binding with its expected canonical digest |

The example contains no key material, credential, customer identifier, or real target data.

## What the attestation proves

One statement: *this Run was admitted against exactly these immutable references, for this audience, until this instant.*

A downstream capability service uses it to authorize one exchange after Core admitted the invocation. It is host-private execution metadata: it is never an external HTTP header, a public API response, or part of a Run outcome or event payload.

## Canonical bytes

The signature covers the binding encoded as JSON with:

- properties in the order the binding schema declares them;
- `references` sorted by `name`;
- no whitespace between tokens.

`bindingDigest` is the SHA-256 of those exact bytes, lowercase hexadecimal with the `sha256:` prefix. `signature` is the Ed25519 signature over the same bytes, unpadded RFC 4648 base64url. Any implementation that encodes the binding this way reproduces the digest in `execution-attestation-binding.example.json`.

`issuerKeyId`, `audience`, and `expiresAt` are inside the signed binding: a key cannot be substituted, an audience cannot be widened, and an expiry cannot be extended without invalidating the signature.

## What Core binds, and what it does not interpret

Core binds only references it already pinned when it admitted the Run: the Run reference, workspace, App and Action, the publication reference and route generation, the operation reference, the credential snapshot reference, and the pinned Release.

Everything a downstream service needs but Core does not interpret travels in `references` as named immutable pins, exactly as the projection supplied them. A version there is an opaque string whose meaning belongs to the projecting control plane — a snapshot digest, a material version, or a monotonic epoch. Core neither validates nor acts on those values; it only proves they were the ones pinned for this Run.

Core resolves no secret and opens no material.

## Verification

A verifier checks, in order: the document kind and algorithm, that the binding digest covers the binding, the signature against a trusted public key selected by `issuerKeyId`, the expiry, the audience it requires, and exact equality with the binding its own policy expects. It never learns a value from the attestation — it confirms the one it already holds.

`internal/attestation` is the reference implementation of these checks.
