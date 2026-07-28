# ADR 0015: Ship `wf` as a thin workspace client

## Status

Accepted (2026-07-28).

## Context

Windforce Core ships runtime roles and a low-level `windforce` Control Plane client from the same repository. The Go linker already excludes unreferenced server and worker packages from the client executable, but the installed product still presents itself as `windforce`, exposes environment-variable-oriented profiles, emits raw JSON by default, and uses `deploy` for an operation that publishes an app release into an existing Cell.

An operator should not need to install or start a Windforce server to manage an app. A hosted product also must not add its account system, tenant authorization, or identity-provider configuration to the self-hostable engine.

## Decision

1. The installed user command is `wf`. It is a separate executable built from a dedicated entrypoint and contains only command parsing, local configuration, credential-provider interfaces, formatting, and the public Control Plane client.
2. `wf` never provisions, upgrades, or deploys a Cell and never embeds a server or worker role. Infrastructure owners provision Cells; `wf` manages resources inside an already reachable Cell.
3. A context selects a host, Cell or API endpoint, and workspace. Authentication is host-scoped and kept separate from context selection so one authenticated account can use multiple Cells and workspaces.
4. Direct self-hosted connections use engine-owned credentials such as a named workspace token. Hosted authentication is discovered from the target and implemented by the hosting product. The engine does not contain a hosted product issuer, client ID, tenant membership, or role.
5. Interactive credentials go to the operating-system credential store. Configuration files contain only non-secret connection and display metadata. Environment credentials take precedence for automation and are never persisted implicitly. Plaintext credential storage requires an explicit opt-in.
6. Human output is concise and terminal-oriented. Automation can request stable JSON fields and filtering explicitly. Standard output contains command results; diagnostics and progress use standard error; stable exit codes remain part of the contract.
7. User-facing release vocabulary is `publish`. `wf app publish` may compose source discovery, registration, synchronization, and release publication, but it must preserve the engine lifecycle and expose the exact commit and resulting release. `source deploy` remains only as a deprecated compatibility spelling.
8. Run is the user-visible execution resource. Job commands remain an advanced operational surface and must not replace the canonical Run lifecycle.
9. The existing `windforce` executable remains available during a deprecation window and uses the same API client. New documentation and release artifacts lead with `wf`.
10. The first implementation remains in this repository so the CLI and engine API contract change together. Repository extraction is a future release-management decision, not a runtime architecture requirement.

## Command model

```text
wf auth login|status|switch|logout
wf context list|view|use|add|remove
wf workspace list|view|use
wf app list|view|publish
wf release list|view|activate|rollback
wf run create|list|view|watch|logs|cancel
wf api
wf config get|set|list
wf completion
wf version
```

The initial compatibility layer may expose existing lower-level source, action, job, OpenAPI, and provisioning commands while the higher-level command groups are implemented.

## Hosted authentication boundary

```text
wf
  -> target authentication discovery
  -> external browser or device authorization
  -> hosting product API
  -> product membership and role authorization
  -> hosted Cell gateway
  -> existing Cell Control Plane API
```

The hosting product validates the human credential, resolves product-owned membership and role, derives the audit actor, and delegates to the Cell without exposing the Cell administrator credential. An API token intended for the hosting product's management plane is not silently converted into Cell authority.

## Consequences

- Installing `wf` does not install or run Windforce services.
- A self-hoster can use `wf` without any Imprun service.
- Hosted products can offer central sign-in without coupling Core to an identity provider.
- The CLI release contains host binaries for supported operator platforms; this is unrelated to the architecture of a Kubernetes Cell image.
- Browser authentication, secure credential storage, high-level publish orchestration, human formatting, shell completion, and update packaging require incremental implementation behind this fixed boundary.
