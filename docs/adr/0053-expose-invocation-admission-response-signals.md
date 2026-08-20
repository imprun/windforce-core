# ADR 0053: Expose Invocation admission response signals

- Status: Accepted
- Date: 2026-08-20
- Issues: [#269](https://github.com/imprun/windforce-core/issues/269), [#270](https://github.com/imprun/windforce-core/issues/270)

## Context

The canonical Invocation API returns a Run representation for asynchronous admission and wait timeout, but a completed `/runs/wait` response intentionally contains only the raw Action result. An external protocol adapter therefore cannot determine the terminal Run state from that response without another Run read when the Action result has no product-specific success envelope.

Admission also transparently reuses the existing Run for an exact `Idempotency-Key` replay. The asynchronous response body exposes `replayed`, but the completed wait result does not, and the timeout path previously reset the field to false. Callers that resume bounded wait slices or meter durable Runs need to distinguish a new admission from an HTTP retry without counting requests as Runs.

These are transport projections of facts already decided by AdmissionService. They must not introduce billing, quota, or product-specific result classification into Core.

## Decision

Every successful `POST /api/v1/workspaces/{workspace}/runs` and `POST /api/v1/workspaces/{workspace}/runs/wait` response exposes the following headers:

- `Location`: canonical URL of the admitted Run;
- `X-WF-Run-Id`: caller-visible Run identifier;
- `X-WF-Run-State`: lowercase canonical state observed for the response (`queued`, `running`, `waiting_human`, `resuming`, `succeeded`, `failed`, `canceled`, or `expired`);
- `X-WF-Idempotency-Reused`: `true` only when the request exactly reused an existing Run under the authenticated principal and canonical request fingerprint, otherwise `false`.

For `/runs/wait`, `X-WF-Run-State` is the same state snapshot used to choose the raw terminal result or the `202` Run representation. When a Run representation includes the optional `replayed` field, it agrees with `X-WF-Idempotency-Reused`; the header explicitly carries both true and false. A completed `200` response remains the raw Action result, so callers use the headers for Run metadata without interpreting the Action payload.

Responses rejected before successful admission do not promise these headers. A conflicting reuse of an idempotency key remains an error and is not reported as a successful replay.

The generated OpenAPI documents all four response headers. The Python Invocation SDK projects the wait response into `InvocationResult.state` and `InvocationResult.replayed`, with body fallback for compatibility with older Core versions.

## Consequences

Protocol adapters can adjudicate terminal state, resume bounded wait slices, and meter one durable Run without an additional status request. The headers are additive and preserve existing bodies and status codes. Core still does not define commercial billing semantics; downstream products decide how to use the durable Run identity and reuse signal.
