# ADR 0004: Web UI rewrite on a static Vite SPA

## Status

Accepted

## Context

The first admin Web UI was a Next.js app exported with `output: "export"` and
embedded into the Go binary. That worked, but the fit was poor for a small
embedded admin surface:

- Next.js static export needed `basePath`/`assetPrefix` workarounds, custom
  build ids, and dev-server rewrites to live under `/ui/`.
- The export emitted framework routing artifacts (`__next.*.txt`, per-route
  HTML) that bloated the embedded asset tree without adding value for a
  single-page admin console.
- The dependency surface (Next.js + React server components toolchain) was
  large relative to the product need: a handful of admin screens against one
  JSON API.
- The UI never grew the run/job inspection screens that
  [ADR 0003](0003-lightweight-admin-ui.md) put in scope.

The UI was removed and re-planned from scratch. The screen model lives in
[docs/web-ui-model.md](../web-ui-model.md).

## Decision

- The Web UI is a client-side-rendered SPA built with **Vite + React +
  TypeScript**, living in `web/`.
- Runtime dependencies are exactly `react` and `react-dom`. Routing is a
  small in-tree history-API router; state is React hooks; styling is one
  hand-written stylesheet with design tokens. No component or data library.
- `vite build` emits static files to `web/dist` with `base: "/ui/"`. The
  build output is committed into `internal/webui/assets` and embedded into
  the Go binary with `go:embed`, so `go build` from a fresh clone keeps a
  working UI. The Dockerfile rebuilds the UI from source and overwrites the
  embedded assets, so the image never depends on the committed copy being
  fresh.
- The Go server serves the SPA at `/ui/` with an index-html fallback for
  client-side routes (any `/ui/...` path that does not match a static file).
- Development uses the Vite dev server (run with Bun) on
  `WINDFORCE_LITE_WEB_PORT`, proxying `/api`, `/healthz`, and `/readyz` to
  the control plane, same as before. Docker Compose keeps a `web` service
  running the dev server for the local devstack.
- The UI talks only to the documented control-plane API and keeps no
  separate source of truth. Workspace, API token, and actor are browser
  localStorage settings sent per request (`Authorization: Bearer`,
  `X-Windforce-Actor`).

## Non-goals

Unchanged from ADR 0003: no console parity, no tenant/billing/quota UI, no
workflow designer, no adapter administration, no source editing.

## Consequences

- The embedded asset tree is a plain `index.html` + hashed `assets/*` bundle;
  the Go side needs the SPA fallback but no other framework awareness.
- Deep links like `/ui/jobs/{id}` are shareable because the server falls back
  to `index.html` and the client router resolves the path.
- Job/run inspection becomes a first-class UI area (Jobs list, summary, job
  detail with result and logs), closing the ADR 0003 scope gap.
- Upgrading the UI toolchain is a Vite version bump; there is no framework
  routing contract coupled to the Go server beyond `/ui/` + fallback.
