# ADR 0050: Pause Worker claims under local resource pressure

## Status

Accepted (2026-08-18). Extends ADR 0010, ADR 0023, ADR 0025, ADR 0038, ADR 0039, and ADR 0046.

## Context

Core can observe Worker liveness, slots, leases, build identity, and placement compatibility, but a live Worker previously had no neutral way to report that local memory, CPU, or file-descriptor pressure made another claim unsafe. An external fleet operator may scale or replace infrastructure, but that control loop is too slow and too deployment-specific to protect an already busy Worker at the claim boundary.

Resource pressure is not Worker lifecycle. A pressured Worker is still alive and must keep heartbeating and completing its already owned lease. Treating pressure as `draining` or `offline` would obscure operator intent and could incorrectly terminate or replace healthy running work.

## Decision

1. A long-running or one-shot Worker samples provider-neutral memory, CPU, and file-descriptor measurements. Linux prefers cgroup v2 data. If no usable cgroup measurement exists, it uses host observations. Other platforms explicitly report an unsupported/unknown observation in version 1 rather than guessing.
2. Cgroup memory uses `memory.current` and a finite `memory.max`; this includes the Worker and child App processes placed in that cgroup. `memory.max=max` has no comparable ratio and is never interpreted as zero or as a high-pressure limit. Host memory sums RSS for the Worker process tree and compares it with host `MemTotal`. Host CPU uses normalized load; cgroup CPU uses the 10-second `cpu.pressure` average. File-descriptor usage and limit are for the Worker process because the OS limit is per process.
3. The default high watermark is `0.90`, the low watermark is `0.80`, and the minimum sample interval is five seconds. Any supported comparable measurement at or above the high watermark pauses new claims. A paused Worker resumes only when every currently comparable measurement is strictly below the low watermark. An unknown sample fails open before the first pause, but cannot resume a Worker already paused by a known high sample.
4. Registration and heartbeat carry an optional `resource_pressure` observation with `accepting_claims`, a stable reason code, scope, numeric measurements, `observed_at`, and a server-bounded `fresh_until`. Free-form error text, filesystem paths, environment values, credentials, and tokens are not part of the contract.
5. The Worker avoids polling while its local controller is paused, and the State Store repeats the decision at the final registered-claim boundary. Local, remote Worker Plane, Local JSON, and PostgreSQL modes use the same gate. A legacy Worker with no pressure observation remains claim-compatible.
6. Pressure remains separate from `active`, `draining`, and registry liveness. Group inventory reports accepting, pressure-paused, and stale-pressure Worker counts plus stable pressure reasons. Claimable slot totals exclude pressure-paused Workers. Placement candidates use `resource_pressure` when an otherwise matching Worker is paused.
7. A pressure transition never cancels or evicts a running Job. Registry heartbeat, Job lease heartbeat, logs, completion, cleanup, and the explicit shutdown drain contract continue. A stale paused observation remains paused until the Worker reports recovery or disappears from the live registry; freshness is diagnostic, not an automatic unsafe resume.
8. `--resource-pressure-disabled` is an explicit compatibility escape hatch. High/low watermarks and the sample interval are configurable on `worker` and standalone mode. Core does not set container limits, resize replicas, terminate machines, or own provider-specific autoscaling.

## Consequences

- Core gains a fast local safety gate without absorbing WorkerPool or infrastructure ownership.
- Hosted Cloud and self-hosted operators can read the same observed state and decide whether to alert, scale, or replace capacity.
- Cgroup placement determines whether child workloads are included in the preferred measurement. Deployments that put child processes in another cgroup must not interpret the Worker's cgroup value as whole-workload memory.
- Unknown support and stale freshness remain visible. They are not silently converted to zero usage or to an offline lifecycle state.
- Existing registered Workers and third-party backends remain compatible because the new field and pressure heartbeat interface are optional.

## Verification

- Controller tests cover high/low hysteresis, unknown observations, and cached sampling.
- Linux fixture tests cover cgroup v2 unlimited memory and process-tree RSS including a child process.
- Local and PostgreSQL conformance tests persist pressure, reject a paused registered claim, resume after a low heartbeat, and verify the PostgreSQL `jsonb` migration.
- Real Worker Plane and remote-client tests cover registration, heartbeat, legacy empty heartbeat, final server-side claim denial, and redacted validation errors.
- Processor tests prove one-shot pause behavior and that a high transition reported during a running Job does not cancel that Job.
- Cached-observation and registered-claim benchmarks exercise 16 checks per iteration. The review gate is no more than 10% median regression for a healthy pressure observation versus the legacy record. On the 2026-08-18 Windows/amd64 development run, the medians were 7,317 ns versus 6,935 ns per 16 claim checks (5.5%), with identical allocations; the cached controller observation median was 3,695 ns per 16 reads. These numbers are evidence for this change, not a portable timing assertion in the test suite.
