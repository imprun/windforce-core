---
title: Product boundary
description: Why Windforce Core is a Bun/TypeScript execution core rather than a multi-language or provider runtime platform.
---

Windforce Core is a general-purpose execution and integration core for durable Script Apps. It synchronizes and publishes immutable App Releases, admits Runs, queues Jobs, fences Worker Attempts, applies runtime configuration and execution limits, and binds optional external capabilities.

Its generality comes from those neutral execution contracts, not from running every language or embedding every infrastructure service.

## Preferred App path and compatibility

| Path | Product status | Meaning |
| --- | --- | --- |
| Bun/TypeScript | Tier 1 | The preferred path for new Apps, Core Author SDK capabilities, examples, and Application SDK integrations. |
| Python | Compatibility | Existing manifests, bundles, launcher behavior, and Author SDK surfaces remain supported. New feature parity is not automatic. |
| Go | Compatibility | Existing manifests, static bundle preparation, launcher behavior, and Author SDK surfaces remain supported. New feature parity is not automatic. |
| Adapter command | Explicit extension | A deployment-specific execution adapter, not a new built-in authoring language or an implicit fallback. |

An omitted `scriptLang` still means `typescript` for manifest compatibility. Core accepts only the implemented explicit values and never treats an unknown language as Bun. Python and Go are not deprecated by this product direction; removal or a reduced compatibility promise requires a separate decision and migration.

## Responsibility boundary

```text
Hosted product or self-hosted operator
  -> environment policy, infrastructure, fleet capacity, provider services

Windforce Core
  -> source sync, immutable release, admission, queue, lease/fencing
  -> retry, cancellation, limits, runtime configuration, placement
  -> Job-scoped capability binding, masking, cleanup, completion

Bun/TypeScript App
  -> Action orchestration and domain behavior
  -> opaque Application SDK dependencies

External capability service
  -> browser, GPU/AI, document/native engine, mobile, private connector
  -> provider API, native resource limits, binary artifacts, provider errors
```

Core is complete without a hosted control plane. A self-hoster can run ordinary Apps with the embedded Worker and can bind locally operated capability services only when an App needs them. Hosted products and internal fleets use the same Core HTTP and worker contracts rather than introducing commercial tenant or provider vocabulary into Core.

Commercial plans, pricing, billing, legal contracts, and hosted tenant lifecycle belong to the hosted product. Core stores and enforces only the neutral execution policies that are also meaningful to a self-hoster.

## Capability services, not runtime modes

Browser Edge, GPU inference, document engines, and similar facilities are not additional Core runtime modes. They often need independent scaling, native dependencies, long-lived processes, large binary transfer, provider credentials, or hardware scheduling.

Core therefore owns only the neutral execution side of the integration:

- an App or operator declares ordinary placement requirements;
- an eligible Worker discovers a configured capability service;
- Core opens a Job-scoped session with the Worker credential;
- the App receives only a short-lived opaque binding through private launcher transport;
- Core masks the binding and closes it on every terminal path;
- the service owns its provider calls, artifacts, concurrency, and sanitized provider errors.

The current worker-local gateway contract is defined by [ADR 0034](../adr/0034-bind-worker-local-capability-gateways.md). Supporting multiple or remote gateways requires another trust and routing decision; it is not implied by this product boundary.

## Non-goals

Windforce Core is not:

- a framework that promises equal investment in every programming language;
- a Browser, GPU, mobile, document, or database service implementation;
- a Kubernetes, VM, or Worker fleet provisioning controller;
- a commercial tenant, pricing, billing, or global quota system;
- a long-lived Actor, Durable Object, or per-object database runtime.

A long-lived named entity may become useful later, but it has different identity, single-writer, routing, storage, migration, and recovery semantics from a terminating Run/Job. It needs a concrete consumer and a separate ADR instead of being modeled as a special Job or Resource.

## Related contracts

- [Core concepts](core-concepts.md) defines the durable source, release, Run, and Job model.
- [App runtime interface and SDK boundaries](app-runtime-interface.md) keeps Application SDKs opaque to Core.
- [Worker execution lifecycle](worker-execution.md) defines how a pinned Job becomes one process Attempt.
- [Runtime configuration and secrets](runtime-configuration.md) defines Job-scoped Variable, Resource, InputConfig, and Secret resolution.
- [ADR 0046](../adr/0046-define-bun-typescript-app-and-external-capability-boundary.md) records the decision and rejected alternatives.
