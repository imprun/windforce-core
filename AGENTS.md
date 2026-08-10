# AGENTS.md

windforce-core — a small source-sync runtime and execution engine for
Windforce-style apps. Go + PostgreSQL queue; TypeScript, Python, and Go
actions deployed from git.

This file is the canonical agent guide for this repository. `CLAUDE.md` only
imports it — rules live in one place.

## Identity and scope

windforce-core is a **self-sufficient execution engine**: source sync →
catalog/releases → run/job queue → execution. A self-hoster bringing their own
workers must be able to run everything in this repository with no external
service.

The scope discipline of [docs/adr/0001-scope.md](docs/adr/0001-scope.md)
applies to all changes:

- **In scope** (execution semantics): source sync, catalog and release
  publishing, the run/job queue, the ctx-first `main(ctx)` execution contract,
  the worker matching protocol (labels, worker registration, claim, heartbeat),
  outbound webhooks, the execution API and SDKs, the embedded admin UI.
- **Out of scope** (belongs to downstream products and adapters): account and
  multi-tenant SaaS management, billing and quota, managed worker fleets and
  autoscaling, product consoles beyond the embedded UI, product-specific
  vocabulary and integrations.

Litmus test for any new feature: **"does a self-hoster need this?"** If yes,
it may belong here. If it only makes sense for a hosted commercial service, it
does not — keep the engine generic and let adapters or downstream products own
it.

## Vocabulary

Neutral engine vocabulary only: App, Action, Run/Job, Worker, Release,
Workspace. Do not introduce product or brand vocabulary into engine code,
APIs, or docs. Adapters may map external vocabulary onto App/Action, never the
other way around.

Workspace is an organizational scoping partition inside one engine instance,
not a tenant isolation boundary — do not design features that treat it as one.
Tenant isolation is obtained by running one engine instance per tenant.

## Worker execution contract

Before changing `internal/worker`, `internal/runtime`, `internal/executor`, `internal/executionbundle`, `internal/remoteworker`, or the release publication pipeline, read [docs/concepts/worker-execution.md](docs/concepts/worker-execution.md). It is the current canonical description of how a pinned Job becomes a process execution.

Preserve these invariants:

- Release publication, not Job execution, reads the Source Store, installs dependencies, injects SDKs, compiles source, validates the entrypoint, and publishes the content-addressed execution bundle.
- A Job pins the complete Deployment and execution bundle digest at admission. A worker never resolves the current active release again.
- A worker obtains and validates the pinned execution bundle before it creates per-Job runtime files or starts Bun, Python, Go, or an adapter command.
- `input.json` is invocation data, not application source. The launcher imports or executes the entrypoint from the fetched worker-local bundle cache.
- Workers never clone Git or receive repository credentials. A missing, corrupt, or incompatible execution bundle fails the Job; there is no Git or build fallback.
- Cache publication remains content-addressed and crash-safe: fetch into a temporary sibling, verify, atomically promote, and write the ready marker only after validation.
- Local and remote workers have the same execution semantics. Remote workers claim, fetch artifacts, append logs, and complete Jobs only through `/worker/v1`; they do not access the server database or artifact filesystem directly.
- `scriptLang` is normalized once to `typescript`, `python`, or `go`; an empty value means TypeScript and every unknown value is rejected before preparation. Never add an implicit launcher fallback.
- TypeScript publication statically verifies a named `main` export and builds the dependency graph without importing or executing the App.
- On shutdown, a worker stops claiming, reports `draining`, lets the active Job run for `--drain-timeout`, cancels it only after that deadline, completes the lease, and then deregisters. Offline is represented by registry absence.

Any change to this ordering, pinning boundary, bundle identity, ready-marker meaning, or Worker Plane artifact protocol changes execution semantics and requires documentation, focused lifecycle tests, and an ADR.

## App and SDK boundary

Before changing the language wrappers, `internal/sdk`, `WindforceContext`, private `WF_*` transport, or App entrypoint behavior, read [docs/concepts/app-runtime-interface.md](docs/concepts/app-runtime-interface.md) and [ADR 0021](docs/adr/0021-keep-application-sdks-opaque-to-core.md).

Core owns the generic `main(coreCtx)` host interface, Core Author SDK, launcher transport, Job-scoped access, and worker lifecycle. Every SDK used by an App is an opaque App dependency. Core must not inspect or classify SDK identity, import an SDK context, understand its module envelope, negotiate its version, or transfer service credentials and Worker Plane authority to it. `runsOn` and worker labels remain Core scheduling inputs regardless of whether the App uses a scraping SDK, Playwright, Puppeteer, a mobile SDK, or no SDK.

Some App repositories generate a canonical deployment artifact instead of storing `windforce.json` in author source. Their external builder owns `--describe` or equivalent SDK-specific discovery, schema-file emission, dependency bundling, and creation of the deployment Git or snapshot. Core begins at the configured canonical manifest file plus entrypoint boundary (`windforce.json` is only the default filename) and must not execute author code to discover an SDK manifest.

Release-owned placement defaults and operator-owned execution placement are separate. Before changing worker-tag or worker-label resolution, read [docs/concepts/execution-placement.md](docs/concepts/execution-placement.md). New Jobs pin Action policy, App policy, and manifest defaults in that precedence order; published and historical releases must not store operator overrides.

## Decisions

Engine contract decisions are recorded as public ADRs in
[docs/adr/](docs/adr/). Add an ADR when changing execution semantics, the
`windforce.json` manifest, HTTP API surfaces, the webhook contract, or the
worker protocol. General docs describe the current contract only; history and
rationale live in ADRs.

Do not hard-wrap markdown prose: one paragraph per line, soft wrap. Convert
existing files only when touching them — no bulk reformatting.

## Workflow

- Every commit is signed off under the DCO: `git commit -s`. See
  [CONTRIBUTING.md](CONTRIBUTING.md).
- Verify before submitting: `make fmt`, `make build-smoke`, `make test`; for web UI
  changes also run `make web-test`, `make web-typecheck`, and
  `make web-embed-verify`.
- Conventional commit style: `feat: ...`, `fix: ...`, `docs: ...`.
- Releases are SemVer `v*` tags; pre-1.0 minor releases may break.
- Never commit secrets, tokens, internal endpoints, or local state
  (`.windforce-core/`, `.env`).
