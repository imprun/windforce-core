---
title: wf command-line client
description: Install and use the thin client for an existing Windforce Cell.
---

`wf` is the supported command-line client for an existing Windforce Cell. Installing it does not install or start a server, worker, database, or queue. The legacy `windforce` command remains in release archives during the migration period.

The current client exposes the complete low-level Control Plane API and emits compact JSON by default. The higher-level authentication, human output, and `wf app publish .` milestones are tracked in the [wf CLI roadmap](wf-cli-roadmap.md).

## Install

Tagged releases contain `wf` archives for Windows, macOS, and Linux on amd64 and arm64. These are operator-host binaries and are unrelated to the architecture of a Kubernetes Cell image.

On Windows, extract the matching ZIP and place `wf.exe` on `PATH`:

```powershell
Expand-Archive .\wf_<VERSION>_windows_amd64.zip -DestinationPath .\wf
wf\wf.exe version
```

On macOS or Linux, extract the matching archive and install the executable in a directory on `PATH`:

```shell
tar -xzf wf_<VERSION>_<OS>_<ARCH>.tar.gz
install -m 0755 wf "$HOME/.local/bin/wf"
wf version
```

The repository build produces `wf` and the legacy `windforce` alias:

```shell
make cli-build
```

On Windows the outputs are `.tmp/bin/wf.exe` and `.tmp/bin/windforce.exe`.

## Configure a context

A context contains non-secret connection metadata: API URL, workspace, optional audit actor, and an optional environment-variable name. `WF_TOKEN` supplies a bearer token to one process and takes precedence without being written to the configuration file.

```powershell
wf context set local `
  --api-url http://127.0.0.1:18091 `
  --workspace default `
  --actor developer@example.test `
  --use

$env:WF_TOKEN = "<WORKSPACE_TOKEN>"
wf app list --summary
```

Use `wf context list`, `wf context show`, and `wf context use <name>` to inspect or select contexts. `WF_CONFIG` selects an explicit configuration file. When the new default configuration does not exist, `wf` can read the legacy `windforce` profile file; the next context change writes the new `wf` configuration.

For existing automation that names a token environment variable:

```powershell
$env:WORKSPACE_TOKEN = "<WORKSPACE_TOKEN>"
wf context set hosted `
  --api-url https://cell.example.test `
  --workspace team `
  --token-env WORKSPACE_TOKEN `
  --use
```

The configuration stores only `WORKSPACE_TOKEN`, never its value. Secure interactive credential storage will replace this compatibility workflow when the authentication milestone lands.

Global flags override the selected context:

```shell
wf --context staging --pretty app list --summary
wf --api-url https://cell.example.test --workspace team app list
```

The primary process overrides are:

| Variable | Meaning |
| --- | --- |
| `WF_CONFIG` | Explicit non-secret configuration path |
| `WF_CONTEXT` | Selected context |
| `WF_API_URL` | Control Plane API base URL |
| `WF_WORKSPACE` | Selected workspace |
| `WF_ACTOR` | Direct-connection audit actor |
| `WF_TOKEN` | One-process bearer credential; never persisted |

## Publish a release

The current release workflow keeps Register, Sync, and Publish explicit:

```powershell
$env:GIT_ACCESS_TOKEN = "<TOKEN>"
wf source probe `
  --repo-url https://git.example.test/team/app.git `
  --branch main `
  --auth-method pat `
  --access-token-env GIT_ACCESS_TOKEN

wf source register `
  --name example `
  --repo-url https://git.example.test/team/app.git `
  --branch main `
  --subpath apps/example `
  --auth-method pat `
  --access-token-env GIT_ACCESS_TOKEN

wf source list
wf source sync 12
wf source publish 12 --message "Publish validated revision"
```

`publish` calls the existing release-publication endpoint. The legacy spelling `source deploy` remains accepted during migration. Workers never receive repository credentials and never contact Git.

## Inspect apps and schemas

```shell
wf app list --summary
wf app show example
wf app history example
wf action show example health
wf action schema example health
wf app openapi example
wf openapi
```

Commands emit compact JSON by default. Add the global `--pretty` flag before the command for indented JSON.

## Run and inspect work

```shell
wf run create example health --input '{"ping":true}'
wf run wait example parse --input-file input.json --timeout 30s
wf run show <RUN_ID>
wf run result <RUN_ID>
wf run cancel <RUN_ID> --reason "operator request"

wf job list --app example --status running
wf job show <JOB_ID>
wf job logs <JOB_ID> --tail-bytes 65536
```

`--input-file -` reads JSON from standard input. Job logs are written as the raw response so they can be piped.

## Provisioning

```shell
wf provisioning export --format yaml --output windforce.yaml
wf provisioning apply --file windforce.yaml --dry-run
wf provisioning apply --file windforce.yaml
```

Exported secret values remain redacted. Environment-specific secret resources must use the provisioning `valueFrom` contract.

## Exit codes

| Code | Meaning |
| ---: | --- |
| `0` | Command completed successfully |
| `2` | Invalid command or arguments |
| `3` | Invalid local context or configuration |
| `10` | Local I/O or HTTP transport failure |
| `20` | Control Plane returned a 4xx response |
| `21` | Control Plane returned a 5xx response |

JSON API errors are written to standard error. This preserves the existing automation contract while terminal-oriented error categories are added.

## Repository helper

`tools/windforce_control.py` is a repository-local API helper. Released `wf` archives are the installed client contract. Repository maintenance can continue using the helper without making Python a dependency of product repositories.
