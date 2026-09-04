# ADR 0060: Mint audience-bound execution attestations from Admission-pinned references

## Status

Accepted

## Context

An App admitted through the opaque HTTP ingress may need to call a private downstream capability service — one that holds material the App is not allowed to hold itself. That service has to decide whether this particular execution may proceed, and it cannot take the App's word for it.

The invocation pins Core already carries are not usable for that decision. They are unsigned Job metadata: anything that can reach the worker plane can also present them, so they identify an execution without proving one. ADR 0055 said so explicitly and deferred the proof.

Whatever proves it must also stay out of Core's problem domain. A downstream capability service has its own grant model, its own epochs, and its own material versions. If Core stored and interpreted those, the engine would carry a downstream product's semantics for values it can never validate — which the scope discipline in `AGENTS.md` and the exclusion list of the originating issue both forbid.

## Decision

After Admission accepts an invocation that carries resolved invocation pins, Core mints a signed execution attestation and stores it in the Job payload.

The attestation binds exactly what Core already pinned: the Run reference, workspace, App and Action, the publication reference and route generation, the operation reference, the credential snapshot reference, and the pinned Release. The issuer adds the audience, its key id, and an expiry, and the signature covers all of it.

Values Core does not interpret travel in `references` as named immutable pins, verbatim from the projection. A downstream service's snapshot reference, material version, or security epoch reaches the attestation that way. Core proves those were the values pinned for this Run; it never validates or acts on them, and no downstream vocabulary enters the engine.

Canonical bytes are the binding encoded as JSON in schema-declared property order with `references` sorted by name. `bindingDigest` is their SHA-256 and the Ed25519 signature covers the same bytes, so an independent implementation reproduces both.

The issuer is optional and fails closed silently: a deployment that configures no key admits Runs exactly as before and simply cannot use a downstream capability service. One issuer serves one audience; a deployment that needs several is a later decision, not a hidden per-request choice.

The attestation is host-private. It rides in the Job payload beside the invocation pins, which is the established carrier for pinned execution metadata, and the public job status omits it like the pins.

## Consequences

- A private capability service can authorize one exchange from a proof it verifies itself: signature, audience, expiry, and exact equality with its own policy.
- Verification never teaches the verifier a value. It confirms what it already holds, so a forged or replayed attestation for another Run, credential, route generation, epoch, or material version fails on the equality check rather than being trusted.
- Replaying the exact bytes of a valid attestation still only authorizes the Run it names, until its expiry. Lifetime is bounded at fifteen minutes and defaults to five.
- The engine gains a signing key to operate. It is a separate key with a separate purpose from the local state encryption key of ADR 0058, and it is supplied by file reference like every other secret Core reads.
- Nothing is minted until a deployment both mounts the ingress listener and configures an issuer. Mounting remains gated on the isolated-listener work.

## Alternatives

| Alternative | Why not |
| --- | --- |
| Let the downstream trust `InvocationPins` | Unsigned metadata proves nothing; anything on the worker plane can present it. |
| Store the downstream's epoch and material version as typed Core fields | Core would persist, digest, and migrate values it can never validate, in two stores, for one product's model. |
| Mint at Job dispatch instead of at Admission | The references are pinned at Admission; minting later would attest a state nobody checked at the moment it mattered, and a redispatch would silently re-mint. |
| Put the attestation in the App input | The App would forward its own authorization, and the value would land in Run input, which is public to an ordinary API reader. |
| Long-lived or non-expiring attestations | Replay surface grows with no gain: one exchange for one admitted Run needs minutes, not hours. |

## References

- [ADR 0055](0055-add-default-unmounted-opaque-http-ingress-conformance.md) — the ingress that produces the pinned references, and the deferral this decision resolves.
- [ADR 0056](0056-persist-opaque-http-projections-and-resolve-atomically.md) — the atomic Resolver that pins them.
- [Execution attestation v1 contracts](../../contracts/execution-attestation/v1/README.md) — schemas, canonical bytes, and the synthetic fixture.
- [Opaque HTTP ingress conformance](../concepts/opaque-http-ingress.md) — the current description of the boundary.
