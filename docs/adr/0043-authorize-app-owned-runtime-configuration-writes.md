# ADR 0043: Authorize App-owned runtime-configuration writes

- Status: Accepted
- Date: 2026-08-16
- Discussion: [#171](https://github.com/imprun/windforce-core/discussions/171)
- Decision: [owner-approved contract](https://github.com/imprun/windforce-core/discussions/171#discussioncomment-18040542)
- Issue: [#236](https://github.com/imprun/windforce-core/issues/236)

## Context

Variables and typed Resources are currently operator-owned Workspace configuration. A Release may declare paths that an Action can resolve, but an Action cannot persist a refreshed browser session, an external connection profile, or another bounded piece of runtime configuration for its own App. Deployments therefore need an unrelated database or an out-of-band operator update even when the required state is one small object.

Core must support this use case in standalone, self-hosted, and hosted Cells without becoming a general-purpose App database. The contract must preserve existing Releases and reference syntax, prevent a mutable Resource from planting a new read capability, bind writes to the live Job attempt, and keep newly written Secret plaintext out of subsequent logs and results.

## Decision

### Bounded runtime configuration, not an embedded database

Core stores three App-owned runtime-configuration classes: plain Variables, Secret Variables, and non-secret typed Resources. It offers exact path reads and writes, optimistic concurrency, revision history metadata, tombstone/revoke lifecycle controls, and audited deletion. It does not offer arbitrary queries, indexes, aggregation, collection scans, large blobs, high-frequency event storage, multi-object transactions, or a database-compatible protocol. Apps needing those capabilities use an external database Resource or connector.

The following v1 limits are protocol constants and produce stable typed errors. Core never truncates a persisted value or silently drops required mask patterns:

| Limit | Value | Rationale |
| --- | ---: | --- |
| canonical Variable or Resource value | 1 MiB | matches the existing resolver and Worker capability request boundary |
| encrypted Secret candidate envelope | 2 MiB | bounds backend/AAD and encoding overhead for a maximum plaintext value |
| paths across all read/write declarations in one Action | 256 | preserves the existing runtime-access cap |
| path length / segments | 512 bytes / 32 | preserves the existing portable path grammar |
| references resolved from one value | 256 | preserves the existing resolver bound |
| reference nesting depth | 16 | preserves the existing resolver bound |
| runtime writes per Job attempt | 256 | bounds idempotency rows, audit, and dynamic masking growth |
| non-empty JSON string leaves in one Secret write | 2,048 | covers representative Playwright `storageState` while bounding traversal |
| one dynamic mask pattern | 64 KiB | keeps scanning bounded; larger JSON values rely on required leaf patterns |
| distinct dynamic mask patterns per attempt | 4,096 | permits two maximum-leaf writes without unbounded matcher growth |
| total dynamic mask pattern bytes per attempt | 4 MiB | bounds memory and worst-case streaming scan work |

The merge gate includes a representative Playwright `storageState` fixture and a worst-case 4 MiB/4,096-pattern log benchmark. Registration and chunk-spanning masking must remain linear in input bytes plus registered pattern bytes, without exposing plaintext. Limit changes require a protocol/ADR revision because remote Workers and Core must fail identically.

### Explicit owner scope and compatibility

Every Variable and Resource has an explicit owner scope, `workspace` or `app`. An App-owned row also stores its App key independently from its path. The same path in both scopes names two different objects. Resolution never falls back from one scope to the other and has no shadow order.

Existing manifest string entries remain Workspace-scoped forever. Existing `$var:<path>` and `$res:<path>` references also remain Workspace-scoped forever. New declarations use `{ "scope": "workspace|app", "path": "..." }`; Variable write declarations additionally pin `storage: "plain|secret"`. New references are `$var@workspace:<path>`, `$res@workspace:<path>`, `$var@app:<path>`, and `$res@app:<path>`.

Runtime writes are App-scoped in v1. Workspace mutations remain operator Control API operations. Existing Releases therefore gain no write authority. Admission normalizes and pins the complete read/write declaration set in the Run and every Job attempt. App identity comes from the pinned Job, never from an SDK request.

ResourceType definitions remain Workspace-shared. Each Resource instance pins the exact type name and version used for validation. Changing the pinned version is an explicit operator migration, not an incidental schema lookup.

### Reference authority and non-escalation

Operator-owned Workspace Resources are trusted capability bearers for compatibility: their nested references may expand the admitted read closure exactly as before. App-owned Resources are data, not capability bearers. Resolving one never adds a nested target to the Job's authority. Every nested target must already be in the Job's pinned readable closure or have been reached through a trusted Workspace Resource.

Before an App-owned Resource write commits, Core parses and validates all references and requires each target to be a subset of that same Job attempt's pinned readable closure. Schema validation, reference authorization, value mutation, revision allocation, idempotency result, and success audit commit atomically. This prevents same-App cross-Action and cross-App capability planting.

### Attempt-bound authorization, OCC, and idempotency

Each runtime callback is authenticated by an opaque Job/attempt credential and is accepted only while the Job is running with the matching current lease. Core checks the pinned App, exact scope/path, object class, and Variable storage class. Terminal, cancelled, stale-attempt, lease-lost, foreign-App, and foreign-Workspace callbacks fail closed with stable machine-readable codes.

Every mutation requires an `operationId`. SDKs generate a random ID once per logical call; transport retries reuse it and a new attempt creates a new ID. Core stores a canonical request fingerprint. Repeating the same ID and payload returns the original result and revision without another successful audit. Reusing the ID with a different canonical payload returns an idempotency conflict.

A Secret mutation stores through a deterministic, attempt/operation/fingerprint-bound candidate reference before the state transaction. The published row seals that candidate reference together with the backend's opaque value so record-bound encryption is resolved with the exact same AAD. Exact retries address the same candidate, while a conflicting payload cannot overwrite the currently published Secret before OCC/idempotency rejects it. [ADR 0051](0051-collect-orphaned-runtime-secret-candidates.md) defines optional prefix-scoped, versioned mark-and-sweep cleanup for side-effecting external Secret backends; the Database backend creates no external orphan.

Reads return the current positive revision. A mutation may include `expectedRevision`; when present it is exact compare-and-swap. A failed comparison has no mutation, no idempotency success record, and no success audit. Omitting it means replace the current value or create revision 1.

### Worker-local Secret write proxy and dynamic masking

An App never sends a Secret write directly to Core. The Worker injects a loopback-only, Job/attempt-bound runtime-configuration endpoint. For a Secret write the Worker validates the request bounds, derives every required exact mask pattern, atomically registers them in a concurrency-safe per-Job registry, and only then forwards the authorized write to Core. Registration followed by a rejected Core write may over-mask that Job; persistence before complete registration is forbidden.

Reads use the same boundary. Core returns only SHA-256/base64url digests of resolved Secret string leaves in a private response header. The Worker buffers the bounded JSON response, matches exact string leaves to those digests, registers the matching plaintext locally, strips the header, and only then releases the response to the Action. This protects a later Job that reads a Secret created by an earlier Job without placing plaintext in transport metadata or treating every ordinary string as secret.

For valid JSON plaintext, required patterns are every non-empty string leaf, including short values. The exact whole plaintext is also registered when it fits the single-pattern limit. For non-JSON plaintext, the exact whole plaintext is required and therefore must fit. JSON traversal, pattern counts, distinct bytes, payload size, and per-attempt write count are checked before registration; if all required patterns cannot fit, the write fails before persistence.

The log stream, terminal result, Action errors, and Worker diagnostics all consult the same registry. Masking is forward-only: already persisted output is not rewritten. Exact and JSON-escaped forms are masked across stream chunk boundaries. Derived, encoded, hashed, compressed, or otherwise transformed values are not an automatic v1 guarantee and must not be described as one.

The proxy exists for local, remote, and adapter execution. Its credential and registry expire with the attempt/lease and are never placed in logs, results, audit, provisioning exports, or operator-readable Job input.

### App lifecycle

Graceful App tombstone blocks new admission, new claims, and new runtime writes. An already running attempt with a valid lease may continue resolving and writing; the current in-process `ctx.human.wait(...)` remains that attempt. Queued Jobs, retries, and future resumed attempts cannot claim. Data remains until an audited purge after all valid leases are gone.

Emergency revoke immediately blocks subsequent resolve and write callbacks even for a currently valid lease and requests cancellation. It cannot retract plaintext already delivered into Action memory. Purge remains a separate audited operation; force purge requires an explicit operator confirmation and records that valid leases may have existed.

Provisioning and backup preserve owner scope, App key, revision, ResourceType version, tombstone/revoke state, and safe audit provenance. They never export Secret plaintext. Import validates all capabilities and applies the runtime-configuration batch atomically within its storage backend.

### API and ownership boundary

Core owns persistence, scope and reference authorization, attempt/lease validation, OCC/idempotency, audit, lifecycle enforcement, runtime callback APIs, OpenAPI, generic SDK surfaces, and the embedded operator UI. A hosted control plane may project desired configuration or lifecycle intent through those APIs, but commercial plans, billing, organization membership, and cross-Cell orchestration remain outside Core.

Control API and UI use operator language and distinguish Workspace-owned from App-owned objects. Runtime APIs expose only the calling attempt's pinned exact targets. Error bodies use a stable `code`, safe target identity, current revision when applicable, and retryability; they never echo Secret plaintext.

## Consequences

- Existing Releases, rows, archives, and legacy references retain Workspace semantics without reinterpretation.
- Apps can persist small operational state without introducing a database dependency, while arbitrary data workloads remain outside Core.
- Mutable App-owned Resources cannot grant their readers new authority.
- Secret persistence adds a Worker-local proxy and a dynamic per-Job masking registry to the execution hot path.
- Local and PostgreSQL stores gain explicit ownership, revision, operation, audit, and lifecycle state with equivalent behavior.
- App deletion becomes a staged lifecycle rather than an immediate data delete.
- The first end-to-end fixture is a Playwright session written as an App-owned Secret Variable and referenced by a non-secret App Resource, then consumed by a separately declared Action without leaking the session into observable output.
