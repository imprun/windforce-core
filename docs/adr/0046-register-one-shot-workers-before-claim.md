# ADR 0046: Register one-shot Workers before claim

## Status

Accepted (2026-08-18). Extends ADR 0010, ADR 0023, ADR 0025, ADR 0030, and ADR 0038.

## Context

The long-running Worker loop discovers its execution profiles, registers an active Worker record, claims Jobs through that registered identity, transitions to draining on shutdown, and deregisters after the active Job finishes. The `worker --once` path instead called the low-level `ProcessOne` method directly. That worked for the trusted unregistered static-token compatibility path, but a managed `wfr_` credential requires the claim Worker ID to be registered by the same credential generation. A one-shot managed Worker therefore received a forbidden claim before it could execute an eligible Job.

Execution-profile labels do not belong in the managed credential's operator label set. ADR 0030 keeps operator labels under credential authority and derives reserved `sys/execution-profile-*` labels from the structured profiles discovered and registered by the Worker. One-shot execution must preserve that separation while participating in the same registry identity boundary as a long-running Worker.

## Decision

1. `Processor.RunOnce` is the lifecycle entry point for a Worker that processes at most one Job. It discovers execution profiles, registers one active Worker record with the authored labels and structured profiles, performs one claim and execution attempt, and deregisters the Worker before returning.
2. Both local and remote `worker --once` CLI paths call `RunOnce`. `ProcessOne` remains the lower-level primitive for callers and tests that already own Worker registration or deliberately use the unregistered static compatibility path.
3. `RunLoop` and `RunOnce` construct their registry records through one helper so Worker ID, group, build identity, tags, authored labels, execution profiles, slots, and status do not drift between lifecycle modes.
4. Managed credential labels remain the exact operator-authored label scope defined by ADR 0025. Core continues to derive execution-profile labels from the registered profiles when it builds the authoritative claim selector defined by ADR 0038.
5. Deregistration after a one-shot attempt uses a bounded shutdown-independent context. A cleanup failure is logged but does not replace the already fenced Job outcome with a second execution failure; registry liveness expiry remains the fallback.

## Consequences

- Ephemeral schedulers may launch `worker --once` with managed credentials without pre-creating Worker registry rows or using the static compatibility token.
- A one-shot Worker becomes visible as managed capacity only while its process is active and cannot claim outside its registered group, authored labels, derived execution profiles, or credential workspace allowlist.
- No Worker Plane HTTP shape, credential field, Job schema, or State Store migration changes.
- Fleet scaling and infrastructure lifecycle remain outside Core; Core owns only the neutral one-shot Worker registration, claim, completion, and deregistration sequence.

## Verification

- A focused Processor test requires the order `register -> claim -> deregister` and verifies that registration preserves authored labels while claim adds the detected execution-profile label.
- Existing managed Worker Plane tests continue to enforce exact credential operator labels, registered claim identity, workspace scope, rotation, revocation, and drain fencing.
- The complete Core test suite and `go vet ./...` pass for local and remote execution paths.
