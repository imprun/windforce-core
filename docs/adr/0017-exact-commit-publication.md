# ADR 0017: Make exact commit publication a server-enforced precondition

## Status

Accepted (2026-07-29).

## Context

A developer-facing client can inspect a local Git worktree, resolve `HEAD`, synchronize the configured remote branch, compare the returned commit, and then publish. That client-side comparison does not close the race between synchronization and publication: another caller can synchronize a newer branch head before the first caller publishes, causing the latest release candidate to differ from the commit the developer reviewed.

The server already stores immutable release candidates by workspace, Git source, and commit. Publication intentionally selects the latest synchronized candidate rather than allowing a client to select arbitrary historical source material. We need optimistic concurrency without changing that ownership boundary.

## Decision

1. `POST /api/w/{workspace}/git_sources/{id}/sync` accepts an optional `expected_commit`.
2. The server resolves and validates the remote branch as before, but it does not save the release candidate or update the source synchronization marker when the resolved commit differs from `expected_commit`. It returns `409 Conflict`.
3. `POST /api/w/{workspace}/git_sources/{id}/deploy` accepts an optional `expected_commit`.
4. Publication still selects the latest synchronized candidate. When its identity does not match `expected_commit`, the server returns `409 Conflict` before building or activating an execution bundle.
5. The existing `commit` request field remains rejected. `expected_commit` is a precondition, not a historical release selector.
6. Omitting `expected_commit` preserves the existing low-level operator workflow.
7. A successful publication returns the immutable `release_id` selected by the server, together with the published commit and bundle digest. The CLI treats a success response without that identifier as an incompatible Cell response instead of guessing from release history.

## Consequences

- `wf app publish .` can guarantee that the active release uses the exact local commit it inspected, provided that commit is the selected remote branch head.
- A concurrent synchronization becomes a visible conflict instead of silently changing the published revision.
- The server remains authoritative for remote source access, validation, immutable candidate storage, execution bundle preparation, and active release mutation.
- Synchronization can leave an unreferenced immutable source snapshot when the branch precondition fails, but it cannot change the source marker, release candidate, or active release.
- `wf app publish` can hand its exact result directly to `wf release view`; it does not need a race-prone follow-up lookup for the newest release.
