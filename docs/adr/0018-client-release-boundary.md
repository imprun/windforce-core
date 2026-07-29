# ADR 0018: Keep installed product clients outside the Core runtime repository

## Status

Accepted (2026-07-29). Supersedes [ADR 0015: Ship `wf` as a thin workspace client](0015-thin-wf-client.md).

## Context

The initial thin-client implementation proved that the Control Plane and canonical Invocation APIs can support app publication, release inspection, Runs, workspace selection, stable automation output, and hosted Device Authorization without linking server or worker packages into the client.

The implementation also established a release and ownership mismatch. An installed client with product-branded Identity login, operating-system credential storage, account contexts, upgrade packaging, and hosted tenant delegation evolves on a product lifecycle. Windforce Core must remain a provider-neutral self-hosted execution engine, and its release archives should not contain product clients or deprecated aliases.

## Decision

1. This repository releases only the `windforce-core` runtime binary and container source. It does not release `wf`, `windforce`, or another installed product client.
2. The public `imprun/cli` repository owns the separately installed `imprun` command, user configuration, secure credential-store integration, shell completion, release signing, and operating-system packages.
3. The client consumes Core's versioned HTTP APIs. Core does not import the client and does not receive Imprun Identity issuer, OAuth client, tenant membership, role, or commercial product policy.
4. Direct self-hosted credentials remain Core contracts. A downstream hosting product may publish authentication discovery and delegate an authorized request, but Core verifies only its own machine and workspace credentials.
5. Existing `wf` and `windforce` release assets remain immutable historical artifacts. New Core releases do not contain those binaries or aliases.
6. CLI-oriented helper packages that have no runtime consumer leave this module. Reusable engine protocol must be specified by HTTP/OpenAPI and language SDK contracts instead of keeping an otherwise unused Go client solely for the former command.

## Consequences

- A Core installation has one unambiguous executable, `windforce-core`, with `server`, `worker`, and `standalone` roles.
- Product client releases can move independently without making Core releases carry hosted authentication or packaging concerns.
- Self-hosters are not required to use Imprun services; they can call the documented HTTP APIs directly or use any compatible client.
- Cross-repository acceptance tests must prove that the separately released client still controls a real Core Cell through public API boundaries.
