# ADR 0056: Persist opaque HTTP projections and resolve them atomically

- Status: Accepted
- Date: 2026-09-02
- Issue: [#283](https://github.com/imprun/windforce-core/issues/283)

## Context

ADR 0055 defines a body-blind Resolver port and an unmounted opaque HTTP conformance handler. A production host cannot implement that Resolver from mutable in-memory routing data: publication and credential changes may race with one another, a process restart must not forget a revoke, and a stale gateway generation must not be admitted against a newer target.

Core needs a self-hostable, provider-neutral projection store. It must not become a global customer, gateway, or credential catalog, and it must never receive raw authentication or cryptographic material.

## Decision

Core persists two immutable inputs. An opaque ingress credential snapshot contains only an immutable credential reference, freshness bounds, an operation reference, and bounded opaque immutable references. An opaque ingress publication revision contains the trusted issuer and audience, exact HTTP route and media policy, App and Action, the exact active Release identity, response limits, freshness bounds, and the credential snapshot references allowed by that publication.

The trusted ingress request has no caller-selected workspace. Consequently, `(issuer, audience, publicationRef)` is a globally unique route slot and `(issuer, audience, credential id, credential revision)` is a globally unique credential-reference coordinate. Resolution discovers the workspace from the immutable projection; a caller cannot use a workspace value to reinterpret the same trusted tuple.

Credential rotation creates another immutable snapshot, creates a publication revision that pins it, and activates that revision through the publication compare-and-swap path. Credential revocation is append-only and remains durable across restart. A publication revision is never edited. Activation appends a route generation and advances the publication head only when the caller's expected generation equals the current generation. Generation starts at one and increases by exactly one. Publication revocation also appends a generation and closes the head.

Every mutation, including retention pruning, carries an operation identifier and a server-derived request fingerprint. Repeating the same operation and fingerprint returns the original result; reusing the identifier for different input fails. Local and PostgreSQL stores perform replay detection, compare-and-swap, head movement, retention changes, and audit append in one atomic update or database transaction.

Credential snapshot and publication revision digests use the versioned `projection-ascii-subset/v1` contract. Every key starts with an ASCII letter and contains only ASCII letters or digits, every string value is ASCII, and every number is an integer within the exact JSON safe-integer range. Objects are recursively sorted by ASCII key bytes and encoded as compact UTF-8 JSON with no optional HTML escaping. Arrays remain ordered. Publishers sort named immutable references by `name`, credential references by `id` then `revision`, and response content types by ASCII bytes before hashing. Optional reference collections are encoded as `[]`, never `null` or an omitted member. Timestamps are UTC RFC 3339 with the shortest exact fractional-second form.

The credential material schema is `windforce-core.opaque-ingress-credential-snapshot/v1`. Its fields are `schema`, the normalized path `workspaceId`, `issuer`, `audience`, `reference` containing only `id` and `revision`, `operationRef`, `references`, `projectedAt`, `notAfter`, and `maxStalenessSeconds`. `reference.digest` is the SHA-256 result and is not part of its own material.

The publication material schema is `windforce-core.opaque-ingress-publication-revision/v1`. Its fields are `schema`, the normalized path `workspaceId`, `issuer`, `audience`, `publicationRef`, `revision`, `app`, `action`, `release`, `http`, `operationRef`, complete credential references including their digests, `references`, `projectedAt`, `notAfter`, `maxStalenessSeconds`, and `retainUntil`. The top-level `digest` is not part of its own material. Mutation metadata (`operationId`, request fingerprint, actor, and creation time) is excluded from both materials. The portable input and expected lowercase SHA-256 values are fixed by [`opaque_ingress_projection_digest_v1.json`](../../internal/state/testdata/opaque_ingress_projection_digest_v1.json). The control-plane OpenAPI advertises the canonicalization, material schema, digest field, and path-derived workspace source on both mutation schemas.

Rollback never moves a head backward. It activates a retained immutable publication revision through the same compare-and-swap path and therefore creates a new greater generation. Audit and activation history remain append-only.

The existing workspace Control Plane listener exposes strict, bounded JSON mutation and observation endpoints under `/api/w/{workspace}/opaque-http-projections`. Unknown and duplicate JSON members are rejected. These endpoints require an explicit admin Bearer token or a Service Principal with the dedicated read or write scope even when the general local-development admin token is unset. Workspace tokens do not authorize this surface. Credential mutation and retention require the administrator because those records do not carry an App and Action authorization target. A Service Principal may create a publication revision only when it has exactly one allowed target equal to that revision's `app/action`. Activation, rollback, and publication revocation pass that exact allowed target into the atomic Store transaction and bind it to the target revision. Mutation responses and audit records contain references, revisions, generations, operation identifiers, actors, and timestamps only.

One atomic Store read resolves the active publication head, its exact immutable revision, one exact active credential snapshot, and the current active Release. The request issuer, audience, publication reference, route generation, credential reference, method, canonical escaped path, media type, and byte limit must all match that same view. Revoked, expired, over-stale, unknown, digest-mismatched, or mixed state returns no snapshot.

The Resolver converts that snapshot into ADR 0055's `ResolvedAdmission`. It derives a stable non-secret principal identity from the credential snapshot digest and constructs a Service Principal with exactly `runs:create` and `runs:read:own` and one allowed `app/action` target. It returns an `ActiveReleasePrecondition` with non-empty Deployment ID, commit, and execution-bundle digest. Admission compares the Release precondition again immediately before selecting the Action. Expected projection failures are fail-closed before Admission and do not create a Run.

Local state persists the complete revision, head, activation, revocation, operation, and audit state through its crash-safe snapshot replacement. PostgreSQL uses immutable revision and credential rows, append-only activation and audit rows, and compare-and-swap heads. Both implementations pass one conformance suite.

Retention pruning is dependency-aware. Active publication heads, revocations, audit history, activation history, every publication revision that has been activated, and every credential snapshot referenced by retained publication history remain available. Only expired immutable snapshots that were never activated and are not referenced by retained publication history may be removed.

## Consequences

An operator can prepare and activate route projections without placing a Control Plane on the request data path. Restart, concurrent writers, retries, revoke, and rollback preserve one monotonic history. A caller can observe safe references and generations but cannot read a credential, token hash, key, provider document, or arbitrary secret-bearing configuration through this API.

The projection store does not mount ADR 0055's handler, open a listener, authenticate an external credential, interpret `publicInterfaces`, configure a gateway, or mint a downstream execution attestation. Those remain separate host, adapter, and follow-up decisions.

## Rejected alternatives

- **Reuse HTTP route bindings.** Rejected because that resource is tied to Webhook Trigger and provider lifecycle semantics, updates desired state in place, and has no immutable credential snapshot or activation compare-and-swap.
- **Keep the active projection only in memory.** Rejected because restart would forget generation and revoke state.
- **Let each publication embed raw credentials.** Rejected because Core needs only immutable references and must not become a credential custodian.
- **Reuse a previous generation for rollback.** Rejected because observers could not distinguish rollback from stale replay.
- **Let the Resolver return a stored broad Service Principal.** Rejected because mutable or excessive scopes would widen the authority used by Admission.
- **Treat the Release check performed during resolution as final.** Rejected because publication can race with Admission; the exact Release precondition is checked again at the admission boundary.
