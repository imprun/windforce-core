---
title: Release and execution lifecycle
description: How Register, Sync, Publish Release, Run admission, and worker execution change system state.
---

The lifecycle has four operator-visible operations: Register, Sync, Publish
Release, and Run. Each operation has a separate responsibility and failure
boundary.

```text
Register source
      |
      v
Sync exact Git commit -----> synchronized revision
      |                         in Source Store
      v
Publish Release -----------> prepared execution bundle
      |                         in Artifact Store
      v
Active release
      |
      v
Run admission -------------> pinned Run + Job
      |
      v
Worker fetch and execute
```

## 1. Register a repository source

Registration records where app source comes from:

- repository URL
- branch
- optional repository subpath used as the app root
- credential reference managed by the Control Plane

Before saving the source, registration verifies repository access, branch and
subpath containment, `windforce.json`, referenced action schemas, and lockfile
requirements. Registration does not create an active release.

The app key is read from `windforce.json`; the repository source name is only an
operator-facing alias. One should not be substituted for the other.

## 2. Sync source

Sync acquires the repository source operation lease and then:

1. Resolves the repository credential inside the Control Plane.
2. Fetches the configured branch and resolves its exact commit.
3. Selects the configured subpath as the app root.
4. Validates the manifest, schemas, entrypoint references, and lockfile.
5. Materializes an immutable source snapshot in the Source Store under the
   workspace, repository source ID, and commit.
6. Saves an immutable release candidate and updates the source's last-synced
   marker.

Sync does not install dependencies, inject an SDK, compile source, create an
execution bundle, or change the active release. It is safe for source
validation to succeed even when runtime preparation later fails.

Every sync resolves the current branch head. Release candidates are stored by
exact commit, while the current Publish Release operation selects the latest
synchronized candidate for that repository source.

## 3. Publish a release

Publish Release uses the latest synchronized candidate. It does not read Git.
While holding the same source operation lease, it:

1. Loads the exact source snapshot from the Source Store.
2. Copies it into a preparation cache.
3. Installs declared Python or Bun dependencies, or builds the Go app.
4. Injects the matching Windforce SDK and prepares the app entrypoint.
5. Writes the preparation fingerprint to `.ready`.
6. Validates the prepared entrypoint.
7. Hashes the complete prepared tree and publishes it to the Execution Artifact
   Store under its SHA-256 digest.
8. Verifies that the published artifact matches the digest.
9. Atomically records the release and selects it as the active release.

For the PostgreSQL backend, publication updates the active release, immutable
release history, source release marker, audit record, Control Plane event, and
matching Webhook delivery records in one transaction. Webhook HTTP requests run
outside that transaction.

If preparation, artifact publication, verification, or the state transaction
fails, the previous active release remains selected. A successful Sync remains
available so the dependency or toolchain problem can be corrected and Publish
Release can be attempted again.

## 4. Admit a Run

Protocol adapters submit `workspace`, `app`, `action`, and input to the
Execution API. The Execution API owns admission; an adapter does not read the
release catalog or write queue tables.

Admission performs one atomic decision:

1. Resolve the app's active release.
2. Validate that the action exists and can be routed to a worker.
3. Validate and materialize the action input and output schemas.
4. Pin the deployment, bundle digest, entrypoint, schemas, route, timeout, and
   execution settings.
5. Create the caller-visible Run and its first Job.

Once admitted, the Job is independent of later active-release changes.

## 5. Execute the pinned Job

A worker claims a Job from the PostgreSQL queue and reads the deployment
snapshot embedded in it. The worker then:

1. Locates its cache by the pinned execution bundle digest.
2. Reuses the cache only when `.windforce-execution-ready` contains that digest
   and the bundle preparation fingerprint matches the worker.
3. Otherwise fetches the digest from the Execution Artifact Store into a clean
   local directory.
4. Validates the fetched bundle and writes the worker-local ready marker.
5. Executes the pinned app entrypoint and action.
6. Stores logs and the action result through the Execution Plane.

Job processing does not clone a repository, resolve repository credentials,
install dependencies, inject an SDK, or compile application source.

## State changes at a glance

| Operation | Reads Git | Writes Source Store | Writes Artifact Store | Changes active release | Creates a Job |
| --- | --- | --- | --- | --- | --- |
| Register | Access probe only | No | No | No | No |
| Sync | Yes | Yes | No | No | No |
| Publish Release | No | Reads only | Yes | Yes, after validation | No |
| Rollback Release | No | No | Validates only | Yes, after validation | No |
| Run admission | No | No | No | No | Yes |
| Worker execution | No | No | Reads only | No | Claims the pinned Job |

## Roll back to a historical release

Rollback selects an immutable historical Release ID, validates that its stored execution bundle is still available, and atomically moves the Control Plane Active Release Pointer to that release. It does not synchronize Git, install dependencies, rebuild source, create another release-history record, or change the latest synchronized candidate.

Existing Runs and Jobs keep the release snapshot pinned at admission. New Runs pin the selected historical release. The rollback actor and reason are recorded in the app audit trail and emitted as a `windforce.release.rolled_back` control-plane event.

After rollback, a newer synchronized candidate remains available. Publish Release is enabled whenever that candidate commit differs from the active historical release, so operators can return to the newer release through the normal publish flow.

The Control Plane Active Release Pointer identifies which immutable release is used for new Runs. The worker-local `.windforce-execution-ready` file only proves that a specific execution bundle digest is prepared in that worker cache; it does not select the active release.
