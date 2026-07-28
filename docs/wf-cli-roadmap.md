---
title: wf CLI roadmap
description: Product contract and delivery gates for the thin Windforce workspace client.
---

This roadmap records the 2026-07-29 delivery target for `wf`. The quality benchmark is GitHub CLI: predictable command discovery, secure browser authentication, useful terminal output, stable automation output, cross-platform installation, actionable failures, and complete help. It does not mean copying GitHub-specific commands or shipping a Windforce runtime inside the client.

## Product boundary

`wf` is a local client for an existing Cell. Infrastructure owners create and upgrade Cells. A user selects a context and then manages apps, releases, and Runs inside the selected workspace.

```text
operator workstation
  wf
   |
   +-- direct Cell -------- named workspace credential
   |
   `-- hosted product ---- human login + product authorization
                              |
                              `-- delegated Cell request
```

The runtime remains `windforce-core server|worker|standalone`. No `wf cell deploy` command, background `wf` service, Kubernetes workload, or bundled worker is introduced.

## Quality contract

- `wf help` and `wf <group> <command> --help` are complete enough to discover normal workflows without opening the source repository.
- Interactive commands use readable tables, labels, progress, confirmations, and suggestions. Piped or explicitly selected machine output is deterministic.
- `--json <fields>`, `--jq`, and `--template` form the long-term structured-output contract. Commands do not require scraping decorative terminal output.
- Standard output is reserved for results. Progress, warnings, and diagnostics use standard error. `NO_COLOR` and non-terminal output disable decoration.
- Exit codes distinguish usage, local configuration, authentication, authorization, transport, API client failure, and API server failure.
- Destructive operations explain their target and require confirmation unless `--yes` is supplied.
- `WF_API_URL`, `WF_CONTEXT`, `WF_WORKSPACE`, and `WF_TOKEN` provide explicit non-interactive overrides. Environment credentials are never copied into configuration.
- Secrets use the operating-system credential store. Failure to access secure storage is visible and fails closed without a plaintext fallback.
- Diagnostic output redacts authorization headers, cookies, OAuth codes, PKCE verifiers, access tokens, refresh tokens, and engine credentials.

## Authentication profiles

### Direct Cell

`wf auth login --with-token` reads an engine-owned credential from standard input and associates it with the selected host. A named workspace token is the normal operator credential. A token supplied through `WF_TOKEN` takes precedence for one process and is not stored.

### Hosted product

The target advertises its supported authorization method. The preferred interactive flow is OAuth 2.0 Device Authorization Grant when the provider supports it; Authorization Code with PKCE and a loopback redirect is the fallback for a native public client. The external system browser performs human authentication.

The CLI stores only the refreshable hosted credential in the operating-system credential store and refreshes short-lived access credentials as needed. The hosting product validates the token, resolves its own membership and role, derives an audit actor, and delegates the request. Hosted management API tokens and Cell credentials remain separate audiences and are never silently exchanged.

## Context model

A context is non-secret metadata:

```yaml
name: hosted-cell
api_url: https://cell-east-1.platform.example.test
workspace: default
actor: developer@example.test
account: account-label
auth_type: oauth2-device
```

The account selects a host credential. The context selects a routing target and workspace. Changing a workspace does not create another login, and changing an account does not mutate a Cell. Logout clears every context sharing the same host credential, while context deletion requires prior logout and explicit confirmation.

## App publish workflow

`wf app publish .` is the primary developer workflow:

1. Find `windforce.json` and the containing Git worktree.
2. Reject an uncommitted tree unless the user explicitly selects a supported override.
3. Resolve the Git remote, repository subpath, branch, and exact local commit.
4. Find or register the matching source.
5. synchronize with an expected commit so a branch race cannot publish an unintended revision.
6. Publish the prepared immutable release.
7. Print the app, exact commit, release ID, bundle digest, active state, and target context.

The server remains authoritative for source access, validation, preparation, artifact publication, and active-release mutation. The CLI does not build an execution bundle or send Git credentials to workers.

## Delivery milestones

### M1 — Product shell

- Release an independent `wf` executable while retaining the legacy `windforce` binary.
- Add context aliases over the existing profile contract and migrate non-secret configuration safely.
- Lead help and installation documentation with `wf`.
- Prove that the `wf` dependency graph excludes server, worker, database, queue, and runtime packages.

### M2 — Terminal and automation contract

- Add terminal-aware human output and explicit structured fields.
- Add `--json`, filtering, templates, `NO_COLOR`, paging policy, prompts, and shell completion.
- Stabilize exit codes and safe diagnostic categories.

Terminal-aware labels and tables, deterministic redirected JSON, `--json <fields>`, `--jq`, `--template`, authentication and authorization exit codes, offline command help, and Bash/Zsh/Fish/PowerShell completion are now implemented. Paging is intentionally disabled until a bounded large-result workflow needs it. The client emits no color today, so `NO_COLOR` and non-terminal output are decoration-free.

### M3 — Authentication

- Implement secure credential-store abstraction and explicit environment-token precedence.
- Add direct `--with-token` login, status, switch, and logout.
- Add provider discovery and hosted browser or device authorization without product-specific code in Core.
- Verify token refresh, revocation, multiple accounts, headless automation, and redaction.

Direct token login, status, logout, credential-store isolation, account switching, and `WF_TOKEN` precedence are implemented. Hosted targets can now advertise a secretless Device Authorization client through `/.well-known/wf-cli.json`; `wf auth login` discovers the issuer, opens the external browser or prints the verification URL, stores the refreshable credential securely, validates workspace access, and refreshes the access token before expiry. Hosted logout performs provider token revocation with a fail-closed local fallback, while central browser-session logout remains a separate Identity concern. Tests cover the browser-opening and headless paths, refresh rotation, multiple local accounts, environment-token precedence, remote revocation, revocation failure, older stored credentials, and revocation of a newly issued credential when workspace verification cannot complete. Full live hosted verification remains open.

### M4 — Workspace and app workflows

- Add workspace discovery and switching.
- Add `wf app publish .` with exact-commit protection.
- Add release history, activation, rollback, and canonical Run commands.
- Keep low-level source and Job commands available for advanced operations.

Exact-commit `wf app publish`, safe context deletion, workspace list/view/use, release list/view/activate/rollback, and Run watch/result are implemented. Workspace switching verifies access before mutating the context. The server rejects a Sync or Publish race before candidate mutation or release activation, and a successful publication returns the immutable release ID. A direct-Cell integration test now exercises a real Git remote through publish, release inspection, worker claim/completion, Run watch, and result retrieval in one workflow. Hosted discovery across multiple Cells remains a platform concern and is still open.

### M5 — Distribution and live verification

- Publish signed checksums and host-specific archives for Windows, macOS, and Linux.
- Add installer and package-manager channels only after archive verification is stable.
- Test upgrades without losing contexts or credentials.
- Exercise direct standalone and hosted Cell paths end to end.

Cross-platform archives, checksums, exact archive-content assertions, and Windows, macOS, and Linux CLI smoke commands are configured. Tagged releases additionally create and verify a keyless Sigstore bundle for the checksum file. Release verification runs each published amd64 binary on its native Windows, macOS, or Linux host and verifies that executable replacement preserves an external context file. Pull-request CI also runs the CLI package under Go's race detector. A hosting-product E2E builds the real `wf` and Cell binaries and covers exact-commit publication through Run result retrieval with non-interactive product authorization. The workflows must still pass remotely, and a new tag plus live hosted Cell verification are required before this milestone is complete.

## Completion gates

The goal is complete only when:

1. `wf` is independently installable and contains no runtime role.
2. Browser login, direct token login, logout, refresh, multiple accounts, and environment-token automation are covered by tests.
3. A user can select a hosted Cell and workspace, publish the exact local Git commit, inspect the resulting release, create a Run, watch it, and retrieve its result.
4. The same app and Run workflow works against a direct self-hosted Cell.
5. Human output is usable on a terminal and every supported automation command has stable structured output.
6. Secrets are absent from configuration, logs, test snapshots, release artifacts, and error messages.
7. Help, completion, installation, upgrade, authentication, context, publish, release, Run, and troubleshooting documentation is current.
8. CI verifies tests, binary dependency boundaries, archives, checksums, and smoke commands on the supported host matrix.
9. The released binary and hosted integration are validated against a real Cell; a source push alone is not completion.
