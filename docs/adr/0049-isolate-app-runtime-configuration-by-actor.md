# ADR 0049: Isolate App runtime configuration by actor

## Status

Accepted

## Context

App-scoped runtime configuration is intentionally exact-path and release-owned, but one App-scoped path is shared by every invocation of that App. Interactive Apps that maintain a user's external-provider session need durable Secret Variables and non-secret connection metadata without allowing one authenticated subject to select or enumerate another subject's data. Using a workspace as a human identity is incorrect, putting credentials in Run input leaks them into durable invocation records, and storing a subject-indexed map in one App Secret creates avoidable concurrency and isolation failures.

## Decision

Core adds the generic `actor` runtime-configuration scope. A Release declares exact logical actor paths in `runtimeAccess`. At every read or write Core requires the authenticated Run subject in `permissionedAs`, hashes that subject, and maps the logical path into an opaque App-owned physical namespace. Apps and browser clients provide only the logical path; they cannot provide or override the namespace key.

Actor-scoped targets retain the same exact-path, storage-class, Job-attempt, lease, idempotency, revision, masking, lifecycle, and audit enforcement as App-scoped targets. Actor scope is available to direct Job-scoped Variable and Resource reads and writes. It does not add wildcard grants, collection queries, cross-actor lookup, or actor references inside invocation input. Admission therefore pins declared actor targets without reading or expanding actor data; the runtime resolves only the current subject after a Worker owns the Job.

Authenticated control-plane callers may provision an actor-scoped App Variable by specifying `scope: actor` and an App key. Core derives the namespace from the validated workspace principal subject. Secret values remain encrypted by the configured SecretBackend and are never returned through control-plane reads.

The public App authoring SDK may describe actor-scoped targets structurally, but it remains an opaque App dependency. Core owns manifest validation, subject binding, physical namespacing, runtime authorization, and storage.

## Consequences

Multi-user Apps can keep small per-user connection sessions in Core runtime configuration without using workspace identity or a separate database. Existing Workspace and App behavior remains unchanged, including the historical default of Workspace scope for reads and App scope for runtime writes. Physical actor paths can appear in operator-owned inventory and audit storage, but contain only a one-way subject digest and are never exposed through the App runtime result.

Actor scope is still bounded runtime configuration, not a product database. Apps requiring lists, joins, search, large content, high-frequency writes, or multi-object transactions must use an external data store.
