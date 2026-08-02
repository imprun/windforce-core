# ADR 0022: Make TypeScript a strict Tier 1 runtime

## Status

Accepted (2026-08-02).

## Context

Core supports TypeScript through Bun, alongside Python and Go. Several preparation and publication paths treated every unrecognized `scriptLang` as TypeScript even though the executor rejected unknown values. That disagreement could prepare and publish a release through Bun only to fail when a Worker selected its launcher.

The TypeScript publication check also proved only that Bun could build the entrypoint dependency graph. It did not require the named `main(coreCtx)` export and therefore allowed an invalid App interface to become an active Release. Importing the App to inspect its exports would detect the problem but could execute arbitrary top-level author code during publication.

Some author repositories generate a deployment artifact from code and SDK-specific description output. Core needs a clear boundary so that this convenience does not turn SDK-specific discovery into a Core runtime responsibility.

## Decision

1. `scriptLang` is normalized centrally to `typescript`, `python`, or `go`. An omitted value defaults to `typescript` for Manifest compatibility. Every other value is rejected before source preparation, fingerprinting, publication, or execution.
2. TypeScript is a Tier 1 launcher contract, not a fallback for unknown languages.
3. TypeScript publication uses Bun's static scanner to require a named `main` export, then uses `bun build --target=bun` to validate the dependency graph. Neither step imports or executes the App.
4. Publication continues to install the exact Bun lockfile with `bun install --frozen-lockfile --no-progress` and fingerprints the Bun runtime, platform, and injected Core Author SDK.
5. Core starts at the canonical deployment artifact: `windforce.json`, schema files, the entrypoint, and opaque dependencies. An App-owned builder may run `--describe`, externalize schemas, and bundle dependencies before producing that artifact. Core does not understand or execute the SDK discovery protocol.
6. Conformance includes a hermetic arbitrary-dependency test and an opt-in E2E against the real `demo` and `sample` TypeScript Apps. The E2E creates a deployment Git externally, then exercises Register, Sync, Publish, Run, and Result through both local and remote Workers.

## Consequences

- Unsupported language typos fail at the first contract boundary instead of silently selecting Bun.
- A TypeScript release cannot become active without the named Core entrypoint.
- Publication remains safe from App top-level side effects while still validating the complete import graph.
- SDK-specific author tooling can evolve independently because Core sees only its canonical output.
- Adding another first-class language requires an explicit launcher, preparation fingerprint, validation path, tests, documentation, and a new decision rather than a default branch.

## Rejected alternatives

- **Treat every unknown language as TypeScript.** Rejected because preparation and execution disagree and operator errors become late failures.
- **Import the entrypoint to inspect `main`.** Rejected because module evaluation executes untrusted App code during publication.
- **Run `bun main.ts --describe` inside Core.** Rejected because it is an SDK-owned authoring protocol, returns a non-canonical schema shape, and executes the App before the deployment boundary.
- **Validate only with `bun build`.** Rejected because Bun can successfully bundle an entrypoint that does not export `main`.
