---
title: Workspaces
description: Managed state and authorization boundaries inside one Windforce Core instance.
---

A workspace groups apps, releases, clients, input settings, jobs, inbound triggers, outbound webhooks, variables, resources, and audit records inside one Windforce Core instance. Every workspace has an immutable ID, a display name, a lifecycle status, and zero or more named scoped credentials.

## Identity

Workspace IDs are lowercase slugs. They contain 2 to 48 lowercase letters, digits, or hyphens, start with a letter, and end with a letter or digit. The ID is immutable because it is part of API paths and stored resource keys. The display name can be changed.

`default` is always registered and cannot be archived or deleted. It is the initial workspace for local development and installations that use one workspace.

## Access

Windforce Core recognizes these API principals:

| Principal | Scope | Credential |
| --- | --- | --- |
| Instance administrator | Global workspace lifecycle and every workspace | Token configured by `--admin-token-env` |
| Workspace principal | Full operator access to one workspace only | Named `wfw_` token returned once when explicitly issued or rotated |
| Client principal | Invocation and client-specific input settings for one client in one workspace | `wfk_` token returned once at client creation or rotation |
| Service principal | Scoped system-to-system invocation in one workspace, optionally limited to app/action targets | `wfs_` token returned once at service-principal creation or rotation |
| Job principal | SDK callback endpoints for one job and workspace | Short-lived job token |

Workspace creation and credential issuance are separate operations. Workspace tokens are stored as SHA-256 hashes, have stable IDs and operator-supplied names, and expose the raw value only on issue or rotation. Rotation invalidates that named credential's previous secret immediately; revocation disables it without affecting other credentials. Workspace principals cannot list, create, archive, issue, rotate, or revoke workspaces and credentials, and cannot access another workspace path.

### Bearer prefix contract for fronting proxies

Every bearer credential owned by the engine carries a `wf`-family prefix: `wfjob_` for job tokens, `wfw_` for workspace tokens, `wfk_` for client tokens, `wfs_` for service-principal tokens, and `wfr_` for the dedicated remote-worker-plane credential configured on a Cell. These credentials can only be verified by the engine that owns them — the signing secret, token hashes, and configured worker-plane secret never leave the instance. A fronting platform or proxy that terminates its own authentication classifies an engine-owned bearer by this prefix and forwards it unswapped for the engine to enforce. Hosted browser sessions may be delegated by a trusted proxy as a verified principal, but a host control-plane API token is not a Core credential and must not be exchanged merely because it reaches a Cell hostname. New token kinds extend the same family; platform layers must not repurpose Cloud credentials into the `wf` namespace.

When no instance-admin token is configured, local development accepts requests without authentication. Configure an instance-admin token for any shared environment.

## Lifecycle

An active workspace accepts control-plane changes and new execution requests. Archiving a workspace preserves its state and audit records while blocking configuration changes, credential issuance or rotation, releases, trigger or webhook changes, and new Runs. Read operations, audit queries, provisioning export, and credential revocation remain available. Revocation stays available for workspace, client, and service-principal tokens so a compromised credential can always be disabled. Job-scoped SDK callbacks remain available so running jobs can settle.

Permanent deletion is available to an instance administrator at `DELETE /api/workspaces/{workspace}`. It removes the workspace registry record and every workspace-scoped run, job, app release, trigger, route binding, webhook, variable, resource, input configuration, credential, encryption key, and audit record in one storage transaction. It cannot be undone. The `default` workspace is protected from deletion. Reactivation of an archived workspace is not exposed.

## Operations

Use the sidebar workspace switcher to change the current workspace. Open **Manage workspaces** to create a workspace or switch the active one. The registry is intentionally limited to those two operations.

Settings applies to the active workspace:

- **Workspace** changes its display name and contains archive and permanent-delete lifecycle actions;
- **Access** issues, rotates, and revokes named workspace tokens;
- **Audit** includes identity, access, and lifecycle records under the `workspace` category.

Instance-admin authorization is still required for workspace lifecycle and credential operations. The Web UI requires the workspace display name to match exactly before permanent deletion is enabled, then switches the browser to `default` after deletion succeeds. Old `/workspaces/{id}` detail links first activate that workspace, then redirect to the corresponding Settings or Audit destination.

The global lifecycle API is rooted at `/api/workspaces`. Workspace resources remain rooted at `/api/w/{workspace}` and all operator, client, and service Run invocation is rooted at `/api/v1/workspaces/{workspace}`. Legacy public and execution admission paths remain only until the ADR 0013 v0.3 breaking removal.
