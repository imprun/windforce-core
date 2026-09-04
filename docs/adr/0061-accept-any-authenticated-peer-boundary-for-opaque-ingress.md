# ADR 0061: Accept any authenticated peer boundary for the opaque ingress listener

- Status: Accepted
- Date: 2026-09-04
- Amends: [ADR 0055](0055-add-default-unmounted-opaque-http-ingress-conformance.md)
- Issue: [#285](https://github.com/imprun/windforce-core/issues/285)

## Context

ADR 0055 left the opaque HTTP ingress handler default-unmounted and listed what production activation requires. The first gate named a mechanism: a private listener protected by mutually authenticated TLS, with the issuer and audience binding derived from the verified peer certificate.

Naming the mechanism decides a deployment question for every self-hoster. Mutual TLS needs a certificate authority, an issuance path, and a rotation procedure for both sides of the hop. A deployment that runs Core and its trusted gateway inside one private network may already authenticate peers at the network layer — a dedicated namespace, a service that carries no other traffic, and bidirectional policies that allowlist the calling namespace, pod, and port — and may run no certificate authority at all. For that operator the gate does not add a boundary; it adds a second one to build and rotate, with its own failure modes.

The property the gate exists to guarantee is narrower than the mechanism: only authenticated allowed peers may reach the handler, and the issuer and audience must come from that boundary rather than from an untrusted caller. Both forms can satisfy it. The engine's scope discipline says to require the property and leave the implementation to the deployment, the same way Core states what a Resolver must prove without specifying where the operator stores its projections.

A downstream consumer building on this ingress surfaced the conflict: it removed mutual TLS from its private hops in favour of a network boundary, and could not satisfy a gate that requires a client certificate.

## Decision

The activation gate is the property, not the mechanism.

A production deployment must place the handler on a separate private listener that only authenticated allowed peers can reach, and the issuer and audience must be asserted by that isolated boundary. Core accepts them on that listener only.

Two forms satisfy the gate:

- mutually authenticated TLS, with the peer identity taken from the verified client certificate;
- a network boundary, where the listener carries no other traffic and bidirectional network policy allowlists the calling namespace, pod, and port.

A network-boundary deployment has no per-request cryptographic proof of the peer. Its activation evidence must therefore show that the network layer actually enforces the allowlist — the network plugin applies the policies, and the listener is unreachable from anywhere else — because a policy that is declared but not enforced leaves the handler open.

Nothing else changes. The rule that a caller cannot manufacture a trusted issuer, audience, delivery identity, or projection metadata is unchanged, and Core still refuses trusted headers on any listener other than the isolated one. The remaining ADR 0055 gates stand.

## Consequences

- An operator without a certificate authority can activate the ingress by proving a network boundary instead of building a public key infrastructure for one in-cluster hop.
- An operator who prefers cryptographic peer proof keeps mutual TLS; the gate did not become weaker for them, only narrower in what it mandates.
- The two forms fail differently, and the activation evidence differs with them. A certificate deployment proves peer identity per request; a network deployment proves reachability once, at the layer that enforces it. Stating that difference is the point of the deployment note.
- Core cannot tell which form is in use and does not try. The engine's own invariant — trusted values are accepted on the isolated listener only — holds either way.
- The isolated-listener work tracked by issue #285 implements the property. It no longer implies a certificate authority as a prerequisite.

## Alternatives

| Alternative | Why not |
| --- | --- |
| Keep mutual TLS as the required mechanism | Forces a certificate authority and a rotation procedure on every self-hoster for one in-cluster hop, including those whose network already authenticates the peer. |
| Accept trusted ingress values on the primary listener when the peer is allowlisted | The isolation is what makes the trusted values safe; a shared listener would let any caller that reaches Core assert them. |
| Let Core detect the mechanism and vary its behavior | Core cannot verify a network boundary from inside the process, and a wrong guess would silently weaken the boundary. The deployment asserts the property; Core enforces the listener rule. |
| Leave the gate vague | An operator would have no criterion to satisfy, and the network-boundary form has a real extra obligation — proving enforcement — that a vague gate would hide. |

## References

- [ADR 0055](0055-add-default-unmounted-opaque-http-ingress-conformance.md) — the conformance handler and the gate list this amends.
- [ADR 0056](0056-persist-opaque-http-projections-and-resolve-atomically.md) — the atomic Resolver behind the listener.
- [ADR 0059](0059-bind-opaque-ingress-delivery-identity-to-admission-idempotency.md) — the trusted delivery identity the same boundary supplies.
- [Opaque HTTP ingress conformance](../concepts/opaque-http-ingress.md) — the current description of the boundary.
