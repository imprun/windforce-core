---
title: App runtime interface and SDK boundaries
description: The SDK-neutral system boundary between Core and executable App bundles.
---

This document is the current human-readable reference for the interface between Windforce Core and executable applications. Here, an interface or contract means a runtime communication specification, not a legal agreement.

> Trace implementation status (2026-08-06): the optional ADR 0029 carrier is implemented in the TypeScript, Python, and Go Core Author SDK surfaces and their launcher transport.

Bun/TypeScript is the Tier 1 App authoring path for new Core capabilities and Application SDK integrations. Python and Go keep their implemented launcher and Author SDK surfaces as compatibility runtimes; new feature parity is not automatic. This investment boundary does not change the SDK-neutral `main(coreCtx)` interface described here. See [Product boundary](product-boundary.md) and [ADR 0047](../adr/0047-define-bun-typescript-app-and-external-capability-boundary.md).

## The central rule

Core does not detect, classify, import, negotiate, or otherwise care which SDKs an App uses. An App may use a scraping SDK, Playwright, Puppeteer, a mobile SDK, several SDKs, or no SDK. To Core, all of those are opaque application dependencies inside the same immutable App bundle.

Core evaluates only its own executable interface:

- the pinned manifest and prepared execution bundle are valid;
- the selected worker and language launcher are compatible;
- the pinned entrypoint exposes `main(coreCtx)`;
- the process produces logs and a terminal result under Core lifecycle rules.

Package names, SDK context versions, module envelopes, HTTP helper shapes, and browser libraries are not Core admission, scheduling, or execution inputs.

## SDK names describe consumers, not Core runtime types

The ecosystem uses the word SDK for independent helpers. This is documentation vocabulary for people; Core does not create an SDK registry or branch execution by these categories.

| Helper | Where it runs | What Core observes |
| --- | --- | --- |
| Invocation SDK | Outside Core, as an optional `/api/v1` HTTP client | An authenticated HTTP request, not the client library |
| Core Author SDK | Inside the App bundle as Core-provided language types and helpers | The same `main(coreCtx)` interface |
| Any Application SDK | Inside the App bundle as an ordinary dependency | Nothing SDK-specific; only the App process and its result |

## The Core-to-App interface

An executable App bundle exposes the entrypoint and Actions declared by the configured canonical manifest (`windforce.json` by default). For the Tier 1 TypeScript runtime and the Python and Go compatibility runtimes, the application entrypoint ultimately receives one Core-owned context and returns one terminal Action value.

```text
Core Worker
  -> validate pinned Deployment and execution bundle
  -> select the pinned language launcher
  -> construct the Core context for the claimed Job
  -> call App main(coreCtx)
  -> collect logs and the terminal result
  -> complete the leased Job
```

Core owns the meaning of the host context: effective input, trigger metadata, App and Action identity, Job-scoped identity, actor metadata, logging, Variables, Resources, State, low-level HTTP, generic HumanTask hold, approval, flow-resume data, and a read-only optional W3C telemetry carrier. Core also owns the private transport used by its runtime wrapper and Author SDK to implement those capabilities.

Application code and its dependencies consume the context capabilities. They must not parse private `WF_*` variables, carry `WF_TOKEN`, build Core callback URLs, write queue records, or call the Worker Plane. Core-owned launcher and Author SDK glue may translate private process transport into the public context surface; that exception does not make the private transport an application API.

App-owned runtime configuration is an explicit part of this host boundary. A Release declares the exact Variable and typed Resource paths that an Action may read or write; Admission validates those declarations and pins the resulting targets to the Run and Job attempt. The Worker then exposes only the pinned capabilities through a Job-scoped loopback proxy. An App reference never falls back to a Workspace value and never widens the pinned grant, even when the referenced path exists elsewhere.

Runtime writes use optimistic concurrency and an idempotent `operationId`. A successful write, revision change, and audit entry commit atomically. Secret values are write-only: the Worker registers them with the dynamic masking registry before forwarding the mutation, and Core never returns the plaintext through the management or runtime read surfaces. TypeScript, Python, and Go Author SDKs expose the same neutral Variable and Resource operations.

The App runtime lifecycle is independent from release publication. `active` admits and serves pinned access, `tombstoned` blocks new Runs and attempts while allowing attempts that already started to finish, and `revoked` blocks all access and requests cancellation of running Jobs. Purge removes the App-owned values only after retirement or revocation and is blocked by a valid lease unless an operator explicitly uses the audited force path. Provisioning export/import preserves the values, revisions, lifecycle, and audit-relevant state without inventing Cloud product or billing concepts.

An Application SDK may continue the optional telemetry carrier exposed by the Core context and create SDK-, App-, or Action-level spans. That carrier is the current Job execution context; Worker polling and Worker Plane transport contexts never enter the App interface. If the SDK is executed directly without Core or without a valid carrier, it may start its own root trace. Core does not detect the SDK or require tracing for execution. See [Execution observability and debugging](execution-observability.md) and [ADR 0029](../adr/0029-optional-trace-context-continuity.md).

The read-only carrier is `coreCtx.telemetry.traceparent` and `.tracestate` in TypeScript, `core_ctx.telemetry.traceparent` and `.tracestate` in Python, and `ctx.Telemetry.TraceParent` and `.TraceState` in Go. The low-level HTTP capability forwards it unless an App SDK supplies its own child carrier. Application code uses this context surface and must not read the private launcher environment used to construct it.

The current TypeScript low-level HTTP capability is `coreCtx.http.fetch`. An Application SDK may deliberately expose a different authoring API, for example `scrapingCtx.httpService.get()` and `post()`. Core does not understand or inspect those methods, because they are implemented inside the App process using the host capability.

The TypeScript `coreCtx.human.wait()` capability is similarly generic. It persists a form task and keeps the same Action process and call stack alive until a decision arrives. An Application SDK may wrap it with app-, domain-, or vendor-specific Interaction vocabulary, but that vocabulary and external delivery channel do not become Core types. See [HumanTask hold](human-task-hold.md).

A worker may also attach private metadata for an optional worker-local capability gateway. This metadata contains a loopback URL, an opaque Job run reference, a Job-scoped token, and opaque ready-provider identifiers. It is not a new provider-specific `WindforceContext` API. An Application SDK may consume and remove this reserved metadata at the App boundary, then expose its own typed facade using the existing low-level host HTTP capability. Core never supplies the worker-wide gateway credential, never interprets provider operations, and never proxies provider binary artifacts. See [ADR 0034](../adr/0034-bind-worker-local-capability-gateways.md).

## Application SDK adaptation

An Application SDK is an opaque dependency carried in the immutable source and execution bundle. It may adapt the Core context at the application boundary instead of introducing another runtime service, but adaptation is optional and entirely internal to the App.

```ts
import { createApp, type WindforceContext } from "windforce-client"
import { createScrapingContext, runModule } from "@data-team/scraping-sdk"

export const main = createApp({
  actions: {
    scrape: async (coreCtx: WindforceContext) => {
      const scrapingCtx = createScrapingContext(coreCtx)
      return await runModule(scrapingCtx)
    },
  },
})
```

The exact package and function names belong to the App and its SDK. The architectural rule is the direction of dependency:

```text
App -> any Application SDK -> Core context capabilities

Core -X-> SDK identity, SDK context, module vocabulary, or transport implementation
```

Each Application SDK owns its author-facing context shape, method semantics, compatibility matrix, versioning, fixtures, migration guidance, and mapping from Core capabilities. For a scraping SDK, this includes the fourth-generation `ScrapingContext`, `inputJson`, logger behavior, `httpService.get/post/patch/put/delete/head/options`, and the supported semantics for cookies, encoding, redirects, proxy, delay, tracing, Playwright, Puppeteer, or mobile bridges.

It also owns an explicit decision for every older surface such as `internalInputJson`, `InternalCall`, tracer, or AIB: supported, replaced with a documented migration, or intentionally unsupported. Core must not acquire those domain names simply to run an App that uses the SDK.

The opt-in external conformance test can be run from a checkout containing the real App repositories:

```powershell
$env:WINDFORCE_TYPESCRIPT_APPS_ROOT = 'C:\path\to\scraping\apps'
go test ./internal/server -run TestTypeScriptTier1ExternalAppsE2E -count=1 -v
```

## Author source and deployment artifact boundary

Core's Git Source contract starts at a canonical deployment artifact containing the configured manifest (`windforce.json` by default), the declared entrypoint, schema files, and any required opaque dependencies. An author repository may instead treat code as its source of truth and generate that artifact. In that case, the App-owned build pipeline runs SDK-specific discovery such as `bun main.ts --describe`, writes inline schemas to canonical files, bundles dependencies, and publishes the resulting deployment Git or snapshot for Core to Register and Sync.

Core must not run `--describe`, import the App, or infer a Manifest from an SDK. That would execute untrusted author code during registration and couple Core to an SDK-specific output shape. Publication only performs the generic static `main` export check and dependency-graph build against the canonical artifact. The external `demo` and `sample` E2E test demonstrates this boundary with a generated deployment Git and then exercises Register, Sync, Publish, Run, and Result through both local and remote workers.

## Core responsibilities

Core is responsible for:

- synchronizing an exact source revision and publishing an immutable, prepared execution bundle;
- treating Bun/TypeScript as the Tier 1 authoring path while preserving `python` and `go` as explicit compatibility launcher contracts;
- injecting and fingerprinting the matching Core Author SDK while preserving application dependencies;
- validating and pinning the manifest, Action schema, runtime, entrypoint, `runsOn`, timeout, and bundle digest at admission;
- matching an eligible worker, claiming and leasing the Job, fetching and validating the pinned bundle, and selecting Bun, a compatibility Python or Go launcher, or an explicit adapter command;
- constructing the Core context and granting only Job-scoped runtime access;
- continuing or creating backend-neutral trace context and exposing its read-only carrier without making telemetry a requirement for execution;
- enforcing cancellation, timeout, drain, log/result masking, completion, and retry semantics;
- returning the terminal result through the Run and Invocation APIs.

Core does not import or interpret any SDK package, SDK context, module envelope, or SDK compatibility version. It only requires the resulting App bundle to satisfy the Core App runtime interface.

## Runtime and worker capability boundary

Browser and mobile execution spans two ownership areas:

- the App and its dependencies own how they use Playwright, Puppeteer, a mobile bridge, and application-level HTTP behavior;
- Core owns the pinned launcher, `runsOn` worker requirements, label matching, Job lease, bundle delivery, and execution lifecycle;
- a self-hoster or downstream fleet manager owns the actual worker image, browser binaries, mobile devices, capacity, and autoscaling.

The same boundary applies to GPU/AI inference, document and native engines, and private infrastructure connectors. They are external capability services rather than additional Core runtime modes. Core may provide provider-neutral placement and Job-scoped binding, while the service owns provider operations, binary artifacts, native resource policy, and provider-specific errors.

In Core, capability and label vocabulary already participates in worker matching. An Application SDK must not redefine Core `capabilities`, `runsOn`, worker labels, or WorkerPool as its own context negotiation protocol. SDK-specific feature discovery, if required, remains internal to the App and must not override Core scheduling.

## Versioning and conformance

The two independently evolving interfaces require separate conformance evidence:

1. Core tests the language wrappers, `WindforceContext`, bundle preparation, launcher, Job-scoped callbacks, and worker lifecycle.
2. Each Application SDK tests its optional context adapter and compatibility fixtures against a supported Core Author SDK/runtime version.
3. Sample Apps prove the composed bundle can be published and executed by an unmodified Core.

An Application SDK may publish a name such as `scraping.ctx/v1` for its own context interface. That identifier is not a Core manifest field and does not require Core to deserialize an SDK schema. Conversely, Core owns changes to `WindforceContext`, private launcher transport, the canonical manifest contract, and worker execution semantics.

## Change checklist

Before changing either side, verify:

- The App still exposes `main(coreCtx)` through the pinned entrypoint.
- Any SDK adaptation occurs inside the App process and is bundled with the App.
- No Application SDK receives Core service credentials or Worker Plane authority.
- Core does not inspect SDK identity, import SDK vocabulary, or interpret an SDK context version.
- `runsOn` and worker labels remain Core scheduling inputs rather than SDK capabilities.
- Worker-local provider identifiers remain opaque runtime observations; an Application SDK consumes the private Job binding and owns its typed facade.
- Browser or mobile library behavior is separated from worker provisioning and Job lifecycle.
- Core and Application SDK conformance suites each test the boundary they own.

The worker-side sequence that invokes this interface is documented in [Worker execution lifecycle](worker-execution.md). The external HTTP client boundary is documented in [Invocation API](public-api.md). The decision rationale is recorded in [ADR 0021](../adr/0021-keep-application-sdks-opaque-to-core.md).
