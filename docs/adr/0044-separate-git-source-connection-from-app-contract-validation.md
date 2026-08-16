# ADR 0044: Separate Git source connection from App contract validation

- Status: Accepted
- Date: 2026-08-17
- Issue: [#241](https://github.com/imprun/windforce-core/issues/241)

## Context

Git source registration historically cloned the selected revision, loaded `windforce.json`, validated schemas and lockfiles, returned an App identity preview, and optionally stored an initial execution-placement policy. This coupled a repository connection to one manifest filename and made a source impossible to register until its complete App contract was valid. It also duplicated the validation already performed by Sync.

Core now supports an operator-configured manifest filename through `--manifest-file` or `WINDFORCE_CORE_MANIFEST_FILE`, with `windforce.json` as the default. A connection operation cannot know the App identity without materializing and interpreting that configured file, so registration-time placement is no longer a stable boundary.

Git subprocesses also inherited host credential helpers and askpass configuration. Selecting "no credential" could therefore succeed with a developer workstation's Git Credential Manager, making the saved source appear public and producing behavior that could not be reproduced in another Core Cell.

## Decision

### Register and probe are access-only

Probe and Register validate repository reachability, the selected branch, and safe relative subpath syntax. They do not clone the repository, read a manifest, validate schemas or lockfiles, derive an App key, or prepare a release.

Register repeats the access check before persistence; a successful browser probe is not an authority boundary. Patch repeats the access check when the repository URL, branch, or credential reference changes. A subpath-only patch validates containment without cloning the repository.

The probe response contains reachability, branch existence, remote branch names, and an optional stable error code. It no longer contains a manifest or execution-placement preview.

### Sync owns the source contract

Sync resolves the exact branch commit, materializes the configured subpath, loads the manifest filename selected by Core configuration, validates the manifest, referenced schemas, entrypoint references, and lockfile, stores the immutable source revision, and records the first App identity. A contract failure returns HTTP 422 with `code: git_source_contract_invalid`, a validation check name, and a sanitized operator-facing detail. It does not remove the registered source or mutate its last-synchronized identity.

Publish continues to use only the latest successfully synchronized immutable source revision. It prepares and validates the execution bundle without reading Git.

### Execution placement follows Sync

Registration cannot accept `placement_policy` because no trusted App identity exists yet. The API returns HTTP 422 with `code: git_source_placement_requires_sync` when an old client sends it. After Sync identifies the App, operators use the existing App or Action execution-placement APIs and UI. Those policies remain independent of later Releases.

### Git credentials are explicit

Every Core Git subprocess clears configured credential helpers and HTTP extra authorization headers, disables interactive credential acquisition, and clears inherited Git/SSH askpass hooks. Only the credential explicitly resolved from the source's Workspace variable reference is injected into an HTTP(S) remote URL. Selecting no credential therefore cannot borrow the host user's Git Credential Manager session.

Repository access failures return a stable code and safe guidance rather than raw Git stderr or a credential-bearing URL. The Web UI maps those codes to localized operator guidance while keeping generic API failures opaque.

## Compatibility and migration

Existing registered sources and synchronized revisions remain valid. Existing automation that omits `placement_policy` keeps the same request shape and now avoids the redundant registration clone. Clients that used `ProbeResult.manifest` must obtain App identity from the Sync result. Clients that sent registration-time `placement_policy` must Register, Sync, then patch the identified App or Action policy.

The manifest default remains `windforce.json`; installations using another filename continue to select it with `--manifest-file` or `WINDFORCE_CORE_MANIFEST_FILE`. The filename is consulted only by Sync and later source-contract consumers, not by Probe or Register.

## Consequences

- A repository can be connected while its App manifest is incomplete; the source is not runnable until Sync and Publish succeed.
- Connection failures are reproducible across Cells because host Git credentials are not ambient authority.
- The App registration dialog is smaller and no longer presents placement controls before an App exists.
- Sync becomes the single observable source-contract validation boundary and must retain clear, typed failures.
