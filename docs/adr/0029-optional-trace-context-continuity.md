# ADR 0029: Preserve optional trace context across asynchronous execution

## Status

Accepted (2026-08-06), amended after [Discussion #202](https://github.com/imprun/windforce-core/discussions/202). Promotes and supersedes the candidate-only scope recorded in GitHub issue #128.

## Context

Core execution may begin behind an instrumented gateway or adapter, but it must also support direct Invocation and Control Plane calls, standalone workers, worker fixtures, and App or SDK development harnesses. None of those entry points can assume that an upstream `traceparent` exists. Requiring one would make observability a runtime dependency and would break valid isolated tests.

The local JSON or PostgreSQL State Store, remote Worker Plane, and launcher process are asynchronous or process boundaries. An ambient in-memory trace context cannot cross them without an explicit carrier. The existing `correlation_id` is a caller-owned business correlation value and cannot substitute for a W3C trace ID.

A remote Worker claim also has two valid but different contexts. The `/worker/v1/claims` client and server spans describe polling transport. The claimed Job carries the execution creation context selected at Admission. Treating the polling context as the Job parent would disconnect the execution from the Gateway or Adapter that created it. The same distinction applies to standalone even though its server and worker share a process.

Core is backend-neutral. A self-hoster may export OTLP to an OpenTelemetry Collector, Grafana Alloy, or another compatible endpoint and may store traces in Tempo or another backend. Core must not encode a Tempo-, Loki-, VictoriaMetrics-, Prometheus-, or Grafana-specific execution contract.

## Decision

1. Core uses W3C Trace Context as the canonical distributed trace carrier. `traceparent` and `tracestate` are optional inputs. Baggage is not persisted or injected in the first version because it creates an unnecessary secret and cardinality surface.
2. At an HTTP or protocol ingress, Core follows **optional-in, valid-out**: continue a valid ambient context, otherwise extract a valid carrier, otherwise start a new root when the local OpenTelemetry SDK is enabled. A missing, malformed, or oversized carrier never rejects an HTTP request, message, Job, or App execution. Invalid raw values are not logged; Core records only a bounded origin and propagation reason such as `continued`, `new_missing`, or `new_invalid`.
3. Admission validates and canonicalizes the effective carrier, then stores a versioned creation context outside the Application input with the immutable Run and Job execution metadata. Version 1 contains bounded `traceparent`, bounded `tracestate`, origin, and propagation reason. The selected State Store, whether local JSON or PostgreSQL, is the durable carrier across queue delay. Queue wait is not represented by holding an Admission span open.
4. Job execution does not use the ambient Worker polling or claim transport context as its parent. Attempt 1 uses the stored creation context as its processing parent so a normal Gateway-to-Action path stays in one trace. A Job without a valid creation context starts a Worker root when the Worker SDK is enabled. Local, remote, and standalone workers preserve the same rule.
5. A lease recovery that reclaims the same Job as `attempt > 1` starts a new root trace linked to the immutable creation context. Version 1 does not persist attempt span contexts and does not require a previous-attempt link, because a Worker may disappear before reporting one. Job ID and attempt attributes preserve the ordered operational identity. An Invocation idempotency replay returns the existing Run and Job without changing their creation context or creating another attempt. A caller-requested new Run receives a new creation context.
6. An in-process HumanTask hold remains in the same attempt and trace. A future suspend/resume that creates another attempt starts a new root linked to the creation context. Parent-child propagation is used for one causal parent; span links are used for a new attempt or work with multiple causal parents. Batch or fan-out alone does not force link semantics.
7. The Worker Plane carries optional versioned telemetry metadata next to the Job and lease, not inside Application input. Older Workers may ignore it. A newer Worker claiming a legacy Job without the field starts a root. Raw `tracestate` is never exposed through the Web UI, ordinary Job APIs, or Job logs. The local State Store writes validated metadata to its JSON state file, so validation, size bounds, file permissions, and normal Run/Job retention apply.
8. The launcher injects only the Job execution context through Core's private process transport and exposes a read-only optional telemetry carrier through the Core host context. It never exposes Worker polling or Worker Plane authority. A Core Author SDK continues that context when present. An opaque Application SDK may adapt the carrier and create App- or Action-level spans, or start its own root when it is run directly without Core. Core never detects the Application SDK or makes its vocabulary part of the engine.
9. Core instruments backend-neutral spans and exports them with the OpenTelemetry SDK through OTLP configuration. Standard `OTEL_*` configuration controls SDK enablement, exporter endpoint, sampling, resource attributes, and shutdown flushing; Core does not add a separate sampling trust policy. Carrier validation and forwarding remain available during mixed-role or mixed-version rollout even when a role does not export spans. Exporter absence, timeout, or shutdown flush failure never changes execution state.
10. Server, Worker, and standalone use role-specific low-cardinality default resource identity unless standard OTel resource configuration overrides it. Standalone uses one process resource and distinguishes server and worker components on spans. HTTP and process instrumentation follows applicable OpenTelemetry semantic conventions; Core-specific execution spans and attributes use a versioned `windforce.*` namespace.
11. Core service logs may include `trace_id` and `span_id` fields for log-to-trace navigation. Secret-masked Job logs remain the authoritative workspace-scoped App log contract and are not implicitly duplicated to an external log backend.
12. A Run is the stable caller-visible invocation, a Job is the durable internal work item, and an Attempt is one lease-fenced Worker execution of that Job. `correlation_id`, Run ID, Job ID, attempt, Workspace, App, Action, Worker group, release commit, and bundle digest retain their operational meanings. High-cardinality identifiers may be span attributes but must not become metric labels or log-index labels. Inputs, results, credentials, tokens, secret values, raw invalid carriers, and unrestricted baggage are never trace attributes.

## Required trace shape

```text
Gateway or adapter ingress (optional)
  -> Core HTTP ingress or Admission
    -> persist Run + Job + creation context in the State Store
      -> attempt 1: Worker processing continues the creation trace
      -> attempt >1: Worker root links to the creation context
        -> bundle fetch and launcher
          -> Core Author SDK
            -> opaque Application SDK (optional)
              -> App and Action work
```

Remote claim transport remains separate:

```text
Worker poll client -> /worker/v1/claims server
                           -> returns Job + lease + optional telemetry metadata

Job processing parent/link = Job creation context, never the poll context
```

Any ingress may be the first instrumented node. An execution boundary uses the Job context when one exists and otherwise becomes a root; it does not select an unrelated ambient transport context.

## Verification

- A request with a valid `traceparent` preserves its trace ID through Admission, both State Store implementations, attempt 1, launcher transport, and the Core Author SDK.
- A Control Plane or Invocation request without `traceparent` creates a Core Server root and the Worker continues it.
- A Job without stored trace context creates a Worker root and remains executable.
- A directly executed Author SDK or Application SDK fixture without Core context creates its own root when that SDK's tracing is enabled.
- A remote Worker with an active claim polling trace still parents Job processing from the stored execution context, not from the polling trace. Standalone proves the same invariant in process.
- Lease recovery creates a new trace linked to creation without requiring a previous-attempt context. A different inbound carrier on idempotency replay neither changes the request fingerprint nor overwrites creation context.
- Missing, malformed, sampled, unsampled, and oversized carriers do not change the API or Job outcome and never echo invalid raw values.
- Local/PostgreSQL, local/remote/standalone Worker, older/newer Worker, and mixed exporter enablement tests preserve execution behavior.
- Exporter failure and tracing-disabled roles preserve all existing API, queue, launcher, cancellation, timeout, HumanTask, and App behavior.

## Consequences

- One normal request can be followed from an external ingress through asynchronous queueing and process execution without keeping an HTTP or Admission span open for the Job duration.
- Core, Worker, and App development remain independently testable because every boundary can become a valid trace root.
- Adapters and Application SDKs integrate through a standard carrier instead of importing Core internals or sharing a tracing backend dependency.
- Persisted creation context and Worker Plane fields become execution semantics and require nullable schema changes, compatibility tests, and version-aware rollout. Version 1 deliberately avoids durable per-attempt span history.
- Transport and execution traces remain causally accurate even when a remote Worker polls continuously or standalone combines roles in one process.
- Metrics, log shipping, backend deployment, dashboards, and alert rules remain separate operational concerns even when Grafana correlates them with traces.
