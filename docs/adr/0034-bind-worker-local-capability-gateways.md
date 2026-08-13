# ADR 0034: Bind worker-local capability gateways

- Status: Accepted
- Date: 2026-08-13
- Issue: [#223](https://github.com/imprun/windforce-core/issues/223)

## Context

Some application bundles need worker-local facilities that should not run in
the application process: native document engines, spreadsheet parsers, PKI
adapters, or other bounded external-service connectors. These facilities may
hold native resources, need independent concurrency limits, or exchange binary
artifacts that do not belong in Job JSON, logs, or Core state.

Core must preserve its SDK-neutral execution model. It must not recognize a
scraping SDK, provider operation, document profile, or provider-specific error
schema. At the same time, a worker must not claim a Job that needs a local
facility which is absent, and a worker-wide gateway credential must never enter
an application process.

## Decision

### Optional worker binding

A worker may be configured with one HTTP capability gateway on an explicit
loopback URL, one worker credential, and one or more ordinary placement labels.
Core accepts only an absolute `http` URL on `localhost` or a loopback IP with an
explicit port. Credentials, paths, queries, and fragments are rejected.

At startup, the worker calls the gateway's generic discovery endpoint. Startup
fails when the configured gateway is unreachable, its response is invalid, or
it reports no ready provider. Only after successful discovery does the worker
advertise the configured gateway labels. The provider identifiers returned by
discovery remain opaque strings to Core.

The gateway binding is configured with:

- `--capability-gateway-url`;
- `--capability-gateway-token-env` or
  `--capability-gateway-token-file`;
- `--capability-gateway-timeout`;
- `--capability-gateway-labels`.

Equivalent `WINDFORCE_CAPABILITY_GATEWAY_*` environment settings are available
for worker packaging. The gateway URL and labels do not become Release fields.

### Placement and run lifecycle

The canonical manifest and operator policy continue to resolve to `runsOn`
labels under the existing placement contract. Core opens a gateway run only
when the claimed Job's effective required labels intersect the configured
gateway labels. A Job without that intersection receives no gateway metadata.

Before launching a matching Job, the worker creates a gateway run with the
worker credential. The requested TTL follows the pinned Action timeout, with a
small cleanup allowance and the gateway's one-hour maximum. The gateway returns
an opaque run reference and a Job-scoped run token.

Core injects only the following private worker metadata into the effective
input under the reserved runtime key:

```json
{
  "_SCRAPING_RUNTIME": {
    "capabilities": {
      "baseUrl": "http://127.0.0.1:18092",
      "runRef": "opaque",
      "runToken": "job-scoped",
      "available": ["opaque.provider/v1"]
    }
  }
}
```

The key is worker-owned private transport. An Application SDK may consume and
remove it while adapting the generic low-level host context. Application code
must not depend on the key directly. Core does not add provider methods to
`WindforceContext` and does not inspect the Application SDK.

The worker registers the run token as a secret before application logs and
results are processed. It closes the gateway run after every terminal,
interrupted, or canceled execution path. Cleanup uses a short context that is
independent of the canceled execution context. The worker credential remains
inside the worker and is used only to create runs.

### Provider and artifact boundary

Core owns only discovery, run creation, private binding, masking, and cleanup.
It does not proxy provider operations or binary artifacts. The Application SDK
uses the Job-scoped gateway session through Core's existing low-level local HTTP
transport. The gateway owns provider allowlists, native-engine loading,
deadlines, provider concurrency, binary artifact limits, and sanitized provider
errors.

Provider identifiers and placement labels are deliberately separate
namespaces. An external authoring pipeline or Application SDK maps a provider
identifier such as `document.pdf/v1` to a label-valid manifest value such as
`document.pdf.v1`. Core evaluates only the label and carries discovery values
opaquely at runtime.

## Consequences

- Applications remain ordinary immutable bundles and Core remains unaware of
  SDK and provider identities.
- Jobs requiring a configured local facility are placed only on workers that
  completed gateway discovery.
- A compromised App receives only its own short-lived run token, not the
  worker-wide gateway credential.
- Provider-native resource and binary-transfer concerns stay outside Core
  state, Job JSON, and logs.
- The worker currently binds one loopback gateway. Multiple gateways or remote
  gateways require a separate decision covering label-to-gateway resolution
  and trust boundaries.

