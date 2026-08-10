# ADR 0032: Observe Worker build identity without owning desired state

- Status: Accepted
- Date: 2026-08-11

## Context

Core Workers may run in-process, as independent processes, or behind the remote
Worker Plane. The live registry already reports identity, capability, slots,
status, and heartbeat state, but it does not say which Core build a Worker is
actually running. An operator therefore cannot distinguish a placement or
capacity problem from a partially rolled out or stale Worker build.

Hosted and self-hosted control planes may already own a desired WorkerPool
image, rollout strategy, and drain sequence. Moving those deployment concerns
into Core would duplicate the control plane and make the execution engine
authority over infrastructure it does not manage.

## Decision

Each Worker registration may report two optional build observations:

- `engine_version`: the Core release or development version;
- `build_revision`: the source revision used for the binary.

The Core process supplies these values to both local and remote Processors.
Release builds inject the version and revision through linker variables, while
development builds remain explicitly identifiable as `dev` and `unknown`.
Docker image builds receive the same values as build arguments.

The Local and PostgreSQL stores persist the observations, the Worker Plane
carries them during registration, and the canonical Worker registry API
returns them. Values are trimmed, bounded to 128 UTF-8 bytes, and reject
control characters.

These values are self-reported observations. They do not grant authority,
change credential scope, select Jobs, or prove artifact provenance. Existing
Workers that do not send them remain compatible and expose omitted fields.

Ownership remains split:

- Core owns the observed build identity and its neutral registry API.
- A hosted portal or self-hosted deployment controller owns the desired image
  or version, compares desired and observed state, and performs rollout,
  replacement, drain, and alerting.
- Build and release pipelines own truthful injection of release version and
  source revision.

## Consequences

- Operators can diagnose mixed or stale Worker fleets without making Core a
  deployment controller.
- Imprun Cloud can compare WorkerPool desired state with Core observations
  without a Cloud-specific field or policy in the public engine.
- Headless installations receive the same observation contract and may build
  their own reconciliation around it.
- The fields are useful for diagnostics but must not be treated as a security
  attestation. Strong provenance would require a separately designed signed
  identity contract.
