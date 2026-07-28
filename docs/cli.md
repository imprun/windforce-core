---
title: wf command-line client
description: Install and use the thin client for an existing Windforce Cell.
---

`wf` is the supported command-line client for an existing Windforce Cell. Installing it does not install or start a server, worker, database, or queue. The legacy `windforce` command remains in release archives during the migration period.

The client exposes high-level app, release, and Run workflows together with the complete low-level Control Plane API. Interactive terminals receive readable labels and tables. Redirected output remains compact JSON unless `--pretty`, `--json`, `--jq`, or `--template` selects another stable automation format. Hosted Device Authorization and direct Cell credentials are supported.

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

Each release includes `checksums.txt` and a keyless Sigstore bundle named
`checksums.txt.sigstore.json`. Verify the signer and checksum before installing:

```shell
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity "https://github.com/imprun/windforce-core/.github/workflows/release.yml@refs/tags/<VERSION>" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  checksums.txt
sha256sum --check --ignore-missing checksums.txt
```

Upgrade by verifying and replacing the executable with the archive for the new
version. Context files and credentials are stored outside the executable and
remain in place across upgrades.

The repository build produces `wf` and the legacy `windforce` alias:

```shell
make cli-build
```

On Windows the outputs are `.tmp/bin/wf.exe` and `.tmp/bin/windforce.exe`.

## Configure a context

A context contains non-secret connection metadata: API URL, workspace, optional audit actor, account label, and authentication type. Interactive credentials are stored in Windows Credential Manager, macOS Keychain, or the Linux Secret Service. `WF_TOKEN` supplies a bearer token to one process and takes precedence without being written to the configuration file or credential store.

For a hosted target, login discovers a secretless OAuth 2.0 Device Authorization client from the selected target, opens the system browser, and validates the resulting access against the selected workspace:

```powershell
wf context set hosted `
  --api-url https://cell.example.test `
  --workspace team `
  --use

wf auth login
wf auth status
wf app list --summary
```

Use `wf auth login --no-browser` on a remote shell to print the verification URL instead of trying to open it. `--web` explicitly selects the same hosted flow. `--account <label>` keeps credentials for another hosted account under a distinct local label.

The target publishes only non-secret discovery metadata at `/.well-known/wf-cli.json`:

```json
{
  "schema_version": 1,
  "authentication": {
    "type": "oauth2-device",
    "issuer": "https://identity.example.test",
    "client_id": "wf-cli",
    "audience": "windforce-api",
    "scopes": ["openid", "profile", "email", "offline_access"]
  }
}
```

The issuer's OpenID configuration supplies the Device Authorization and token endpoints. The public client has no client secret. `wf` stores the short-lived access token and refresh token only in the operating-system credential store and refreshes before expiry. Discovery and OAuth endpoints require HTTPS except for loopback development; cross-origin discovery redirects and OAuth POST redirects are rejected.

For a direct self-hosted Cell, read a named workspace credential from standard input:

```powershell
wf context set local `
  --api-url http://127.0.0.1:18091 `
  --workspace default `
  --actor developer@example.test `
  --use

Get-Content .\workspace-token.txt | wf auth login --with-token --account operator
wf auth status
wf app list --summary
```

Use `wf context list`, `wf context show`, and `wf context use <name>` to inspect or select contexts. `wf auth switch <account>` selects another credential already stored for the same host and verifies it before changing the context. `WF_CONFIG` selects an explicit configuration file. When the new default configuration does not exist, `wf` can read the legacy `windforce` profile file; the next context change writes the new `wf` configuration.

Both login modes validate the credential against the selected workspace before storing it. If the system credential store is unavailable, login fails without writing a plaintext fallback. `wf auth logout` removes only the local credential reference and secret. It does not revoke an Identity session, hosted refresh token, or direct workspace credential remotely.

Inspect or change the workspace without creating another login:

```shell
wf workspace show
wf workspace list
wf workspace view team
wf workspace use team
```

`workspace use` probes the target with the current credential before updating
the context. A failed authorization therefore leaves the previous workspace
unchanged. The global list and view endpoints require an instance-admin
credential on a direct Cell or equivalent authorization from a hosted product.

For automation, prefer the one-process environment override:

```powershell
$env:WF_TOKEN = "<WORKSPACE_TOKEN>"
wf app list --summary
Remove-Item Env:WF_TOKEN
```

Existing automation can still name a token environment variable:

```powershell
$env:WORKSPACE_TOKEN = "<WORKSPACE_TOKEN>"
wf context set hosted `
  --api-url https://cell.example.test `
  --workspace team `
  --token-env WORKSPACE_TOKEN `
  --use
```

The configuration stores only `WORKSPACE_TOKEN`, never its value. This is a compatibility workflow; new interactive use should use the system credential store.

Global flags override the selected context:

```shell
wf --context staging app list --summary
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

Run the primary workflow from the app directory or any child directory:

```shell
wf app publish .
wf app publish . --message "Ship invoice validation"
```

`wf app publish` finds the nearest `windforce.json` and Git worktree, resolves
the remote, branch, repository subpath, and full `HEAD` commit, finds or
registers a matching source, synchronizes with an exact-commit precondition,
and publishes the immutable bundle. If the remote branch does not resolve to
that commit, publication fails before activation.

The worktree must be clean by default. `--allow-dirty` explicitly publishes
only committed `HEAD`; uncommitted files, including a changed manifest, are
ignored. The result identifies the app, exact commit, source, release, bundle
digest, workspace, and context. Private repositories use a server-side
credential reference:

```shell
wf app publish . --creds-ref github-app-installation
wf app publish . --source-id 12
```

The low-level Register, Sync, and Publish sequence remains available for
advanced operations:

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
wf source sync 12 --expected-commit <FULL_COMMIT>
wf source publish 12 --expected-commit <FULL_COMMIT> --message "Publish validated revision"
```

`publish` calls the existing release-publication endpoint. The legacy spelling `source deploy` remains accepted during migration. Workers never receive repository credentials and never contact Git.

## Inspect and activate releases

```shell
wf release list example
wf release view example <RELEASE_ID>
wf release activate example <RELEASE_ID> \
  --reason "Restore the last known good release" \
  --yes
```

`activate` and its explicit `rollback` alias validate the immutable bundle in
the Cell before changing the active release. A reason and `--yes` are required
so automation cannot mutate the active release accidentally.

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

On a terminal, commands render readable labels or tables. Redirected output is
compact JSON. Output flags are global and therefore appear before the command:

```shell
wf --pretty app show example
wf --json app,commit,bundle_digest app publish .
wf --json app,state --jq '.state' run show <RUN_ID>
wf --template '{{.app}} {{.state}}' run show <RUN_ID>
```

`--json` accepts comma-separated top-level fields or `*`. Unknown fields fail
with the available field names instead of silently returning empty data.
`--jq` uses jq syntax, and `--template` uses a Go template. Standard output is
reserved for results; progress and diagnostics use standard error.

## Run and inspect work

```shell
wf run create example health --input '{"ping":true}'
wf run wait example parse --input-file input.json --timeout 30s
wf run show <RUN_ID>
wf run watch <RUN_ID> --result
wf run result <RUN_ID>
wf run cancel <RUN_ID> --reason "operator request"

wf job list --app example --status running
wf job show <JOB_ID>
wf job logs <JOB_ID> --tail-bytes 65536
```

`--input-file -` reads JSON from standard input. Job logs are written as the raw response so they can be piped.

`wf run watch` prints state changes to standard error, polls no faster than
100 ms, and prints the terminal Run or successful result to standard output.
Its default timeout is ten minutes.

## Shell completion and help

Help, version, and completion do not require a configured context or login:

```shell
wf app publish --help
wf help release
wf completion bash
wf completion zsh
wf completion fish
wf completion powershell
```

Load the generated script using the normal mechanism for the selected shell.

## Call the API directly

`wf api` is the authenticated escape hatch for Control Plane operations that
do not yet have a high-level command:

```shell
wf api apps
wf api git_sources
wf api git_sources/12/sync --field expected_commit=<FULL_COMMIT>
wf api /healthz
wf api provisioning/apply --method POST --input request.json
```

A relative endpoint is resolved below `/api/w/<workspace>/`. A path beginning
with `/` is resolved on the selected context host. Absolute URLs, scheme-relative
URLs, fragments, and parent traversal are rejected so the selected credential
cannot be redirected to another host. `--field` converts booleans, null,
numbers, arrays, and objects; `--raw-field` always sends a string.

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
| `1` | The requested operation completed unsuccessfully |
| `2` | Invalid command or arguments |
| `3` | Invalid local context or configuration |
| `4` | Authentication is required or invalid |
| `5` | The authenticated principal is not authorized |
| `10` | Local I/O or HTTP transport failure |
| `20` | Control Plane returned a 4xx response |
| `21` | Control Plane returned a 5xx response |

JSON API errors are written to standard error. This preserves the automation
contract while keeping authentication, authorization, client, and server
failures distinct.

## Troubleshooting

- Exit `4`: run `wf auth status`, then `wf auth login`. On a remote shell use
  `wf auth login --no-browser`.
- Exit `5`: the credential is valid but lacks access to the selected hosted
  tenant or workspace. Check `wf context show` and `wf workspace show`; do not
  replace the credential with a Cloud management token.
- `Git worktree has uncommitted changes`: commit the intended files. Use
  `--allow-dirty` only when deliberately publishing committed `HEAD`.
- `409 Conflict` with `expected_commit`: push `HEAD` to the selected remote
  branch and retry. The CLI will not publish a different branch tip.
- `secure credential storage is unavailable`: restore Windows Credential
  Manager, macOS Keychain, or Linux Secret Service. The client does not fall
  back to a plaintext token file.
- A redirected API response is rejected. Configure the context with the final
  canonical Cell URL instead of relying on an HTTP redirect.

Diagnostics and JSON errors redact credential fields, Bearer values, OAuth
codes, and Windforce token prefixes before writing standard error.

## Repository helper

`tools/windforce_control.py` is a repository-local API helper. Released `wf` archives are the installed client contract. Repository maintenance can continue using the helper without making Python a dependency of product repositories.
