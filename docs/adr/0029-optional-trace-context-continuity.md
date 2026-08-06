# ADR 0029: Preserve optional trace context across asynchronous execution

## Status

Accepted (2026-08-06). Promotes and supersedes the candidate-only scope recorded in GitHub issue #128.

## Context

Core execution may begin behind an instrumented gateway or adapter, but it must also support direct Invocation and Control Plane calls, standalone workers, worker fixtures, and App or SDK development harnesses. None of those entry points can assume that an upstream `traceparent` exists. Requiring one would make observability a runtime dependency and would break valid isolated tests.

The PostgreSQL Run and Job queue, remote Worker Plane, and launcher process are asynchronous or process boundaries. An ambient in-memory trace context cannot cross them without an explicit carrier. The existing `correlation_id` is a caller-owned business correlation value and cannot substitute for a W3C trace ID.

Core is backend-neutral. A self-hoster may export OTLP to an OpenTelemetry Collector, Grafana Alloy, or another compatible endpoint and may store traces in Tempo or another backend. Core must not encode a Tempo-, Loki-, VictoriaMetrics-, Prometheus-, or Grafana-specific execution contract.

## Decision

1. Core uses W3C Trace Context as the canonical distributed trace carrier. `traceparent` and `tracestate` are optional inputs. Baggage is not persisted or injected in the first version because it creates an unnecessary secret and cardinality surface.
2. Every independently invocable instrumented boundary follows **optional-in, valid-out** when tracing is enabled: continue a valid ambient context, otherwise extract a valid carrier, otherwise start a new root trace. A missing or malformed carrier never rejects an HTTP request, message, Job, or App execution. The new root records a bounded origin and propagation reason so normal isolated execution can be distinguished from invalid input.
3. HTTP ingress reads the standard `traceparent` and `tracestate` headers. A protocol adapter carries the same fields in that protocol's metadata. Admission persists the resulting creation context with the immutable Run and Job execution metadata. PostgreSQL is a durable context carrier; queue wait time is not represented by holding an Admission span open.
4. A Worker extracts the context pinned to the claimed Job. A legacy, direct, or test Job without a valid context starts a Worker root trace. Local and remote workers preserve identical behavior, and the Worker Plane carries the trace fields without requiring database access from a remote worker.
5. Every execution attempt has its own processing span and records the effective trace ID on Job inspection metadata. A normal single-Job delivery may use the creation context as the processing parent so Gateway-to-Action work appears in one trace. Batch or fan-out processing uses span links. Retry, replay, or a future suspend/resume that begins a new execution attempt starts a new trace linked to the creation context and previous attempt; an in-process HumanTask hold remains in the current attempt and trace.
6. The launcher injects the current context through Core's private process transport and exposes a read-only optional telemetry carrier through the Core host context. A Core Author SDK continues that context when present. An opaque Application SDK may adapt the carrier and create App- or Action-level spans, or start its own root when it is run directly without Core. Core never detects the Application SDK or makes its vocabulary part of the engine.
7. Core instruments its own backend-neutral spans and exports them with the OpenTelemetry SDK through OTLP configuration. Server, Worker, and standalone roles use distinct low-cardinality resource identity. No Tempo-specific endpoint, tenant, dashboard, or query rule becomes an engine API.
8. Core service logs may include `trace_id` and `span_id` fields for log-to-trace navigation. Secret-masked Job logs remain the authoritative workspace-scoped App log contract and are not implicitly duplicated to an external log backend.
9. `correlation_id`, Run ID, Job ID, Workspace, App, Action, Worker group, release commit, and bundle digest remain operational attributes with their existing meanings. High-cardinality identifiers may be span attributes but must not become metric labels or log-index labels. Inputs, results, credentials, tokens, secret values, and unrestricted baggage are never trace attributes.
10. Standard `OTEL_*` configuration controls enablement, exporter endpoint, sampling, resource attributes, and shutdown flushing. When tracing is disabled, the APIs, queue, launcher, and App behavior remain unchanged.

## Required trace shape

```text
Gateway or adapter ingress (optional)
  -> Core HTTP ingress or Admission
    -> persist Run + Job + trace creation context
      -> Worker Job processing
        -> bundle fetch and launcher
          -> Core Author SDK
            -> opaque Application SDK (optional)
              -> App and Action work
```

Any node in this sequence may be the first instrumented node. It continues a valid parent when one exists and otherwise becomes a root.

## Verification

- A request with a valid `traceparent` preserves its trace ID through Admission, Job persistence, local or remote Worker claim, launcher transport, and the Core Author SDK.
- A Control Plane or Invocation request without `traceparent` creates a Core Server root and the Worker continues it.
- A Job without stored trace context creates a Worker root and remains executable.
- A directly executed Author SDK or Application SDK fixture without Core context creates its own root when that SDK's tracing is enabled.
- A malformed `traceparent` creates a new root, records `new_invalid`, and does not change the API or Job outcome.
- Tracing disabled preserves all existing behavior and produces no required telemetry side effect.
- Retry, replay, remote Worker, and HumanTask tests prove the attempt and link semantics above.

## Consequences

- One normal request can be followed from an external ingress through asynchronous queueing and process execution without keeping an HTTP or Admission span open for the Job duration.
- Core, Worker, and App development remain independently testable because every boundary can become a valid trace root.
- Adapters and Application SDKs integrate through a standard carrier instead of importing Core internals or sharing a tracing backend dependency.
- Persisted trace context and Worker Plane fields become execution semantics and require compatibility tests and version-aware rollout.
- Metrics, log shipping, backend deployment, dashboards, and alert rules remain separate operational concerns even when Grafana correlates them with traces.
