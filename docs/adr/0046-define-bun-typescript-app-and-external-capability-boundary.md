# ADR 0046: Define the Bun/TypeScript App and external capability boundary

- Status: Accepted
- Date: 2026-08-18
- Issue: [#248](https://github.com/imprun/windforce-core/issues/248)

## Context

Windforce Core began with a general-purpose runtime goal and currently implements TypeScript, Python, and Go launchers. Equal language support, however, is not the product property that makes Core general-purpose. The durable value is the provider-neutral execution contract around immutable Releases, admission, Runs, Jobs, lease-fenced Attempts, retries, limits, runtime configuration, and worker placement.

The product direction for Script Apps has converged on Bun-based TypeScript. New authoring features, Application SDK integrations, and domain adapters are expected to use that path. Python and Go still have working publication, bundle, launcher, and Author SDK contracts that existing deployments may rely on, so changing the investment priority must not silently remove compatibility.

Some Apps need browsers, GPU or AI inference, document engines, mobile devices, private network connectors, or other expensive and stateful facilities. Embedding every facility into Core would couple the queue and launcher to provider APIs, credentials, binary artifacts, capacity policy, and deployment topology. ADR 0021 already keeps Application SDKs opaque to Core, while ADR 0034 defines a provider-neutral, Job-scoped capability gateway binding.

The comparison with long-lived stateful entity systems such as [denoland/celld](https://github.com/denoland/celld) also makes a separate boundary explicit. Core's unit is a terminating Run/Job with lease-fenced Attempts. A named, single-writer entity with a per-object database solves a different problem and must not be added by stretching Run, Job, Variable, or Resource semantics.

## Decision

1. **Core is a general-purpose execution and integration core, not a general-purpose language runtime framework.** Its generality comes from neutral lifecycle and capability contracts rather than equal investment in many language launchers.
2. **Bun/TypeScript is the sole Tier 1 App authoring path for new product capabilities.** New Core Author SDK features, examples, and Application SDK integration work target TypeScript first and are not required to acquire Python or Go equivalents.
3. **Python and Go remain compatibility runtimes.** Their current manifest values, publication paths, immutable bundles, launchers, and documented interfaces remain supported until a separate compatibility decision says otherwise. This ADR does not deprecate or remove them.
4. **Core owns execution coordination.** It owns source synchronization, immutable release publication, admission, queueing, lease/fencing, retry, cancellation, limits, runtime configuration, placement, Job-scoped authorization, masking, and completion.
5. **Apps own domain orchestration.** A TypeScript App may use any Application SDK as an opaque bundle dependency and adapt the Core context inside its process. Core does not classify SDKs or import their domain vocabulary.
6. **External services own provider-native capabilities.** Browser sessions, GPU inference, document or native engines, mobile devices, and similar facilities are supplied by a self-hoster, fleet operator, or hosted product. Core binds them through provider-neutral placement and Job-scoped capability contracts. Provider APIs, worker-wide credentials, binary artifact transport, native resource limits, and provider-specific errors stay outside Core.
7. **Hosted and self-hosted deployments use the same Core boundary.** Core does not require a hosted control plane. A self-hoster may run ordinary Bun/TypeScript Apps without an external capability service and may bind locally operated services when an App requires them.
8. **Long-lived stateful entities are not inferred from the current model.** Actor, Durable Object, per-object database, or single-writer entity semantics require a concrete consumer and a separate ADR covering identity, routing, ownership, storage, migration, and recovery.

## Consequences

- Documentation and examples can explain one preferred authoring path without making working Python and Go deployments disappear.
- New host capabilities do not automatically multiply across three Author SDKs. A change may deliberately extend compatibility runtimes, but it needs an explicit consumer and maintenance commitment.
- Core stays small enough for installation on premises while allowing Cloud or an internal fleet to supply expensive capabilities independently.
- Browser Edge and similar services are integrations, not additional Core runtime modes. Their availability participates in placement and a Job-scoped binding instead of changing Run/Job semantics.
- The standard image may continue to contain Python and Go for compatibility. Image contents do not define the Tier 1 investment policy.
- A future Python/Go deprecation, multiple capability gateways, remote gateways, or stateful entity model needs its own migration and decision record.

## Rejected alternatives

- **Keep TypeScript, Python, and Go permanently symmetric.** Rejected because every Core context feature would require three equally maintained authoring surfaces even though the product's Script App path has converged on Bun/TypeScript.
- **Remove Python and Go immediately.** Rejected because existing manifests and execution bundles may rely on those contracts and no compatibility migration has been approved.
- **Embed Browser, GPU, document, or provider runtimes in Core.** Rejected because provider credentials, capacity, native dependencies, and artifact protocols would make the neutral execution engine own downstream infrastructure.
- **Adopt a Durable Object or per-object database as the Core execution unit.** Rejected because long-lived named state and terminating at-least-once Jobs have different ownership and recovery semantics.

## Related decisions

- [ADR 0021: Keep Application SDKs opaque to Core](0021-keep-application-sdks-opaque-to-core.md)
- [ADR 0022: Make TypeScript a strict Tier 1 runtime](0022-make-typescript-a-strict-tier-1-runtime.md)
- [ADR 0034: Bind worker-local capability gateways](0034-bind-worker-local-capability-gateways.md)
