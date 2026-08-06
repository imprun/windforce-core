---
title: Worker execution lifecycle
description: The canonical sequence from a pinned Job to bundle fetch, launcher startup, and result completion.
---

This document is the current canonical description of worker execution in Windforce Core. It defines the ordering and ownership rules that runtime implementations, tests, and coding agents must preserve.

> Trace implementation status (2026-08-06): workers now implement the accepted ADR 0029 continuity rules, including durable creation context, polling-parent isolation, recovery links, and launcher injection.

## The central rule

A worker executes an immutable execution bundle pinned into the Job. It does not execute directly from Git, the Source Store, the active release, or a JSON file.

`input.json` contains invocation input. Application source and prepared dependencies live separately in the worker-local execution bundle cache. The launcher starts only after that bundle has been fetched and validated.

## Two distinct lifecycles

Release publication and Job execution are deliberately separate:

| Phase | Owns | Must not do |
| --- | --- | --- |
| Sync | Fetch an exact Git commit, validate source metadata, and materialize the immutable source snapshot in the Source Store. | Install runtime dependencies or select an active release. |
| Publish Release | Fetch the synchronized snapshot, prepare dependencies and SDKs, validate the entrypoint, publish the complete tree by digest, and select the release. | Create or execute a Job. |
| Run admission | Resolve the active release once and pin its complete Deployment plus optional versioned W3C creation context into the Run and Job. | Fetch source or execute application code. |
| Worker execution | Claim the pinned Job, select its stored execution context instead of an ambient polling context, start the attempt span, resolve effective input, fetch and validate its execution bundle, launch the entrypoint, and complete the Job. | Read Git, prepare dependencies, resolve the active release again, or parent execution from claim transport. |

## Canonical execution sequence

The ordering below is part of the execution contract:

```text
Control Plane                                  Worker
-------------                                  ------
Sync exact Git commit
  -> Source Store snapshot

Publish Release
  -> install/build/inject SDK
  -> validate entrypoint
  -> publish sha256 execution bundle
  -> active immutable release

Run admission
  -> pin Deployment + bundle digest
  -> continue or create trace context
  -> create Run + Job with trace context
                                                claim Job + lease
                                                start lease heartbeat
                                                select stored Job context, never poll context
                                                attempt 1: continue creation trace
                                                attempt >1: root + creation link
                                                missing context: start Worker root
                                                resolve effective input
                                                open pinned execution bundle
                                                  -> validated cache hit, or
                                                  -> fetch digest to temp
                                                  -> verify and atomically promote
                                                  -> validate preparation fingerprint
                                                  -> write ready marker
                                                resolve entrypoint inside bundle
                                                create per-Job directory
                                                write input.json + launcher wrapper
                                                start Bun/Python/Go/adapter command
                                                stream masked logs
                                                read result.json
                                                complete or fail Job
```

In the implementation, the Processor passes `job.Payload.PinnedDeployment()` to the runtime Runner. Before that execution path is instrumented, it restores the optional creation context pinned at Admission or starts a Worker root for a legacy, direct, or test Job. `Runner.Run` calls `openExecutionBundle` before the canonical executor creates its per-Job directory or writes `input.json`. The executor then writes a language wrapper, injects the current W3C carrier through private transport, and starts the selected runtime. For TypeScript, `bun run wrapper.ts` imports the absolute entrypoint path inside the fetched bundle and calls `main(ctx)`.

`scriptLang` is normalized to exactly `typescript`, `python`, or `go`. An omitted value defaults to TypeScript for manifest compatibility; any other value is rejected before preparation and never falls through to Bun. During TypeScript publication, Core uses Bun's static scanner to require a named `main` export and then runs `bun build` over the entrypoint dependency graph. Neither step imports or executes the App, so publication cannot trigger App top-level effects.

## Filesystem separation

The worker uses two different locations:

```text
<worker-cache>/execution-bundles/<digest>/
  main.ts
  node_modules/
  .ready
  .windforce-execution-ready
  ...prepared application tree

<temporary-job-dir>/
  input.json
  wrapper.ts
  result.json
```

The bundle cache is reusable and addressed by the pinned digest. The Job directory is disposable and contains only execution-specific input, wrapper, and result files. The wrapper imports the entrypoint from the bundle cache; copying application source into the Job directory is not required.

The Core launcher constructs `WindforceContext` and calls the App entrypoint. Core does not inspect which SDKs the App uses. Any SDK runs as an opaque dependency inside the App process; it does not fetch the bundle, select the launcher, claim the Job, or receive Worker Plane authority. See [App runtime interface and SDK boundaries](app-runtime-interface.md).

## Bundle acquisition and cache safety

On every Job, the runtime requires a non-empty pinned bundle digest. A cache hit is accepted only when `.windforce-execution-ready` contains that digest and the bundle preparation fingerprint is compatible with the worker runtime.

On a cache miss, concurrent requests for the same digest are coalesced. The bundle is fetched into a temporary sibling directory, validated, and atomically promoted to its digest-addressed cache directory. The ready marker is written only after the runtime fingerprint has been accepted. A canceled, missing, corrupt, or incompatible bundle produces a named bundle failure; execution must not fall back to Git, dependency installation, compilation, or a different release.

## Local and remote workers

Local and remote workers preserve the same ordering and pinned-bundle semantics.

- A local worker obtains the digest from the configured Execution Artifact Store and copies it into its worker-local cache.
- A remote worker requests `GET /worker/v1/artifacts/{digest}`. For a managed credential, the client attaches the current Job, workspace, and worker lease context and Core verifies that the requested digest is pinned by that owned Job. Core reads the configured Artifact Store and streams a tar archive; the worker extracts it into a temporary directory, verifies the digest on the POSIX deployment target, and atomically promotes it into its local cache.
- A remote worker does not mount Core's database, Source Store, or Artifact Store filesystem. Repository credentials and the server encryption root remain in Core.

The current server-side Artifact Store implementation is filesystem-backed. This is independent of remote worker transport: Core owns that filesystem and exposes digest-addressed artifacts to remote workers through the Worker Plane.

### Managed remote-worker authority

The static worker token remains a trusted self-hosted compatibility path. A
managed remote worker instead uses a generation-specific `wfr_` credential
whose group, exact offered labels, and workspace allowlist are persisted by
Core. Registration binds the live worker record to that credential ID and
generation. Claim selection then applies the existing tag and AND-label rules
inside the credential's workspace scope.

Credential rotation activates a new generation before the old generation is
revoked. Revocation blocks new registration and claims, but a revoked
generation may finish its already owned lease until its explicit drain
deadline. Separately, a persisted group run state of `draining` returns no new
managed claim while leaving existing lease heartbeat, log, cancellation, and
completion behavior unchanged. See [ADR 0025](../adr/0025-managed-worker-credentials-and-group-drain.md)
and the [Worker management API](../api/worker-management.md).

## Worker shutdown lifecycle

A registered worker is `active`. When its process receives interrupt or termination, it stops making new claims and updates the same registry record to `draining`. Registry and Job lease heartbeats continue while an already claimed Job keeps its execution context for the configured `--drain-timeout`, which defaults to 30 seconds. If it finishes within that period, the worker writes the terminal result normally. If the deadline expires, Core cancels the launcher, records the resulting failure, and still performs lease-fenced completion with a shutdown-independent completion context.

After the active Job has completed or been canceled, the worker deregisters. `offline` is represented by absence from the live registry, not by a retained status row. Standalone and independent worker commands install the same interrupt and SIGTERM handling, and local and remote backends carry the same `active` and `draining` registry states.

## Input and secret boundary

The worker resolves the effective input before launch. Depending on its backend, resolution may be performed by the in-process store or returned as a prepared claim by the Worker Plane. Runtime Variables, Resources, InputConfig values, and Secret references become the effective input used for the Job, while secret values are registered for log and result masking.

This input preparation does not change the bundle identity. The same pinned bundle can execute different Job inputs. `input.json` is never a source-distribution mechanism and must not contain repository credentials or an alternate application revision.

## Completion and failure semantics

The launcher writes the terminal action value to `result.json`. The worker streams masked logs while the process runs, reads the result, masks secret values again, and completes the leased Job as succeeded or failed. During a Phase 1 `HumanTask` hold, the launcher has not produced a terminal result: the same process and lease remain running until the decision returns or a terminal cause interrupts it. Legacy `waiting_human` completion is suspend scaffolding and is not the `ctx.human.wait` path. Process failure is represented as a Job result; harness failures such as bundle fetch or launcher startup are converted into structured runtime errors.

Action timeout, operator cancellation, lease loss, and worker shutdown are recorded as distinct execution interruption causes. Any pending held HumanTask is terminated with the corresponding stable cause. See [HumanTask hold](human-task-hold.md).

Publishing a newer release while a Job is queued or running does not change that Job. Retry attempts continue from the Deployment snapshot already pinned into the Run and Job unless a higher-level workflow explicitly admits a new Run.

Application stdout and stderr are one masked Job-log stream, while the terminal
action value remains a separate result. Offset-based live following, service
logs, browser artifacts, and the rule against exposing Bun Inspector on shared
workers are defined in [Execution observability and debugging](execution-observability.md).

## Change checklist for maintainers and coding agents

Before accepting a worker or runtime change, verify all of the following:

- The Job still supplies a pinned Deployment and bundle digest to the Runner.
- Bundle open and validation still happen before Job runtime files and process startup.
- No execution path clones Git, installs dependencies, injects SDKs, compiles source, or queries the active release.
- Cache miss handling remains temporary, verified, atomic, and safe under concurrent fetches.
- Cache hits validate both the digest marker and preparation fingerprint.
- Entrypoint containment prevents paths from escaping the fetched bundle root.
- Local and remote worker paths preserve equivalent bundle and completion semantics.
- Managed credentials never broaden their exact labels or workspace scope, and draining groups acquire no new managed leases.
- Shutdown stops new claims, exposes `active -> draining`, preserves the active Job until the drain deadline, and removes the registry record only after completion.
- Logs and results remain secret-masked and lease-fenced.
- Missing, malformed, or oversized trace context never blocks execution. Local, remote, and standalone workers use the stored Job creation context rather than ambient claim transport, start a Worker root when no valid Job context exists, and pass only the effective execution carrier to the launcher.
- A Job is the durable work item and an Attempt is one lease-fenced execution. Attempt 1 may continue the creation trace; lease recovery at `attempt > 1` starts a new root linked to creation without requiring durable previous-attempt context. Idempotency replay does not create an Attempt or replace creation context.
- Log appends remain ordered and reconnectable by byte offset without mixing
  application logs, terminal results, service logs, or binary artifacts.
- HumanTask hold keeps the original process, lease, and worker slot alive; terminal interruption causes cancel the pending task without creating another Job.
- Tests cover bundle publication/fetch, cache behavior, remote extraction, runtime execution, static TypeScript `main` validation, graceful and timed-out drain, and Job failure on bundle errors.

The primary implementation areas are `internal/worker`, `internal/runtime`, `internal/executor`, `internal/executionbundle`, `internal/remoteworker`, and `internal/server/worker_plane.go`. Execution-semantic changes require an ADR in addition to updating this current-state document. Optional trace propagation and independent root creation are defined in [ADR 0029](../adr/0029-optional-trace-context-continuity.md).
