# ADR 0038: Bind registered Worker claims to immutable lease identity

- Status: Accepted
- Date: 2026-08-14
- Issue: [#219](https://github.com/imprun/windforce-core/issues/219)

## Context

Worker registration advertises the tags, labels, execution profiles, group,
status, and credential generation currently owned by one Worker ID. The claim
API also carries tags and labels because unregistered static Workers predate the
registry.

Treating the request selector as authoritative after the same Worker ID has
registered permits a caller to claim work outside its advertisement. Deriving a
running Job's group later by joining `lease_owner` to the mutable registry is
also unsafe: deregistration followed by reuse of the same Worker ID can move an
old lease to a different group or make its original group appear quiescent.

Core needs one claim-time identity contract that both placement preconditions
and Worker-group observation can rely on. It must preserve the self-hosted
unregistered Worker compatibility path without representing that path as
attributable managed capacity.

## Decision

### Registered claim authority

If a Worker ID exists in the canonical registry when a Job is claimed, the
registry record is authoritative. The State Store performs the check inside the
same Local lock or PostgreSQL transaction that claims the Job:

- the registered Worker must be active and live;
- the requested tags must exactly equal the normalized registered tags;
- the requested labels must exactly equal the normalized registered labels plus
  Core-derived execution-profile labels; and
- Job selection uses that canonical registered selector, not the request body.

PostgreSQL holds a row lock on the registry record until the claim transaction
commits. Registration, deregistration, and claim therefore cannot mutate an
existing Worker identity between validation and lease creation.

Placement capacity checks must derive their matching selector through the same
canonical helper. HTTP handlers may reject invalid requests earlier, but the
State Store remains the final claim boundary.

### Immutable attempt attribution

A successful registered claim pins this non-secret identity to the Job attempt:

```text
WorkerLeaseIdentity
  group
  credential_generation
```

Worker ID, credential ID, token, and request selector are not copied into the
identity. Local storage persists identities in a Job-keyed snapshot map;
PostgreSQL persists nullable `jobs.lease_identity` JSONB. The internal `Job` and
`Lease` values carry the identity for Core state operations, but normal Job and
Worker-plane JSON omit it.

The identity remains unchanged if the Worker deregisters or the same Worker ID
registers under another group. A terminal Job may retain the historical
identity. Requeue after lease expiry and cancellation paths that remove lease
ownership clear it; the next claim pins a new identity for its new attempt.
Existing running rows have no identity after migration and are treated as
unattributed rather than joined to mutable registry state.

### Compatibility path

When no registry row exists at claim time, the existing static/direct claim
contract remains available and uses the request tags and labels. Such a claim
has no `WorkerLeaseIdentity`. Operational gates must treat missing attribution
as fail-closed: it cannot prove Worker-group capacity or group quiescence.

A static Worker that does register is subject to the registered selector rule.
Its credential generation remains zero, which allows observations to distinguish
unmanaged registered Workers without weakening claim enforcement.

## Consequences

- Reusing a Worker ID cannot rewrite the group or credential generation of an
  already claimed attempt.
- Registered static and managed Workers cannot claim with selectors broader or
  different from their current registry advertisement.
- Local and PostgreSQL storage gain equivalent internal attribution state; no
  public Job or Worker-plane API field is added.
- Existing unregistered self-hosted Workers keep claiming work, but group-scoped
  rollout and capacity decisions cannot count those claims as positive evidence.
- Worker-group observation and placement mutation work must consume this pinned
  identity and canonical selector before it can be considered safe to merge.
