---
title: Core concepts
description: The source, release, artifact, cache, and execution terms used by Windforce Lite.
---

Windforce Lite keeps source management separate from job execution. A Git
revision becomes a release through the Control Plane. The Execution Plane then
runs only the immutable bundle pinned into each Job.

## Domain model

| Term | Meaning | Source of truth |
| --- | --- | --- |
| Repository source | A repository URL, branch, optional subpath, and credential reference registered with the Control Plane. | Control Plane state |
| App | The stable executable identity declared by `app` in `windforce.json`. A repository source supplies an app, but the repository and app are not the same object. | Released manifest |
| Action | A named operation exposed by an app, including its schemas and execution settings. All actions enter through the app-level entrypoint. | Released manifest |
| Synchronized revision | An exact commit whose app root, manifest, schemas, and lockfile have passed source validation. It is also called a release candidate. | Release candidate catalog and Source Store |
| Deployment | The complete executable snapshot assembled from a synchronized revision: app, commit, entrypoint, actions, schemas, execution settings, and execution bundle coordinates. | Release record or pinned Job |
| Release | An immutable publication record for a validated deployment. | Release history |
| Active release | The release selected for new Runs of an app in a workspace. | Active release catalog |
| Run | A caller-visible invocation of one app action. | Execution state backend |
| Job | An internal execution attempt. It contains the deployment and action snapshot selected when the Run was admitted. | Execution queue |

The repository source may be renamed or replaced without changing the app key.
Conversely, removing a repository source does not erase an already published
release. The app remains executable while its active release and execution
bundle remain available.

## Shared stores and local caches

Windforce Lite uses two shared stores and one disposable worker cache. They have
different identities and must not be treated as one directory.

| Storage | Identity | Contents | Used by |
| --- | --- | --- | --- |
| Source Store | Workspace, repository source ID, exact Git commit | Validated source files before dependency installation or compilation | Sync and release publication |
| Execution Artifact Store | SHA-256 tree digest | Prepared source, installed dependencies, injected Windforce SDK, compiled output, and preparation fingerprint | Release publication and workers |
| Worker-local bundle cache | Execution bundle digest | A fetched copy of an execution bundle validated for a worker environment | Runtime worker |

The **Source Store** is an object cache of immutable Git snapshots. Git remains
the reconstruction source for this store, but only the Control Plane source
synchronizer reads Git credentials or runs Git operations.

The **Execution Artifact Store** is the distribution boundary between release
publication and workers. Its content-addressed digest identifies the complete
prepared tree, not only the application source. An active release and every
queued Job refer to this digest.

The **worker-local cache** is filesystem state available to a worker execution
environment. "Local" describes its contract, not a required volume topology:
the cache may use ephemeral disk or an attached cache volume, but it is not
catalog state and is not a source of truth. A container replacement or pod
eviction may remove it; the worker reconstructs it by fetching the pinned digest
from the Execution Artifact Store. A worker never reconstructs this cache from
Git.

The current binary provides filesystem-backed stores. With `--store <store>`
and `--cache <cache>`, their relevant layout is:

```text
<store>/gitrepos/<workspace>/<source-id>/<commit>/
<store>/artifacts/execution-bundles/sha256/<digest>/
<cache>/src/<workspace>/<source-id>/<commit>/
<cache>/execution-bundles/<digest>/
```

`<cache>/src` is the release builder's preparation cache. It is not a source
directory used by workers during Job execution. When the Control Plane and
workers run as separate services, they must be configured to see the same
persistent execution artifact root. The current implementation does not fetch
execution bundles from S3 or another remote object service.

```text
Git repository
    |
    | Sync (Control Plane, Git credential allowed)
    v
Source Store: workspace / source ID / commit
    |
    | Publish Release (prepare and validate)
    v
Execution Artifact Store: sha256:<prepared-tree-digest>
    |
    | Fetch pinned digest (no Git credential)
    v
Worker-local bundle cache
```

## Digests, fingerprints, and ready markers

These values answer different questions:

- The **execution bundle digest** answers: "Are these prepared files exactly
  the same?" It is a SHA-256 identity for the bundle tree.
- The **preparation fingerprint** answers: "Can this worker execute a bundle
  prepared by this toolchain?" Current runtime errors call this the runtime
  fingerprint.
- A **ready marker** answers: "Did this cache step finish and validate?" A
  marker contains an identity value; the marker itself is not that identity.

The preparation fingerprint is a JSON value containing:

- preparation contract version
- normalized script language
- language runtime identity
- operating system and architecture identity
- digest of the injected Windforce SDK

Windforce Lite currently writes two ready markers:

| File | Location | Value | Meaning |
| --- | --- | --- | --- |
| `.ready` | Prepared source and published execution bundle | Preparation fingerprint JSON | Dependency installation, SDK injection, compilation, and entrypoint preparation completed for this environment contract. |
| `.windforce-execution-ready` | Worker-local bundle cache only | Execution bundle digest | The worker fetched this digest and accepted the bundle's preparation fingerprint. |

On a cache hit, the worker checks the local bundle marker and validates the
preparation fingerprint again. If the fingerprint does not match its runtime,
the worker rejects the bundle instead of installing dependencies or compiling
source during the Job.

The marker filenames are runtime implementation details. Integrations should
use release and execution APIs rather than reading these files.

The stores also maintain two completion metadata files that are not runtime
compatibility markers:

- `.windforce_clone_complete` records that a Source Store snapshot finished
  materializing for its workspace, source ID, and commit.
- `.windforce-execution-bundle.json` is the Artifact Store descriptor containing
  the bundle digest, URI, creation time, file count, and byte size. The
  descriptor itself is excluded from the prepared-tree digest.

## Pinning rules

Run admission resolves the active release once and copies its executable
coordinates into the Run and Job. This gives the queue stable behavior:

- Publishing a release affects only Runs admitted after publication.
- A queued Job stays pinned to the release selected when it was created.
- A worker restart does not move a Job to a newer release.
- The worker does not query the active release before executing a pinned Job.

Therefore the active release is a pointer for **new work**, while a Job is an
immutable execution decision for **already admitted work**.

## Credential boundary

Repository credentials belong to source synchronization in the Control Plane.
They are not part of a release, execution bundle, Run, Job, or worker
configuration. A worker that cannot fetch a pinned execution bundle must fail
that execution path; falling back to a Git clone would violate the architecture.
