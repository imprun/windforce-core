# ADR 0006: Link to the forge instead of mirroring app source

## Status

Accepted. Amends the app-detail screens of [ADR 0004](0004-web-ui-rewrite.md);
companion to the aggregate-first direction of
[ADR 0005](0005-aggregate-job-observability.md).

## Context

The rebuilt app detail had a Source tab: a file browser that rendered the
materialized repository snapshot returned by `GET /apps/{app}/source`.

Reading app code is not an admin task. The repository host (GitHub, GitLab)
is where code is browsed, reviewed, and blamed — with real tooling. A
read-only mirror in the admin UI duplicates that poorly, transfers whole
source trees to answer questions nobody asks there, and invites the UI to
grow viewer features (syntax highlighting, search) that the forge already
has.

## Decision

- The Source tab and file viewer are removed from the Web UI.
- Wherever the UI references app code, it links to the repository host,
  pinned to the release commit:
  - the app Overview links the active contract's source tree
    (`.../tree/{commit}/{subpath}` on GitHub,
    `.../-/tree/{commit}/{subpath}` on GitLab);
  - release history commits link to the forge commit page.
- URL construction recognizes github.com and GitLab hosts (including
  self-managed hosts with `gitlab` in the hostname) from the registered
  `repo_url`, converting `git@host:path` SSH remotes to their https form.
  For unrecognized hosts (e.g. local file paths in development) the UI shows
  the repo URL and commit as plain text.
- The control-plane endpoint `GET /apps/{app}/source` is unchanged; workers
  and tooling still rely on materialized bundles. This is a UI-surface
  decision only.

## Consequences

- The admin UI never transfers source trees; the largest payload it renders
  is an action schema.
- Code inspection lands where it belongs — on the forge, at the exact pinned
  commit workers run, including the subpath scope.
- Local-path development repositories lose in-UI code browsing; the CLI and
  the working tree itself cover that case.
