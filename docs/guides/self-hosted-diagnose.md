# Self-hosted diagnostics

`windforce-core diagnose` is a provider-neutral, read-only preflight for Core
installations. It inspects the same durable state, immutable artifacts, runtime
profiles, and placement projections used by Core without running migrations,
repairing files, claiming Jobs, or depending on hosted Identity, Kubernetes, or
Flux.

```sh
windforce-core diagnose --mode standalone
windforce-core diagnose --mode server --state-backend postgres \
  --database-url "$WINDFORCE_CORE_DATABASE_URL"
windforce-core diagnose --mode remote-worker --api-url https://core.example \
  --worker-token-env WINDFORCE_WORKER_TOKEN
windforce-core diagnose --mode standalone --json > diagnose.json
```

Use the same state, store, runtime, credential-environment, and optional
capability-gateway flags as the process being diagnosed. Values of credentials,
database URLs, filesystem paths, raw errors, App inputs, and Secret values are
never emitted. JSON output conforms to
[`diagnose/v1`](../api/diagnose-report.schema.json).

## Modes and applicability

| Check | standalone | server | remote-worker |
| --- | --- | --- | --- |
| State connectivity and schema | checked | checked | unsupported |
| Active Release and artifact integrity | checked | checked | unsupported |
| Runtime profiles | checked | unsupported | checked |
| Placement and queued scheduling reasons | checked | checked | unsupported |
| Server readiness probe | optional | optional | required |
| Server/worker authentication | checked | checked | checked |
| Secret backend | checked | checked | unsupported |
| Worker-local capability gateway | optional | unsupported | optional |

`unsupported` is an explicit applicability result, not a failure. Stable exit
codes are:

- `0`: all applicable checks passed
- `1`: no applicable check failed, but at least one warning needs attention
- `2`: at least one applicable check failed
- `64`: invalid flags or the report could not be written

Stable placement reason codes include `missing_tag`, `missing_label`,
`execution_profile_mismatch`, `draining`, `no_live_capacity`, and
`no_available_slots`. These codes are safe automation inputs; human messages
and remediation text are descriptive.

The PostgreSQL schema check verifies the minimum table-and-column contract for
the current Core build. It never migrates. Stop serving or claiming Jobs and run
the normal migration command when `state.schema` fails. Source snapshots are
checked using their completion marker identity and file count; execution bundles
are checked using their immutable descriptor and digest.
