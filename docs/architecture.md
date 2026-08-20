---
title: Architecture
description: Control, Invocation, Trigger, and Worker Plane ownership in Windforce Core.
---

Windforce Core separates deployment management, Run invocation, protocol ingress, and worker execution into planes with package boundaries and system-to-system HTTP specifications. Those boundaries are deployed through three process roles: `server`, `worker`, and `standalone`.

Core is a general-purpose execution and integration engine rather than a multi-language or provider runtime platform. Bun/TypeScript is the Tier 1 App path, Python and Go preserve existing compatibility contracts, and expensive provider-native facilities are bound as external capability services. See [Product boundary](concepts/product-boundary.md) and [ADR 0047](adr/0047-define-bun-typescript-app-and-external-capability-boundary.md).

[Run admission architecture](concepts/run-admission.md) is the human-readable current reference for the relationship between SDKs, the Invocation API, AdmissionService, Triggers, Gateways, Runs, Jobs, workspaces, and Core instances. The OpenAPI served at `/api/v1/openapi.json` is the machine-readable Invocation contract. ADRs preserve decision history and rationale rather than replacing these current-state documents.

[Runtime configuration and secrets](concepts/runtime-configuration.md) is the
current reference for Variable, Resource, InputConfig, SecretBackend, Job
runtime access, and worker resolution.

```text
operators / CI / clients / external adapters
                    |
                    v
       server: HTTP planes + Web UI
          |                 |                  ^
          |                 |                  |
          |                 |          Router Provider
          |                 |          (K8s or hosted)
          |                 |
          |                 +-- Webhook Dispatcher ---> signed HTTPS endpoint
          v
 Source Store ---> active release catalog ---> Execution Artifact Store
                          |
                          +-- admission ---> PostgreSQL queue
                                                   |
                                                   v
                                             runtime workers
                                                   |
                                                   +-- local bundle cache
```

## Control Plane

The Control Plane owns repository registration, source validation, release
publication, active release selection, configuration, and audit history. Its
API is rooted at `/api/w/{workspace}`.

A workspace is a registered organizational and authorization partition inside
one engine instance. Workspace tokens cannot cross workspace paths. The worker
pool, bundle storage, database, and encryption root remain instance-wide, so
mutually untrusting tenants require separate engine instances. Workspace
lifecycle and access rules are defined in [Workspaces](concepts/workspaces.md).

Synchronization materializes the source revision and action schemas without
changing the active catalog. Publishing prepares the latest synchronized
revision as a worker-ready execution bundle before the catalog points at that
release. Workers and protocol adapters never clone a repository or read
repository credentials.

The Source Store contains exact source snapshots keyed by workspace, repository
source ID, and commit. The Execution Artifact Store contains complete prepared
trees keyed by SHA-256 digest. A worker keeps only a disposable local copy of a
pinned execution bundle. The current filesystem-backed deployment requires the
release builder and workers to access the same persistent artifact root. See
[Core concepts](concepts/core-concepts.md) for the store, cache, fingerprint,
and marker definitions, and
[Release and execution lifecycle](concepts/release-lifecycle.md) for the state
transitions.

Core prunes a completed Source Store snapshot only when active deployments,
immutable release history, latest release candidates, and source release
markers do not reference it and its completion marker is older than the
configured grace period. Invalid or incomplete marker state is retained. The
default seven-day grace period can be disabled with
`--source-bundle-grace-period=0`, and
`--source-bundle-retention-dry-run` observes eligible snapshots without
deleting them. See [ADR 0028](adr/0028-prune-unreferenced-source-bundles.md).

The selected state backend owns the active release catalog. A publication writes
the active release, immutable release history, source release marker, and audit
record in one transaction. Local mode persists the catalog in its state JSON
file; PostgreSQL mode persists it in tables shared by the server and workers.

Release placement defaults do not own live execution placement. Workspace-scoped
operator Execution Placement Policy is stored independently, survives publication and
rollback, and is combined with the active release only at admission. The
canonical manifest can be committed or generated before Core synchronizes it.
See [Execution placement](concepts/execution-placement.md).

The same publication transaction stores a CloudEvents-compatible Control Plane
event and one pending delivery for each enabled matching Webhook subscription.
Endpoint and signing-secret values use workspace encryption. External HTTP
delivery is always outside the publication transaction.

The Control Plane API owns workspace-scoped Webhook subscription CRUD, test
events, delivery history, and failed-delivery retry. Read responses expose only
the endpoint scheme and host. A signing secret is returned only by the create
or rotation response, and every management change is included in the canonical
workspace audit stream. Deleted subscriptions remain visible only through the
explicit history query while pending deliveries are canceled.

Instance-level capacity observers use the separate
[queue demand observation](concepts/queue-demand-observation.md) contract.
`POST /api/queue-demand-snapshots` evaluates multiple workspace/tag/label
selectors against one persistent store fence. It is an admin-only observation
surface; it does not reserve jobs or introduce hosting-product vocabulary into
Core.

## Webhook Dispatcher

The server runs a Webhook Dispatcher alongside its HTTP listener. The dispatcher reads only encrypted subscriptions, immutable event bodies, and delivery state. It claims work with a lease, signs the CloudEvents body, sends it outside the release transaction, and records success, terminal failure, or a scheduled retry. Every server replica may run the loop; PostgreSQL row locks prevent duplicate active claims while expired leases remain recoverable.

Each attempt resolves DNS again and connects directly to an address that passed
the egress policy. HTTPS is required except for explicitly enabled local HTTP
loopback. Redirects, link-local and metadata addresses are rejected. Private
addresses require a configured host or CIDR allowlist. Logs identify the
delivery and event type without endpoint paths, queries, signing secrets, or
response bodies.

## Trigger Plane

The Trigger Plane is a set of protocol adapters. A protocol adapter owns only
its inbound protocol and compatibility policy:

- route and message parsing
- caller authentication and request budgets
- mapping protocol fields to `app`, `action`, and `input`
- correlation and idempotency metadata
- mapping the generic run result to a protocol response

In-tree adapters running in the server call `AdmissionService` in-process.
External adapters and other languages call the versioned Invocation API with a
scoped `wfs_` Service Principal. Both transports preserve the same principal
authorization, release resolution, schema validation, idempotency, and durable
Run admission semantics. Adapters do not write queue tables or read catalog
files.

## Invocation and Execution Plane

The Invocation Plane owns caller authentication, Run admission, caller-visible
status/result/cancel, and app/action schema discovery. Its system-to-system HTTP
specification is rooted at `/api/v1`. The internal Execution Plane owns the
PostgreSQL Job queue, runtime workers, execution results, and job-scoped runtime
callbacks. Workers use `/worker/v1` and job callback APIs; they do not use the
Invocation API for claim or completion.

Run admission performs one atomic decision:

1. Resolve the active app release in the requested workspace.
2. Validate the action and worker capability routing.
3. Resolve InputConfig once, enforce LockedKeys, validate `$var/$res` references without reading Secret plaintext, close and pin runtime access, and validate the merged input against the active action schema.
4. Materialize the action input and output schemas.
5. Pin the effective input, deployment, commit, entrypoint, runtime, schemas, route, and timeout.
6. Create the caller-visible Run and its first internal Job in one transaction.

A Run is the stable caller-visible invocation. A Job is an internal execution
attempt. Workers execute only the deployment pinned in the Job payload; they do
not resolve the active catalog again.

## Invocation API

- `POST /api/v1/workspaces/{workspace}/runs`
- `POST /api/v1/workspaces/{workspace}/runs/wait`
- `GET /api/v1/workspaces/{workspace}/runs/{run_id}`
- `GET /api/v1/workspaces/{workspace}/runs/{run_id}/result`
- `POST /api/v1/workspaces/{workspace}/runs/{run_id}/cancel`
- `GET /api/v1/workspaces/{workspace}/apps/{app}`
- `GET /api/v1/openapi.json`

`Idempotency-Key` scopes duplicate suppression to the authenticated principal.
Replaying the same key and request returns the existing Run; changing app,
action, input, or correlation data returns a conflict.

The app description endpoint returns the active release and materialized action
schemas. Protocol adapters use it to generate their own customer-facing API
documentation without mounting the Windforce catalog.

Operator, `wfk_` Client Registry, and scoped `wfs_` Service Principal
credentials use these same routes. Successful admission responses expose Run ID, state, and idempotent reuse through `X-WF-Run-Id`, `X-WF-Run-State`, and `X-WF-Idempotency-Reused`, never Job ID. See [Invocation API](concepts/public-api.md).

The v0.3 boundary removes `/execution/v1`,
`/api/v1/w/{workspace}/run/...`, and control-plane Job submission. ADR 0013
records the breaking transition. The non-production `wf-triggers` repository
is implemented separately against the stable canonical OpenAPI and is not part
of the operating rollback release set.

## Trigger resources and adapters

Trigger definitions are managed under `/api/w/{workspace}/triggers`. The
definition stores the adapter kind, enabled state, target app/action, public
protocol configuration, an optional credential reference, and encrypted
write-only secret configuration. Local and PostgreSQL stores preserve
configuration audit events and recent delivery outcomes without exposing
payload or credential values.

The `server` and `standalone` roles reconcile enabled definitions into
`Initialize -> Start -> Stop` adapter instances. Configured webhook, schedule,
and RabbitMQ adapters normalize source events into TriggerEvent and submit them
to the same in-process AdmissionService used by the Invocation API. They never
loop back through HTTP.

The source delivery ID, schedule occurrence instant, or a stable payload digest
becomes the principal-scoped idempotency key. Admission persists the Run and
first Job before a broker ACK. Retryable admission failures preserve the same
key and RabbitMQ requeues the message; terminal failures reject it for the
queue's dead-letter policy. Schedule actions receive `WF_SCHEDULED_FOR`.

Every Trigger pins an explicit completion policy on its TriggerDelivery:
authenticated Invocation API polling, signed HTTP callback, confirmed RabbitMQ
publish, or deliberate no output. A separate Webhook response policy chooses
immediate 202 or a bounded synchronous wait. Terminal Run completion is
dispatched durably without exposing the internal Job ID. See
[ADR 0016](adr/0016-trigger-completion-and-response-policy.md).

External trigger processes do not load this SPI. They use a least-privilege
`wfs_` Service Principal and the canonical `/api/v1` routes described in
[Triggers](concepts/triggers.md).

## HTTP Route Binding and Router Provider

Built-in Webhook exposure and generic external HTTP invocation are separate capabilities.

A built-in Webhook Trigger owns signature verification, delivery idempotency, target selection, response policy, completion policy and Run admission. Its HTTP Route Binding owns portable desired `hostname`, `path`, `visibility`, `provider` fields and provider-reported `state`, `public_url`, `error_summary`, and `observed_generation`.

```text
public hostname/path for a configured built-in Webhook
  -> external Router Provider
  -> POST /api/v1/workspaces/{workspace}/triggers/{trigger}/events
  -> TriggerSubmitter
  -> AdmissionService
```

Core exposes this desired state through the Control API and never talks to a Kubernetes API. A provider may materialize a Gateway API `HTTPRoute` or a hosted alias for the built-in Webhook ingress.

A Gateway acting as an external semantic adapter follows a different path:

```text
public hostname/path
  -> HTTP invocation adapter
  -> POST /api/v1/workspaces/{workspace}/runs or /runs/wait
  -> AdmissionService
```

The adapter owns external authentication and payload mapping, resolves the target Cell, App and Action, and calls the selected Cell with a scoped Service Principal. A transparent network rewrite to the built-in Trigger ingress is not equivalent to generic external invocation. [Run admission architecture](concepts/run-admission.md) defines the decision rule and complete request flows.

Desired updates increment `generation` and return to `pending`. Provider status
updates cannot modify desired fields, and a stale generation cannot make the
binding `ready`. Delete uses a tombstone until provider cleanup is observed, so
Trigger deletion cannot silently leave an external route behind. Provider
unavailability never removes or replaces the canonical ingress.

The historical decision that introduced the current built-in Webhook Route Binding is recorded in [ADR 0015](adr/0015-provider-neutral-http-route-bindings.md).

## SDK Boundary

Windforce Core does not identify or classify the SDKs used by an App. It exposes two SDK-neutral system interfaces, and optional libraries consume them.

- The Invocation SDK is an external HTTP client. The v0.3 reference client is `windforce-invocation` / `windforce_invocation.WindforceInvocationClient`. It provides create, status, wait, result, cancel, and app-description operations over `/api/v1`.
- The App runtime interface is `main(ctx)` and `WindforceContext`. Core injects its Author SDK as a language helper, but runtime acceptance does not depend on which SDK packages the App imports.
- Every other SDK is an opaque App dependency inside the prepared bundle. It may adapt `WindforceContext` into any application API, but Core neither observes nor interprets that adaptation.

PostgreSQL schemas, Job IDs, bundle paths, catalog storage, `WF_*` process transport, and Worker Plane authority are private implementation details of Windforce Core. Application SDKs do not receive Core credentials or choose a worker, launcher, bundle revision, lease, or completion path. Core does not inspect an SDK identity, context, version, or module envelope. The former execution SDK package has no v0.3 compatibility import.

See [App runtime interface and SDK boundaries](concepts/app-runtime-interface.md) for the SDK-neutral Core and App boundary. [ADR 0021](adr/0021-keep-application-sdks-opaque-to-core.md) records the decision rationale.

## Process Roles

| Role | Responsibility |
| --- | --- |
| `server` | Control `/api/w`, Invocation `/api/v1`, Worker `/worker/v1`, embedded Web UI, inbound Trigger adapters, outbound Webhook Dispatcher, and retention loops |
| `worker` | Queue claim and action execution, using shared PostgreSQL state or the remote worker API selected by `--api-url` |
| `standalone` | `server` and `worker` in one process |

The HTTP plane boundaries separate ownership and system-to-system specifications,
not processes. Principal scopes, rather than separate public/trusted URL trees,
separate caller authority. Server replicas expose every HTTP plane and may
safely run the dispatcher concurrently. Internal package boundaries remain
independent so an adapter can move between in-process and HTTP deployment
without changing admission semantics.
