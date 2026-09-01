# ADR 0054: Preserve opaque public interface declarations

- Status: Accepted
- Date: 2026-08-30
- Issue: [#279](https://github.com/imprun/windforce-core/issues/279)

## Context

An App-owned build pipeline may need an immutable Release to advertise that an Action implements an externally defined public interface. The declaration must survive manifest parsing, source synchronization, Release candidates, active and historical Release storage, action discovery, and Admission pinning. Core must preserve that declaration without recognizing an Application SDK, provider, protocol, context version, or declaration kind.

The unversioned canonical manifest accepts the existing v1 fields and ignores unknown fields. Adding an opaque extension to that shape would make misspellings indistinguishable from declarations that an older Core silently dropped. A versioned contract is required before Releases can rely on the metadata.

## Decision

The Core-owned canonical manifest v2 is selected by the root field `"apiVersion": "windforce.app-manifest/v2"`. A manifest without `apiVersion` is v1. Any other declared value is unsupported and publication fails.

V2 decodes a source-manifest allowlist and rejects unknown root and Action fields, including runtime-owned Deployment fields that are not author inputs. `publicInterfaces` is an optional Action field containing an ordered array of opaque JSON objects. Its presence, including an empty array, is valid only in v2; JSON `null` is not an array and is rejected. Core treats every object body as data and never branches on its member names or values.

Core applies only generic structural rules:

- at most 16 declarations per Action;
- at most 16 KiB per canonical declaration and 64 KiB of canonical declarations per Action;
- at most 64 KiB of source encoding per declaration and 256 KiB per Action before canonicalization;
- every declaration is one JSON object with no trailing value, no duplicate object member name, and no nesting deeper than 16 containers.

Core canonicalizes each declaration with Go `encoding/json` after decoding numbers as `json.Number`. The representation removes insignificant whitespace, sorts object member names according to `encoding/json`, retains array order and JSON number spelling, and applies the standard encoder string escaping. The canonical byte sequence is the declaration's generic identity. Two declarations with the same canonical bytes in one Action are duplicates and publication fails. Core does not derive identity from a declaration field.

The canonical `apiVersion` and declarations are copied into the synchronized Deployment, Release candidate, active Release, Release history, Run, compact Job payload, reconstructed pinned Deployment, and App/Action description projections. Every in-memory snapshot boundary deep-copies the `json.RawMessage` bytes. JSON-backed stores may format their containing records; catalog reads reapply the canonical declaration representation before returning a snapshot.

The canonical manifest and Deployment JSON use `apiVersion` and `publicInterfaces`. Existing control and Invocation API projections use their established snake-case convention and expose `api_version` and `public_interfaces`.

`publicInterfaces` is discovery metadata and does not bypass the Action `inputSchema`. Admission still validates the exact resolved input submitted to Core against the pinned materialized schema before creating a Job. A bridge that admits an opaque transport envelope must therefore publish an `inputSchema` for that envelope; any decrypted or translated domain input may be validated separately inside the App. A bridge that keeps a domain-shaped `inputSchema` must translate into that shape before Admission.

Core materializes and pins `outputSchema`, but it does not currently validate an App result against that schema. The runtime rejects only a result that is not valid JSON. The canonical `outputSchema` therefore describes the JSON actually returned by the App: it is a public-protocol response wrapper only when the App itself returns that wrapper. A gateway or bridge that encodes a domain result into another public response shape owns that response validation and translation outside Admission.

## Consequences

External publishers can discover a stable declaration from the same immutable Release that Admission pins for execution. V1 manifests without declarations keep their existing parsing behavior. A declaration requires an explicit v2 marker, so a misspelled v2 root or Action field cannot be silently dropped.

Application SDKs and protocol adapters remain independently versioned. A bridge may translate an SDK-owned description into the Core-owned v2 manifest, but Core does not import or validate the source vocabulary. Route publication, authentication, request and response codecs, and gateway configuration remain outside this contract.

## Rejected alternatives

- **Recognize a provider, SDK package, context version, or declaration kind in Core.** Rejected because it reverses the opaque Application SDK boundary in ADR 0021.
- **Use one declaration member as the Core identity.** Rejected because choosing such a member assigns domain meaning to otherwise opaque data.
- **Preserve source bytes without canonicalization.** Rejected because JSON-backed catalogs may format containing records and semantically identical object member order would make duplicate detection unstable.
- **Allow declarations in unversioned manifests.** Rejected because an older or misspelled consumer could silently drop Release metadata that callers expect to be immutable.
