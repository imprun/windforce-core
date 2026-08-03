# ADR 0025: Managed worker credentials and group drain fences

## Status

Accepted (2026-08-03). Extends ADR 0010 and ADR 0023. Tracking: issues #193 and #194.

## Context

ADR 0010 introduced the remote worker plane with one static worker token (or
the instance-admin token as a fallback). That is sufficient for a trusted
self-hosted worker, but it is not a safe contract for independently managed
worker groups:

- one leaked token can advertise another group's labels and claim its jobs;
- one token cannot be rotated or revoked without affecting every remote worker;
- a controller cannot stop new claims for one group while preserving its
  already leased jobs;
- retrying a timed-out create or drain request can create duplicate authority
  or roll state backward after a restart.

The engine must own these authorization and lease fences. An external control
plane may call the public HTTP API, but it must not read the Core database or
import internal Go packages.

## Decision

### 1. Credential generations

Instance-admin callers create credentials with:

`POST /api/worker-groups/{group}/credentials`

Each credential has an immutable group, generation, exact offered-label set,
workspace allowlist, optional expiry, and a `wfr_` bearer. The raw bearer is
returned only by the successful non-replayed create response. Core persists
only its SHA-256 hash. API projections, registry records, errors, and logs omit
both the raw bearer and its hash.

Creation carries `operation_id` and `expected_generation`. A retry with the
same normalized request returns the existing credential without returning the
bearer again. A reused operation with different input, or a stale expected
generation, returns conflict. Rotation creates the next active generation;
earlier generations remain usable until explicitly revoked.

### 2. Worker-plane scope enforcement

A managed bearer authenticates to a credential principal. Registration must
exactly match the credential's group and offered labels; workspace scope is
derived from the credential and is not accepted from the worker body. The live
registry stores only credential ID and generation as its authority reference.
Registration updates are atomically accepted only when that authority matches
the existing worker ID, so concurrent credentials cannot take over a registry
record between an ownership check and an upsert.

Claims must come from a worker registered by that credential, retain the
registered tag set, and use the exact credential labels. Candidate selection
atomically filters by the credential workspace allowlist before leasing a Job.
The existing tag-membership plus required-label subset rule remains unchanged.

The legacy static worker token and instance-admin fallback remain compatible
for trusted self-hosted deployments. They do not gain managed scope semantics
and should not be distributed to managed workers.

### 3. Revocation preserves owned leases

Revocation uses:

`POST /api/worker-groups/{group}/credentials/{credential_id}/revoke`

It records a drain deadline while retaining the bearer hash. A revoked
credential cannot register a new active worker or claim new work. Until the
deadline it may heartbeat/deregister an existing registered worker and may
heartbeat, append logs to, complete, or fetch the execution artifact needed by
an already owned lease. Managed artifact fetches carry Job, workspace, and
worker lease context and the requested digest must equal that Job's pinned
bundle. Lease fencing still decides whether a completion is
valid. After the deadline those continuation calls are denied.

### 4. Persisted group run state

Instance admins set a group fence with:

`PUT /api/worker-groups/{group}/run-state`

The body contains `operation_id`, `expected_revision`, `state` (`running` or
`draining`), and a required `deadline_at` for draining. The first revision is
zero and the implicit initial state is `running`. The operation ID plus request
fingerprint makes exact retries idempotent; the expected revision rejects stale
or reordered commands, including after restart.

`draining` causes managed claims for that group to return no work. It does not
invalidate existing leases and the deadline does not imply SIGKILL, Job
cancellation, credential revocation, or automatic resume. A later `running`
revision reopens claims.

### 5. CLI contract

Remote workers read their bearer from the environment named by
`--worker-token-env`. `--api-token-env` remains a deprecated compatibility
fallback. The bearer is not accepted as a command-line value.

## Consequences

- Managed groups can rotate independently and drain without losing fenced Job
  results or ordered logs.
- PostgreSQL and local JSON stores persist the same credential and run-state
  semantics.
- A caller that loses the only create response cannot recover the raw bearer;
  it must observe the created generation, issue the next generation with a new
  operation ID, and revoke the unusable one.
- The v1 drain deadline is an observation/coordination deadline. Forceful
  termination requires a separate, explicit, audited contract.
- Gateway, tenant, billing, Kubernetes, and product-specific policy remain
  outside Windforce Core.
