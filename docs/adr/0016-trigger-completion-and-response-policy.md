# ADR 0016: Trigger completion and HTTP response policy

- Status: Accepted
- Date: 2026-07-28
- Tracking: [#155](https://github.com/imprun/windforce-core/issues/155)

## Context

Built-in Webhook, Schedule, and RabbitMQ Triggers already converged on the
in-process AdmissionService and created the same caller-visible Run as the
Invocation API. Their source acknowledgement behavior was defined, but the
Trigger definition did not say how an integration receives the terminal Action
result. The configured Webhook ingress returned only a Run ID, Schedule had no
result destination, and RabbitMQ defined only the source queue.

HTTP waiting is also different from durable completion delivery. A request can
wait briefly for a result, but disconnects and timeouts cannot be the durable
record of an integration outcome.

## Decision

Every Trigger definition has two explicit policies:

1. `completion` controls the durable terminal result:
   - `poll`: retain the result for authenticated Invocation API status/result
     requests.
   - `callback`: POST a signed completion envelope to an HTTPS endpoint with
     leases, bounded exponential retry, and a terminal failure state.
   - `publish`: publish a persistent completion envelope to RabbitMQ with
     publisher confirms and mandatory routing.
   - `none`: deliberately create no external completion delivery.
2. `response` controls only the configured Webhook HTTP response:
   - `async`: return `202`, `run_id`, `status_url`, and `result_url`.
   - `wait`: wait at most 60 seconds and return the raw Action result when the
     Run settles; a timeout still returns the admitted Run.

`completion` is required by the Control API. Storage normalization maps an
absent legacy value to `none` so old local and PostgreSQL records remain
readable. `wait` is valid only for Webhook Triggers.

The non-secret completion policy is pinned to `TriggerDelivery` at admission.
Changing or deleting the Trigger therefore does not redirect an already
accepted source event. The encrypted current Trigger secret supplies the
callback signing key or RabbitMQ connection URL. Write-only secret updates are
nested patches, so rotating one completion credential cannot erase the source
adapter credential.

When the linked Run reaches `SUCCEEDED`, `FAILED`, `CANCELED`, or `EXPIRED`, the
completion dispatcher materializes the delivery:

```text
source adapter
  -> AdmissionService
  -> Run + first Job
  -> TriggerDelivery (pinned completion policy)
  -> terminal Run
  -> completion dispatcher
       -> poll available
       -> signed HTTP callback
       -> confirmed RabbitMQ publish
       -> ignored (none)
```

The completion envelope contains workspace, Trigger delivery, correlation, Run,
app/action, terminal state, output/error, and completion time. It never exposes
the internal Job ID. Callback delivery uses the existing outbound egress policy
and HMAC headers. RabbitMQ credentials and callback signing keys are encrypted
and never returned.

## Consequences

- Source acknowledgement remains tied to durable admission, not Action
  completion.
- Invocation API and built-in Triggers still converge at AdmissionService.
- An integration chooses output wiring when it creates the Trigger; `none` is
  visible rather than accidental.
- HTTP `wait` is convenient but is not a replacement for callback or publish
  when delivery must survive disconnects.
- Poll URLs require an Invocation credential with Run read permission; the
  ingress HMAC secret does not become a general workspace token.
- Completion attempts and terminal delivery state are observable on the
  Trigger delivery history without revealing lease or credential data.
