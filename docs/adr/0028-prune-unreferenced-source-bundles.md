# ADR 0028: Prune only unreferenced source bundles after a grace period

## Status

Accepted (2026-08-04).

## Context

Source synchronization materializes an immutable source snapshot before it updates the transactional release catalog. A publication transaction failure, an `expected_commit` conflict, or a newer synchronization can therefore leave a completed snapshot that no current state references. Keeping every such snapshot forever causes the Source Store to grow without providing rollback or execution value.

Deletion must not weaken release history or race normal synchronization. A source snapshot may still be needed by the active release, immutable release history, the latest synchronized release candidate, or the source release marker. A directory without a valid completion marker is not safe to classify automatically.

## Decision

1. Core runs a Source Store retention sweep in `server` and `standalone` modes when `--source-bundle-grace-period` is greater than zero.
2. The reference set is the union of active deployments, immutable release history, latest release candidates, and source release markers in one loaded catalog snapshot.
3. The filesystem store considers only directories with a valid `.windforce_clone_complete` marker whose identity matches its canonical directory path.
4. A completed snapshot is eligible only when it is absent from the reference set and its `completedAt` is older than the grace-period cutoff.
5. Invalid or incomplete snapshots are retained for operator inspection. The sweep never guesses their identity or age.
6. The default grace period is seven days and the default sweep interval is one hour. A zero grace period disables the sweep.
7. `--source-bundle-retention-dry-run` reports eligible, referenced, recent, and invalid counts without deleting data.
8. Repeated sweeps are idempotent. Deletion is limited to the exact commit directory represented by the validated marker.

## Consequences

- Every published release remains recoverable from its source snapshot because release history is a permanent reference.
- Transaction leftovers and superseded unpublished candidates are reclaimed after a conservative delay.
- A catalog read failure prevents that sweep from deleting anything.
- Operators can inspect dry-run counts before enabling deletion or when investigating malformed store contents.
- Execution Artifact Store retention remains a separate concern because Jobs pin prepared bundle digests independently of Source Store snapshots.
