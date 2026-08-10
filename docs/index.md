---
title: Windforce Lite
description: Source synchronization, release publication, and execution for script applications.
---

Windforce Lite is a small control plane and execution runtime for applications
described by `windforce.json`. It synchronizes an exact Git revision, publishes
a prepared execution bundle, pins that release when a Run is admitted, and
executes the pinned bundle on a worker.

The product is organized around two independent paths:

- The **release path** turns Git source into an immutable, worker-ready
  execution bundle and selects an active release.
- The **execution path** accepts an app and action, pins the active release into
  a Job, and runs that exact bundle without contacting Git.

## Start here

1. [Core concepts](concepts/core-concepts.md) defines apps, synchronized
   revisions, releases, stores, caches, fingerprints, Runs, and Jobs.
2. [Container images](guides/container-images.md) explains the signed public images, supported platforms, immutable tags, and local source builds.
3. [Release and execution lifecycle](concepts/release-lifecycle.md) explains
   Register, Sync, Publish Release, and Run in order.
4. [Worker execution lifecycle](concepts/worker-execution.md) defines the canonical pinned-bundle fetch, launcher, and completion sequence.
5. [Execution observability and debugging](concepts/execution-observability.md) separates Job logs, results, service logs, artifacts, and interactive debuggers.
6. [App runtime interface and SDK boundaries](concepts/app-runtime-interface.md) separates the Core host context, Invocation SDK, Core Author SDK, and Domain Authoring SDK responsibilities.
7. [Run admission architecture](concepts/run-admission.md) distinguishes SDKs, the Invocation API, AdmissionService, Triggers, Gateways, Runs, and Jobs.
8. [Runtime configuration and secrets](concepts/runtime-configuration.md) explains Variable, Resource, InputConfig, SecretBackend, and Job-scoped resolution.
9. [Architecture](architecture.md) defines the Control, Trigger, Invocation, Execution, and Worker Plane boundaries.
10. The separately released [Imprun CLI](https://github.com/imprun/cli) consumes Core's HTTP contracts without becoming part of the runtime. `tools/windforce_control.py` is only a repository-local development and operator helper; Core does not ship a separate end-user CLI.

## Documentation hosting

The `docs` directory is the documentation root for both supported hosting
options. The Markdown content is shared; only the site configuration differs.

- **Mintlify:** connect the repository and set the documentation directory to
  `docs`. Mintlify reads `docs/docs.json`.
- **GitHub Pages:** configure the publishing source as the `main` branch and
  `/docs` directory. GitHub Pages reads `docs/index.md` and
  `docs/_config.yml`.

The product documentation describes the current implementation. Design history
and decision rationale remain in the `docs/adr` records and are not presented as
current operator instructions.
