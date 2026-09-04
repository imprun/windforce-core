# ADR 0062: Serve the opaque HTTP ingress on its own configured listener

- Status: Accepted
- Date: 2026-09-04
- Amends: [ADR 0055](0055-add-default-unmounted-opaque-http-ingress-conformance.md)
- Issue: [#285](https://github.com/imprun/windforce-core/issues/285)

## Context

ADR 0055 built the opaque HTTP ingress as a conformance handler and mounted it nowhere. ADR 0056 gave it an atomic Resolver, ADR 0059 a trusted delivery identity, ADR 0060 an execution attestation, and ADR 0061 restated the peer gate as a property rather than a mechanism. Every piece of the data path exists and none of it is reachable: no address serves the handler, and no flag turns it on.

That leaves an operator two bad options. Fork `main.go` to mount it, which means maintaining a patch against every release, or mount it on the primary listener, which the first activation gate forbids — the isolation is what makes a trusted issuer, audience and delivery identity safe to accept at all.

The listener is also where three deployment concerns land that the handler cannot answer on its own. A prober needs to know whether this boundary is serving. A burst of deliveries needs a bound, because each one holds a synchronous wait on a Run. And a listener that dies needs a defined process outcome.

## Decision

Core owns the isolated listener and configures it at startup.

**One address, unmounted by default.** `-opaque-ingress-addr` (or `WINDFORCE_CORE_OPAQUE_INGRESS_ADDR`) mounts the listener; empty leaves it off. ADR 0055's default-unmounted posture is unchanged — a Core that is not configured for this path does not open it.

**Two paths, and nothing else.** The listener serves the exact ingress path and a readiness path. Every other path, including a prefix of the ingress path, answers 404 with a platform failure. The primary listener does not serve the ingress path, and a test asserts it.

**Readiness belongs to this listener.** The gateway that delivers here probes here. A readiness endpoint on the primary listener cannot report on a boundary the prober is not allowed to reach, and on a network-boundary deployment (ADR 0061) it is not reachable from the same place at all. The probe reports ready once the listener is bound with its resolver and admission wired, and unready as soon as drain begins, so a gateway stops sending deliveries into a listener that is going away. It is deliberately not a store health check: Core has no cheap liveness probe for the projection store on this path, and running a query per probe would add both load and a failure mode of its own. Storage faults surface as platform failures on the delivery itself.

**Bounded concurrency, then shed.** The listener admits a bounded number of concurrent deliveries — 64 by default, ceiling 4096 — and a delivery waits at most 50 milliseconds for a slot, ceiling one second, before the listener answers 503 with a retryable `capacityUnavailable`. The short wait is the point. Each admitted delivery holds a Run wait, so an unbounded queue converts a capacity problem into what the caller reads as a Core hang, and by the time the caller's own deadline expires the work is already committed.

**Fail closed at startup and at runtime.** An unbindable address, an unusable limit, or an unusable execution attestation key exits the process non-zero before anything is served. If the ingress listener stops while the process runs, the process drains the primary listener and exits non-zero. A Core that answers its primary API while this boundary is dead looks healthy to every prober except the one that matters, and a restart is the honest outcome.

**Execution attestation is configured with it.** The signing key file, key id, audience and lifetime (ADR 0060) sit in the same flag group, because the deployment that mounts this boundary is the one that needs them. Minting stays opt-in: with no key, Admission behaves exactly as before. A partial configuration — a key without an audience, an audience without a key — is a startup error rather than a silent downgrade to unsigned pins.

**No TLS here.** The listener terminates no TLS and verifies no certificate. Which mechanism proves the peer is the deployment's choice under ADR 0061, and Core neither implements nor detects it.

## Consequences

- The ingress is reachable by configuration. Activation is now a deployment decision, not a fork.
- The isolation gate is enforced in code rather than in prose. The trusted envelope is accepted on one listener, on one path, and the primary listener 404s it.
- A deployment gets a readiness signal it can wire to a gateway, and a drain that stops new deliveries before in-flight ones finish.
- Overload has a defined answer with a retryable category, so a gateway can back off instead of timing out.
- A dead ingress listener is a dead process. An operator running the ingress and the primary API in one process accepts that coupling; splitting them into separate deployments remains available and is unaffected by this decision.
- The numbers here are defaults, not a capacity model. A deployment that fronts a slower downstream will raise the concurrency bound and lower the wait, and both are flags.

## Alternatives

| Alternative | Why not |
| --- | --- |
| Mount the handler on the primary listener behind a path prefix | Forbidden by the first activation gate. Any caller that reaches Core could then assert a trusted issuer, audience and delivery identity. |
| Leave mounting to the operator's own build | Every self-hoster maintains a patch against `main.go`, and the isolation rule is enforced by nobody. |
| Put readiness only on the primary listener | The prober for this boundary is behind it. On a network-boundary deployment the primary readiness path may be unreachable from the delivering gateway, and it reports nothing about whether this listener is bound. |
| Probe the projection store on every readiness call | Adds database load proportional to probe frequency and makes a transient store blip look like a dead listener. Storage faults already surface per delivery. |
| Queue deliveries without a bound | Each delivery holds a Run wait, so a burst becomes unbounded in-flight work and the caller sees a hang rather than a retryable rejection. |
| Log and continue when the ingress listener dies | The process would keep passing its primary health checks while admitting nothing on the boundary that matters. |
| Terminate TLS on this listener inside Core | ADR 0061 leaves the peer mechanism to the deployment. Building certificate handling here would re-impose the requirement it removed. |

## References

- [ADR 0055](0055-add-default-unmounted-opaque-http-ingress-conformance.md) — the conformance handler and the activation gates this amends.
- [ADR 0056](0056-persist-opaque-http-projections-and-resolve-atomically.md) — the atomic Resolver the listener serves.
- [ADR 0059](0059-bind-opaque-ingress-delivery-identity-to-admission-idempotency.md) — the trusted delivery identity this boundary supplies.
- [ADR 0060](0060-mint-audience-bound-execution-attestations-after-admission.md) — the execution attestation issuer configured with this listener.
- [ADR 0061](0061-accept-any-authenticated-peer-boundary-for-opaque-ingress.md) — the peer property this listener assumes and does not verify.
- [Opaque HTTP ingress conformance](../concepts/opaque-http-ingress.md) — the current description of the boundary.
