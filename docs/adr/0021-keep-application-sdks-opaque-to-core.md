# ADR 0021: Keep Application SDKs opaque to Core

## Status

Accepted (2026-08-02).

## Context

An App may use any SDK or no SDK. Scraping, Playwright, Puppeteer, mobile, and future libraries are application dependencies, while Windforce Core is a generic self-hosted execution engine. SDK choice must not become an admission, scheduling, bundle transport, launcher, or completion input.

Without an explicit boundary, an Application SDK can appear to own Core scheduling, WorkerPool, source synchronization, launcher selection, Job credentials, or completion. The opposite mistake is also possible: Core can start interpreting SDK package identities, contexts, module envelopes, Playwright or Puppeteer semantics, and SDK-owned versions. Either direction couples Core execution to application dependencies.

Core already has an App runtime interface: admission pins a Deployment, the Worker fetches the immutable execution bundle, the selected language launcher constructs `WindforceContext`, and the pinned entrypoint runs as `main(ctx)`. The TypeScript host context exposes low-level HTTP as `ctx.http.fetch`. A scraping SDK may need the intentionally different fourth-generation `httpService.get/post/...` author API, but that difference does not require another Core runtime or a new Worker protocol.

Browser and mobile execution add another ambiguity. The application library controls browser or mobile operations, Core controls execution and worker matching, and a self-hoster or downstream fleet manager supplies the actual runtime capacity. Treating all three as SDK responsibility would erase those operational boundaries.

## Decision

1. **Core owns one SDK-neutral App runtime interface.** The pinned App entrypoint receives the Core-owned context and returns the terminal Action value. SDK selection is not part of this interface. Core owns the context meaning, private launcher transport, Job-scoped authorization, bundle and launcher lifecycle, logging, result collection, cancellation, timeout, and completion.
2. **Core does not classify App SDKs.** An App may use any number or kind of SDKs, or none. Core does not inspect package identity, negotiate SDK versions, deserialize SDK contexts, or branch execution by SDK.
3. **The Core Author SDK is an implementation helper.** Core injects and fingerprints its language-specific Author SDK during release publication. An App may use it directly, wrap it through another library, or expose the required entrypoint without importing its helper API. Runtime acceptance is based on the App interface, not SDK imports.
4. **Application SDKs are opaque App dependencies.** Their code and dependencies are prepared into the immutable execution bundle and run under the existing Core launcher. Any context adaptation occurs inside the App process and remains invisible to Core.
5. **Private process transport stays private.** Domain code must not read or forward `WF_TOKEN`, construct Core callback URLs from `WF_BASE_URL`, call `/worker/v1`, or write queue state. Only Core-owned launcher and Author SDK glue may translate private process transport into public context capabilities.
6. **Application author APIs remain application-owned.** A scraping SDK, for example, owns `ScrapingContext`, `inputJson`, `httpService`, logger behavior, compatibility, migration decisions, versioning, fixtures, and documentation. Core's `ctx.http.fetch` remains a low-level host capability; any SDK may wrap it or a host-supplied native bridge without Core awareness.
7. **Scheduling remains Core-owned.** `runsOn`, worker labels, claim, lease, heartbeat, and completion are Core semantics. An Application SDK must not reuse Core capability vocabulary as its own context negotiation protocol.
8. **Browser and mobile responsibilities are split.** App code and its dependencies own Playwright, Puppeteer, mobile bridge, and application HTTP behavior. Core owns launcher selection and eligible-worker routing. The self-hoster or downstream fleet manager owns worker images, browser binaries, mobile devices, capacity, and autoscaling.
9. **Conformance is federated, not inverted.** Core tests its SDK-neutral runtime interface. Each Application SDK tests its optional adapter against supported Core versions and supplies executable sample or fixture Apps. Core CI is not made dependent on an SDK-owned schema, and an SDK release does not redefine the Core host interface.
10. **SDK names stay outside Core.** `WindforceContext` is the Core surface. A name such as `scraping.ctx/v1` identifies an Application SDK surface only and is not added to `windforce.json` unless a future Core-wide decision deliberately introduces a provider-neutral extension mechanism.

## Consequences

- Existing fourth-generation scraping modules can be supported by an adapter in the scraping SDK without adding another runtime service to Core.
- Core can execute TypeScript Apps that use scraping, Playwright, Puppeteer, or mobile libraries while remaining unaware of their domain context and module envelopes.
- SDK evolution and Core execution evolution remain independently versioned, with compatibility proven by SDK-owned fixtures and end-to-end sample Apps.
- A worker image still has to contain the required browser or mobile runtime, and the App still has to declare the appropriate Core `runsOn` labels. The SDK alone cannot make an incompatible worker eligible.
- Private `WF_*` transport can change behind the Core Author SDK without becoming a cross-repository application API.
- Adding SDK-specific methods to Core context, transferring Job credentials to an SDK, or making Core interpret an SDK identity or context version requires a new Core ADR because it reverses this boundary.

## Rejected alternatives

- **Teach Core `ScrapingContext`, `httpService`, or any other SDK surface.** Rejected because it couples the generic engine to application dependencies and makes Core releases gate SDK evolution.
- **Make an Application SDK a second runtime or worker controller.** Rejected because source sync, bundle fetch, launcher, claim, lease, cancellation, and completion already belong to Core.
- **Let Application SDK code consume `WF_*` variables directly.** Rejected because it exposes private credentials and transport details, prevents least-privilege evolution, and bypasses the Core context interface.
- **Make Core CI consume an SDK-owned HostContext JSON Schema.** Rejected because the dependency points backward. Core owns and tests its host interface; Application SDKs consume that interface and own their adapters.
- **Treat Playwright, Puppeteer, or mobile support as only an SDK capability.** Rejected because library semantics, worker eligibility, and fleet capacity belong to different owners.
