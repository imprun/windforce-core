# ADR 0027: Operationalize HumanTask hold with notifications and durable lifecycle events

## Status

Accepted (2026-08-04). Extends ADR 0026. Tracking: issue #192.

## Context

ADR 0026 established the bounded `HumanTask` hold contract: the same Action
process, call stack, browser session, Job lease, and worker slot remain alive
until a decision or terminal cause wins. The first implementation persisted the
task correctly, but each connected wait request queried the state store every
300 milliseconds. A deadline was normally materialized only by that connected
waiter. An external integration also had to discover tasks by polling the list
API.

Those mechanics are correct enough for a local proof, but they do not form an
operational multi-instance design. A production design must tolerate missed
notifications and server restarts without making notifications the source of
truth, must expire tasks even when no HTTP client is connected, and must not
confuse an HTTP connection lifetime with the HumanTask or Action lifetime.

The existing signed Webhook subsystem already provides an atomic PostgreSQL or
LocalStore outbox, delivery leases, retry, observability, and retention. A
second HumanTask-specific broker would duplicate those guarantees.

## Decision

1. **The persisted HumanTask row remains the source of truth.** Notifications
   are wake-up hints only. Every wake-up is followed by a fresh state read.
2. **Waiters subscribe before reading.** This ordering prevents a terminal
   transition between the read and subscription from being missed. LocalStore
   uses an in-process keyed signal hub. PostgreSQL uses one dedicated
   `LISTEN windforce_human_task` connection per Core process and broadcasts the
   committed task ID to local waiters. A waiter never owns a PostgreSQL
   connection.
3. **Low-frequency reconciliation is mandatory.** Waiters re-read the durable
   row periodically even when no signal arrives. This recovers from a dropped
   PostgreSQL connection, a missed notification, or an older writer that did
   not publish a notification. Reconciliation is a safety net, not the normal
   delivery path.
4. **A deadline sweeper is independent of HTTP waiters.** Each Core server
   periodically claims a bounded batch of due pending hold tasks. PostgreSQL
   uses row locks with `SKIP LOCKED`; LocalStore uses its existing serialized
   update. The same first-terminal-writer rule used by decisions and
   cancellation applies to expiry.
5. **Timeouts have separate meanings.** The Author-requested HumanTask timeout
   produces the durable HumanTask deadline. The Action timeout cancels the App
   process and records `action_timeout`. Run cancellation, lease loss, and
   worker shutdown retain their existing stable causes. A runtime HTTP session
   has a shorter transport timeout and may reconnect with the same hold key;
   ending that HTTP session does not cancel or extend the HumanTask.
6. **HumanTask lifecycle events use the existing durable Webhook outbox.** Core
   publishes `windforce.human_task.created`, `.decided`, `.expired`, and
   `.canceled` CloudEvents in the same state transaction as the task
   transition. Existing workspace Webhook subscriptions, signing, delivery
   retry, audit, and retention apply. Events contain only identifiers, generic
   state, App/Action routing fields, outcome, actor, and terminal cause. They do
   not contain schema values, private context, or decision values. An external
   adapter can receive the event and use its scoped HumanTask API credential to
   read metadata or submit a decision without polling.
7. **Core terminology remains generic.** The public names remain `HumanTask`,
   `ctx.human.wait()`, `/api/w/{workspace}/human-tasks`, and `mode=hold`. An
   Application SDK may call the concept `Interaction` and map its form or
   channel semantics to Core, but those names and payloads do not become Core
   types.

## Failure and race semantics

- PostgreSQL `NOTIFY` is not durable. The task row and Webhook outbox are
  durable; reconciliation repairs a missed internal wake-up.
- A decision, deadline, cancellation, action timeout, lease loss, and worker
  shutdown race under the existing atomic state transition. Only one terminal
  transition is persisted and emitted.
- Replaying the same decision remains idempotent. It does not emit a duplicate
  lifecycle event.
- Reconnecting the runtime wait with the same task key and request fingerprint
  returns the existing task and original deadline. It does not create another
  task or extend the wait.
- Webhook delivery failure does not roll back a task transition. The delivery
  record remains retryable in the durable outbox.

## Consequences

- Normal decision latency no longer depends on a 300 millisecond query loop,
  and PostgreSQL query load does not grow linearly with that polling rate.
- A disconnected client cannot leave an overdue task pending indefinitely.
- External Interaction, notification, or RMQ adapters consume one generic,
  signed lifecycle event contract instead of learning Core storage or polling
  conventions.
- HumanTask hold still consumes live worker capacity. This ADR does not add
  suspend, checkpoint, replay, or process reconstruction.
- Python and Go author helpers, SDK-specific Interaction APIs, vendor forms,
  and external adapter implementations remain outside Core.

## Rejected alternatives

- **Keep fast database polling as the primary wake-up path.** Rejected because
  query volume scales with pending tasks and instances even when nothing
  changes.
- **Use PostgreSQL `LISTEN/NOTIFY` as the source of truth.** Rejected because
  notifications are transient and can be lost during reconnects.
- **Allocate one PostgreSQL listener connection per waiter.** Rejected because
  a held task must not consume a database connection for its full lifetime.
- **Create a HumanTask-specific message broker or RMQ contract in Core.**
  Rejected because the existing Webhook outbox already provides the required
  durable external delivery boundary and Core must remain broker-neutral.
- **Rename hold to Interaction.** Rejected because Interaction is an
  application or SDK vocabulary, while `hold` describes the Core execution
  mode.
