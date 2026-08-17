# ADR 0045: Expose engine build and capability self-description

- Status: Accepted
- Date: 2026-08-17

## Context

Core instances can be upgraded independently and may remain on different
releases for operational reasons. Consumers that coordinate more than one
instance need to explain which build is running and whether a particular API
contract is available. Inferring support from an image tag or a semantic
version alone is unreliable when development builds, backports, and mixed
rollouts are possible.

The authenticated workspace-scoped system information endpoint already
describes the instance planes, backends, authentication configuration, and
runtime capabilities. It is the appropriate neutral contract for exposing
additional build observations and feature availability without making Core a
deployment controller.

## Decision

`GET /api/w/{workspace}/system/info` returns two optional build observations:

- `version`: the Core release or development version;
- `revision`: the source revision used to build the running process.

Release pipelines inject both values through the existing linker variables.
Development builds and older binaries may omit either field. These observations
are diagnostic metadata, not proof of provenance or a security attestation.

The endpoint's `capabilities` map is the authoritative compatibility signal.
A capability name identifies a neutral contract and its value identifies the
contract version. This decision adds the following v1 declarations alongside
the existing execution-limit declarations:

- `worker_management`: Worker management APIs required by an external
  operator;
- `capability_gateway_run_context`: run-context support for capability gateway
  execution.

Consumers must test for the required capability and supported capability
version before enabling a dependent feature. A missing capability means that
support is unknown or unavailable; consumers must not infer it from the build
version. A capability version changes only when its contract changes in a way
that requires consumers to select different behavior.

Desired release selection, rollout policy, lifecycle management, and
compatibility policy remain outside Core. The endpoint does not expose
credentials, infrastructure topology, or deployment ownership.

## Consequences

- Operators can report mixed Core builds while using explicit capabilities to
  decide which integrations are safe to enable.
- Backports can advertise a supported contract without pretending to be a
  different release.
- Older binaries remain compatible because the new build fields are optional
  and absent capability declarations degrade to an unknown state.
- Consumers need a capability catalog that maps product behavior to the Core
  contracts it requires.
- Build identity remains advisory; signed provenance requires a separate
  design.
