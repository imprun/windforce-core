---
title: App runtime interface and SDK boundaries
description: The SDK-neutral system boundary between Core and executable App bundles.
---

This document is the current human-readable reference for the interface between Windforce Core and executable applications. Here, an interface or contract means a runtime communication specification, not a legal agreement.

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

An executable App bundle exposes the entrypoint and Actions declared by `windforce.json`. For the built-in TypeScript, Python, and Go runtimes, the application entrypoint ultimately receives one Core-owned context and returns one terminal Action value.

```text
Core Worker
  -> validate pinned Deployment and execution bundle
  -> select the pinned language launcher
  -> construct the Core context for the claimed Job
  -> call App main(coreCtx)
  -> collect logs and the terminal result
  -> complete the leased Job
```

Core owns the meaning of the host context: effective input, trigger metadata, App and Action identity, Job-scoped identity, actor metadata, logging, Variables, Resources, State, low-level HTTP, generic HumanTask hold, approval, and flow-resume data. Core also owns the private transport used by its runtime wrapper and Author SDK to implement those capabilities.

Application code and its dependencies consume the context capabilities. They must not parse private `WF_*` variables, carry `WF_TOKEN`, build Core callback URLs, write queue records, or call the Worker Plane. Core-owned launcher and Author SDK glue may translate private process transport into the public context surface; that exception does not make the private transport an application API.

The current TypeScript low-level HTTP capability is `coreCtx.http.fetch`. An Application SDK may deliberately expose a different authoring API, for example `scrapingCtx.httpService.get()` and `post()`. Core does not understand or inspect those methods, because they are implemented inside the App process using the host capability.

The TypeScript `coreCtx.human.wait()` capability is similarly generic. It persists a form task and keeps the same Action process and call stack alive until a decision arrives. An Application SDK may wrap it with company-specific Interaction vocabulary, but that vocabulary and external delivery channel do not become Core types. See [HumanTask hold](human-task-hold.md).

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

Core's Git Source contract starts at a canonical deployment artifact containing `windforce.json`, the declared entrypoint, schema files, and any required opaque dependencies. An author repository may instead treat code as its source of truth and generate that artifact. In that case, the App-owned build pipeline runs SDK-specific discovery such as `bun main.ts --describe`, writes inline schemas to canonical files, bundles dependencies, and publishes the resulting deployment Git or snapshot for Core to Register and Sync.

Core must not run `--describe`, import the App, or infer a Manifest from an SDK. That would execute untrusted author code during registration and couple Core to an SDK-specific output shape. Publication only performs the generic static `main` export check and dependency-graph build against the canonical artifact. The external `demo` and `sample` E2E test demonstrates this boundary with a generated deployment Git and then exercises Register, Sync, Publish, Run, and Result through both local and remote workers.

## Core responsibilities

Core is responsible for:

- synchronizing an exact source revision and publishing an immutable, prepared execution bundle;
- treating TypeScript as a Tier 1 runtime while accepting only `typescript`, `python`, and `go` as explicit launcher contracts;
- injecting and fingerprinting the matching Core Author SDK while preserving application dependencies;
- validating and pinning the manifest, Action schema, runtime, entrypoint, `runsOn`, timeout, and bundle digest at admission;
- matching an eligible worker, claiming and leasing the Job, fetching and validating the pinned bundle, and selecting Bun, Python, Go, or an adapter command;
- constructing the Core context and granting only Job-scoped runtime access;
- enforcing cancellation, timeout, drain, log/result masking, completion, and retry semantics;
- returning the terminal result through the Run and Invocation APIs.

Core does not import or interpret any SDK package, SDK context, module envelope, or SDK compatibility version. It only requires the resulting App bundle to satisfy the Core App runtime interface.

## Runtime and worker capability boundary

Browser and mobile execution spans two ownership areas:

- the App and its dependencies own how they use Playwright, Puppeteer, a mobile bridge, and application-level HTTP behavior;
- Core owns the pinned launcher, `runsOn` worker requirements, label matching, Job lease, bundle delivery, and execution lifecycle;
- a self-hoster or downstream fleet manager owns the actual worker image, browser binaries, mobile devices, capacity, and autoscaling.

In Core, capability and label vocabulary already participates in worker matching. An Application SDK must not redefine Core `capabilities`, `runsOn`, worker labels, or WorkerPool as its own context negotiation protocol. SDK-specific feature discovery, if required, remains internal to the App and must not override Core scheduling.

## Versioning and conformance

The two independently evolving interfaces require separate conformance evidence:

1. Core tests the language wrappers, `WindforceContext`, bundle preparation, launcher, Job-scoped callbacks, and worker lifecycle.
2. Each Application SDK tests its optional context adapter and compatibility fixtures against a supported Core Author SDK/runtime version.
3. Sample Apps prove the composed bundle can be published and executed by an unmodified Core.

An Application SDK may publish a name such as `scraping.ctx/v1` for its own context interface. That identifier is not a Core manifest field and does not require Core to deserialize an SDK schema. Conversely, Core owns changes to `WindforceContext`, private launcher transport, `windforce.json`, and worker execution semantics.

## Change checklist

Before changing either side, verify:

- The App still exposes `main(coreCtx)` through the pinned entrypoint.
- Any SDK adaptation occurs inside the App process and is bundled with the App.
- No Application SDK receives Core service credentials or Worker Plane authority.
- Core does not inspect SDK identity, import SDK vocabulary, or interpret an SDK context version.
- `runsOn` and worker labels remain Core scheduling inputs rather than SDK capabilities.
- Browser or mobile library behavior is separated from worker provisioning and Job lifecycle.
- Core and Application SDK conformance suites each test the boundary they own.

The worker-side sequence that invokes this interface is documented in [Worker execution lifecycle](worker-execution.md). The external HTTP client boundary is documented in [Invocation API](public-api.md). The decision rationale is recorded in [ADR 0021](../adr/0021-keep-application-sdks-opaque-to-core.md).
