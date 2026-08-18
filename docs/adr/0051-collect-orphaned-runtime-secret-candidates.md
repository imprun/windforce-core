# ADR 0051: Collect orphaned runtime Secret candidates

- Status: Accepted
- Date: 2026-08-19
- Issue: [#240](https://github.com/imprun/windforce-core/issues/240)
- Amends: [ADR 0043](0043-authorize-app-owned-runtime-configuration-writes.md)

## Context

ADR 0043 writes an App-owned Secret through a deterministic Job-attempt, `operationId`, and request-fingerprint candidate before the Core state transaction publishes its sealed reference. The built-in Database backend returns ciphertext without creating an external object. A side-effecting backend can create an object and then observe a failed OCC check, an idempotency conflict, a lease race, a Core crash, or an ambiguous timeout. Its candidate is then not reachable from the current Variable row.

Adding a direct delete hook to `Backend` is insufficient. Core may crash before retaining the backend's returned opaque value, and an unconditional delete can race an exact retry or a concurrent publication. A provider-wide scan would also exceed Core's neutral backend boundary and could inspect unrelated objects.

## Decision

### Optional prefix-scoped mark-and-sweep

`Backend.Store` and `Backend.Resolve` remain unchanged. A side-effecting backend that needs cleanup may additionally implement `RuntimeCandidateCleaner`. Its list operation enumerates only the dedicated runtime-candidate namespace in bounded pages and returns privacy-safe metadata: the complete candidate `Reference`, deterministic candidate ID, last-touched time, and an opaque compare-and-delete version. It never returns plaintext, stored Secret material, credentials, or provider-global inventory.

Every `Store` attempt for a deterministic runtime candidate, including an exact same-payload retry, is an idempotent upsert of one candidate object. It refreshes `LastTouchedAt` and advances `Version`. `DeleteRuntimeCandidate` is serialized with `Store` for that candidate and deletes only if the listed version is still current. If delete wins first, a concurrent Store recreates the candidate before publication. If Store wins first, the stale conditional delete is deferred. This is the multi-replica race boundary; Core does not require a singleton collector or distributed leader.

Core supplies the mark roots from current App-owned Secret Variable rows. It opens only the sealed candidate envelope and compares the complete normalized Workspace, kind, and path reference. Provisioning or restore therefore protects a restored current reference without importing collector bookkeeping. A malformed sealed envelope or unavailable live-reference snapshot fails the sweep closed before backend enumeration.

The collector lists only candidates whose last touch is at or before `now - gracePeriod`. The default grace period is 24 hours, the default page size is 100, and at most 1,000 candidates are examined per sweep. A backend must apply the requested age filter when it creates each page. A publication that starts before enumeration refreshes the candidate and removes it from the eligible set; one that starts after enumeration changes its conditional-delete version. A crash or ambiguous Store result that never publishes becomes eligible after the grace period.

### Lifecycle and deletion meaning

Tombstone and revoke do not remove a Variable row, so their candidate remains in the live-reference set. Audited purge atomically removes the published row and makes its external candidate eligible for physical collection after the grace period. Collection is eventual rather than part of the purge transaction: backend unavailability cannot partially roll back the audited Core purge, and a delayed or repeated delete remains safe.

Replacing a Secret revision similarly makes the prior candidate unreachable and eventually collectible. Exact idempotent replay retains one candidate object and one current reference. A same-ID/different-payload conflict or failed CAS may leave a different deterministic candidate, which is collected only after it is old and unreferenced.

### Operation and privacy

The collector runs immediately on startup and then at a configured interval when the selected Secret backend implements the optional capability and cleanup is enabled. Unsupported backends continue with unchanged `Store`/`Resolve` behavior. The Database backend deliberately does not implement the capability because it creates no external object.

Metrics expose only cumulative low-cardinality outcomes: `scanned`, `reclaimed`, `skipped_live`, `deferred`, and `failed`. Logs and errors must not contain plaintext, backend credentials, opaque stored values, list cursors, or compare-and-delete versions. A failed candidate does not stop other candidates in the same bounded sweep; a failed live snapshot stops every delete in that sweep.

## Consequences

- Side-effecting backends have a complete crash-recovery protocol without expanding the base `Backend` interface.
- Local and PostgreSQL state stores provide the same current-reference snapshot and lifecycle behavior without adding collector tables.
- Backend implementations must provide stable prefix-scoped pagination and atomic versioned deletion; a simple unconditional delete adapter is not conforming.
- Physical deletion after purge or replacement is asynchronous and bounded by the configured grace period, interval, and sweep limit.
- A future provider adapter can use its native object generation, ETag, CAS index, or equivalent as `Version`, but provider-specific semantics stay outside Core.
